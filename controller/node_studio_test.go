package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func configureNodeStudioForTest(t *testing.T) {
	t.Helper()

	original := system_setting.GetNodeStudioSettings()
	originalServerAddress := system_setting.ServerAddress
	cfg := config.GlobalConfig.Get("node_studio")
	if cfg == nil {
		t.Fatal("node_studio config is not registered")
	}
	if err := config.UpdateConfigFromMap(cfg, map[string]string{
		"enabled": "true",
		"url":     "https://node.example.com/auth/import-keys",
		"secret":  "test-shared-secret",
	}); err != nil {
		t.Fatalf("configure Node Studio: %v", err)
	}
	system_setting.ServerAddress = "https://api.example.com"

	t.Cleanup(func() {
		enabled := "false"
		if original.Enabled {
			enabled = "true"
		}
		_ = config.UpdateConfigFromMap(cfg, map[string]string{
			"enabled": enabled,
			"url":     original.URL,
			"secret":  original.Secret,
		})
		system_setting.ServerAddress = originalServerAddress
	})
}

func runNodeStudioHandoffForTest(t *testing.T, userID int) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/node-studio/handoff", nil)
	c.Set("id", userID)
	NodeStudioHandoff(c)
	return recorder
}

func TestNodeStudioHandoffCreatesAccountOnlyBundleAndReusesAccessToken(t *testing.T) {
	db := openTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate Node Studio test tables: %v", err)
	}
	configureNodeStudioForTest(t)

	user := &model.User{
		Username: "node-studio-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	first := runNodeStudioHandoffForTest(t, user.Id)
	if first.Code != http.StatusOK {
		t.Fatalf("first handoff status = %d, body = %s", first.Code, first.Body.String())
	}
	body := first.Body.String()
	if !strings.Contains(body, `action="https://node.example.com/auth/import-keys"`) {
		t.Fatalf("handoff target missing from body: %s", body)
	}
	if !strings.Contains(body, `value="`+service.NodeStudioHandoffVersionPrefix+`.`) {
		t.Fatalf("encrypted payload missing from body: %s", body)
	}
	if strings.Contains(body, user.Username) {
		t.Fatal("handoff HTML must not contain plaintext user data")
	}

	var persisted model.User
	if err := db.First(&persisted, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	firstAccessToken := persisted.GetAccessToken()
	if firstAccessToken == "" {
		t.Fatal("handoff did not create a dashboard access token")
	}
	if strings.Contains(body, firstAccessToken) {
		t.Fatal("handoff HTML must not contain a plaintext dashboard access token")
	}

	second := runNodeStudioHandoffForTest(t, user.Id)
	if second.Code != http.StatusOK {
		t.Fatalf("second handoff status = %d, body = %s", second.Code, second.Body.String())
	}
	if err := db.First(&persisted, user.Id).Error; err != nil {
		t.Fatalf("reload user after second handoff: %v", err)
	}
	if persisted.GetAccessToken() != firstAccessToken {
		t.Fatal("handoff rotated an existing dashboard access token")
	}
}
