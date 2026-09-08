package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletMigrationRouteRequiresAdministrator(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, redis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = oldDB, redis; sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	hash, err := common.Password2Hash("wallet-test-password")
	require.NoError(t, err)
	adminToken, userToken := "wallet-admin-test-token", "wallet-user-test-token"
	admin := model.User{Username: "wallet-admin", Password: hash, AffCode: "wallet-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AccessToken: &adminToken}
	user := model.User{Username: "wallet-user", Password: hash, AffCode: "wallet-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AccessToken: &userToken, Quota: 5000000}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&user).Error)
	oldCritical, oldCriticalNum, oldCriticalDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 20, 1200
	t.Cleanup(func() {
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldCritical, oldCriticalNum, oldCriticalDuration
	})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("wallet-test", cookie.NewStore([]byte("wallet-test-session-key"))))
	SetApiRouter(engine) // also detects accidental duplicate/public registration
	for _, tc := range []struct {
		name, token string
		userID      int
		success     bool
	}{
		{"anonymous", "", 0, false}, {"normal user", userToken, user.Id, false},
		{"wrong administrator ID", adminToken, user.Id, false}, {"administrator", adminToken, admin.Id, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/user/migration/wallet-transfer", strings.NewReader(`{"action":"inspect","username":"wallet-user","password":"wallet-test-password"}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
				req.Header.Set("New-Api-User", strconv.Itoa(tc.userID))
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			var envelope struct {
				Success bool
				Data    *struct {
					UserID int `json:"user_id"`
				}
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope), response.Body.String())
			require.Equal(t, tc.success, envelope.Success)
			if tc.success {
				require.NotNil(t, envelope.Data)
				require.Equal(t, user.Id, envelope.Data.UserID)
			}
		})
	}
	// FrostFox calls inspect and transfer for every customer from the same server IP.
	// Authenticated migration traffic must not consume the 20-per-20-minute login budget.
	for i := 0; i < 21; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/user/migration/wallet-transfer", strings.NewReader(`{"action":"inspect","username":"wallet-user","password":"wallet-test-password"}`))
		req.RemoteAddr = "203.0.113.47:12345"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("New-Api-User", strconv.Itoa(admin.Id))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, "migration inspect %d was rate limited", i+1)
		var envelope struct{ Success bool }
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
		require.True(t, envelope.Success)
	}
	var current model.User
	require.NoError(t, db.First(&current, user.Id).Error)
	require.Equal(t, 5000000, current.Quota)
}
