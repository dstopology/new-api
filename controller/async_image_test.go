package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageRequestWantsAsyncJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "true", body: `{"model":"test","async":true}`, want: true},
		{name: "false", body: `{"model":"test","async":false}`, want: false},
		{name: "absent", body: `{"model":"test"}`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			async, err := imageRequestWantsAsync(c)
			require.NoError(t, err)
			require.Equal(t, test.want, async)
		})
	}
}

func TestImageRequestWantsAsyncMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "test"))
	require.NoError(t, writer.WriteField("async", "true"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	async, err := imageRequestWantsAsync(c)
	require.NoError(t, err)
	require.True(t, async)
}

func asyncImageHashJSONContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestAsyncImageRequestHashCanonicalJSON(t *testing.T) {
	a := asyncImageHashJSONContext(t, `{"model":"m","prompt":"draw","async":true,"seed":0}`)
	b := asyncImageHashJSONContext(t, `{
		"seed": 0,
		"async": true,
		"prompt": "draw",
		"model": "m"
	}`)
	c := asyncImageHashJSONContext(t, `{"model":"m","prompt":"different","async":true,"seed":0}`)

	hashA, err := asyncImageRequestHash(a)
	require.NoError(t, err)
	hashB, err := asyncImageRequestHash(b)
	require.NoError(t, err)
	hashC, err := asyncImageRequestHash(c)
	require.NoError(t, err)
	require.Equal(t, hashA, hashB)
	require.NotEqual(t, hashA, hashC)
}

func asyncImageHashMultipartContext(t *testing.T, filename, prompt, content string) *gin.Context {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "m"))
	require.NoError(t, writer.WriteField("prompt", prompt))
	require.NoError(t, writer.WriteField("async", "true"))
	part, err := writer.CreateFormFile("image", filename)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestAsyncImageRequestHashCanonicalMultipart(t *testing.T) {
	a := asyncImageHashMultipartContext(t, "first.png", "edit", "same pixels")
	b := asyncImageHashMultipartContext(t, "renamed.png", "edit", "same pixels")
	c := asyncImageHashMultipartContext(t, "first.png", "edit", "changed pixels")

	hashA, err := asyncImageRequestHash(a)
	require.NoError(t, err)
	hashB, err := asyncImageRequestHash(b)
	require.NoError(t, err)
	hashC, err := asyncImageRequestHash(c)
	require.NoError(t, err)
	require.Equal(t, hashA, hashB, "multipart boundaries and filenames are not request semantics")
	require.NotEqual(t, hashA, hashC)
}

func TestPrepareAsyncImageIdempotencyRejectsInvalidKey(t *testing.T) {
	c := asyncImageHashJSONContext(t, `{"model":"m","prompt":"draw","async":true}`)
	c.Request.Header.Set("Idempotency-Key", "contains whitespace")
	require.Error(t, prepareAsyncImageIdempotency(c))
}

func TestReplayAsyncImageIdempotentTask(t *testing.T) {
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "async-image-replay.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	key := "replay-key"
	require.NoError(t, db.Create(&model.Task{
		TaskID:          "task_replayed",
		UserId:          12,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          model.TaskStatusQueued,
		Progress:        "20%",
		SubmitTime:      123,
		IdempotencyKey:  &key,
		RequestHash:     "request-hash",
		SubmissionState: model.TaskSubmissionStateSubmitted,
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 12)
	common.SetContextKey(c, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(c, constant.ContextKeyAsyncImageRequestHash, "request-hash")
	require.True(t, replayAsyncImageIdempotentTask(c))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "true", recorder.Header().Get("Idempotent-Replayed"))
	require.Contains(t, recorder.Body.String(), `"id":"task_replayed"`)

	conflictRecorder := httptest.NewRecorder()
	conflict, _ := gin.CreateTestContext(conflictRecorder)
	conflict.Set("id", 12)
	common.SetContextKey(conflict, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(conflict, constant.ContextKeyAsyncImageRequestHash, "different-hash")
	require.True(t, replayAsyncImageIdempotentTask(conflict))
	require.Equal(t, http.StatusConflict, conflictRecorder.Code)
	require.Contains(t, conflictRecorder.Body.String(), "idempotency_conflict")
}

func TestAsyncImageTaskSubmissionIsNotRetried(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{StatusCode: http.StatusTooManyRequests}, 2))
}
