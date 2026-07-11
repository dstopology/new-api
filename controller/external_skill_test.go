package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupExternalSkillTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:external_skill_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ExternalSkill{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/external-skills", AdminCreateExternalSkill)
	router.GET("/v1/external-skills", ListExternalSkills)
	router.GET("/v1/external-skills/:name", GetExternalSkill)
	router.GET("/v1/external-skills/:name/content", GetExternalSkillContent)
	return router
}

func performExternalSkillRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestExternalSkillPublicEndpoints(t *testing.T) {
	router := setupExternalSkillTestRouter(t)

	created := performExternalSkillRequest(router, http.MethodPost, "/api/external-skills", `{
		"name":"ultracode",
		"description":"Use for coding tasks",
		"content":"Always inspect the repository first."
	}`)
	require.Equal(t, http.StatusOK, created.Code)

	list := performExternalSkillRequest(router, http.MethodGet, "/v1/external-skills", "")
	require.Equal(t, http.StatusOK, list.Code)
	var listPayload struct {
		Success bool                         `json:"success"`
		Data    []model.ExternalSkillSummary `json:"data"`
	}
	require.NoError(t, common.Unmarshal(list.Body.Bytes(), &listPayload))
	require.True(t, listPayload.Success)
	require.Equal(t, []model.ExternalSkillSummary{{
		Name:        "ultracode",
		Description: "Use for coding tasks",
	}}, listPayload.Data)
	require.NotContains(t, list.Body.String(), "Always inspect the repository first.")

	detail := performExternalSkillRequest(router, http.MethodGet, "/v1/external-skills/ultracode", "")
	require.Equal(t, http.StatusOK, detail.Code)
	require.Contains(t, detail.Body.String(), "Always inspect the repository first.")
	require.NotContains(t, detail.Body.String(), "created_time")

	content := performExternalSkillRequest(router, http.MethodGet, "/v1/external-skills/ultracode/content", "")
	require.Equal(t, http.StatusOK, content.Code)
	require.Equal(t, "text/plain; charset=utf-8", content.Header().Get("Content-Type"))
	require.Equal(t, "Always inspect the repository first.", content.Body.String())
}
