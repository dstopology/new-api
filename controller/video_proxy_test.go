package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestServeLocalVideoAssetSupportsRangeRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	t.Setenv("ASYNC_MEDIA_DIR", root)
	video := append(
		[]byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00},
		make([]byte, 256)...,
	)
	fileName := "media_video_range.mp4"
	require.NoError(t, os.WriteFile(filepath.Join(root, fileName), video, 0600))

	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "video-proxy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TemporaryMedia{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, (&model.TemporaryMedia{
		MediaID:     "media_video_range",
		TaskID:      "task_video_range",
		Position:    0,
		UserID:      42,
		FileName:    fileName,
		ContentType: "video/mp4",
		Size:        int64(len(video)),
		ExpiresAt:   time.Now().Unix() + 300,
	}).Insert())

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_video_range/content", nil)
	context.Request.Header.Set("Range", "bytes=0-15")
	context.Set("id", 42)

	serveLocalVideoAsset(context, &model.Task{TaskID: "task_video_range"})

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "bytes 0-15/272", recorder.Header().Get("Content-Range"))
	require.Len(t, recorder.Body.Bytes(), 16)
}
