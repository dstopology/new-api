package router

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletMigrationRouteRequiresAdministrator(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, redis, quotaPerUnit := model.DB, common.RedisEnabled, common.QuotaPerUnit
	model.DB, common.RedisEnabled = db, false
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { model.DB, common.RedisEnabled, common.QuotaPerUnit = oldDB, redis, quotaPerUnit; sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	adminToken, userToken := "wallet-admin-test-token", "wallet-user-test-token"
	admin := model.User{Username: "wallet-admin", Password: "unused", AffCode: "wallet-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AccessToken: &adminToken}
	user := model.User{Username: "wallet-user", Password: "unused", AffCode: "wallet-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AccessToken: &userToken, Quota: 6_172_839}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&user).Error)
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
			req := httptest.NewRequest(http.MethodPost, "/api/user/migration/wallet-transfer", strings.NewReader(fmt.Sprintf(`{"action":"inspect","user_id":%d,"username":"wallet-user","expected_balance":"12.35"}`, user.Id)))
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
	var current model.User
	require.NoError(t, db.First(&current, user.Id).Error)
	require.Equal(t, 6_172_839, current.Quota)
}

func TestWalletMigrationRateLimitIsolation(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			var client *redis.Client
			if backend == "redis" {
				address := os.Getenv("TEST_REDIS_ADDR")
				if address == "" {
					t.Skip("TEST_REDIS_ADDR is not set")
				}
				client = redis.NewClient(&redis.Options{Addr: address})
				require.NoError(t, client.Ping(context.Background()).Err())
				t.Cleanup(func() { _ = client.Close() })
			}
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldRedis, oldRDB, oldUnit := model.DB, common.RedisEnabled, common.RDB, common.QuotaPerUnit
			oldGlobal, oldGlobalNum, oldGlobalDuration := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
			oldCritical, oldCriticalNum, oldCriticalDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
			model.DB, common.RedisEnabled, common.RDB, common.QuotaPerUnit = db, client != nil, client, 500_000
			common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = true, 180, 180
			common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 20, 1200
			t.Cleanup(func() {
				model.DB, common.RedisEnabled, common.RDB, common.QuotaPerUnit = oldDB, oldRedis, oldRDB, oldUnit
				common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = oldGlobal, oldGlobalNum, oldGlobalDuration
				common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldCritical, oldCriticalNum, oldCriticalDuration
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&model.User{}))
			token := "wallet-rate-limit-admin-test-token"
			admin := model.User{Username: "rate-admin", Password: "unused", AffCode: "rate-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AccessToken: &token}
			users := []model.User{
				{Username: "rate-user-one", Password: "unused", AffCode: "rate-one", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 6_172_839},
				{Username: "rate-user-two", Password: "unused", AffCode: "rate-two", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 6_172_839},
			}
			require.NoError(t, db.Create(&admin).Error)
			require.NoError(t, db.Create(&users).Error)
			gin.SetMode(gin.TestMode)
			newEngine := func() *gin.Engine {
				engine := gin.New()
				require.NoError(t, engine.SetTrustedProxies(nil))
				engine.Use(sessions.Sessions("wallet-limit-test", cookie.NewStore([]byte("wallet-limit-test-session-key"))))
				SetApiRouter(engine)
				return engine
			}
			newIP := func() string {
				ip := netip.AddrFrom16(uuid.New()).String()
				if client != nil {
					t.Cleanup(func() {
						require.NoError(t, client.Del(context.Background(), "rateLimit:GA"+ip, "rateLimit:CT"+ip).Err())
					})
				}
				return ip
			}
			send := func(engine *gin.Engine, ip, path, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.RemoteAddr = net.JoinHostPort(ip, "12345")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("New-Api-User", strconv.Itoa(admin.Id))
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, req)
				return response
			}
			inspect := func(engine *gin.Engine, ip string, user model.User) *httptest.ResponseRecorder {
				return send(engine, ip, "/api/user/migration/wallet-transfer", fmt.Sprintf(`{"action":"inspect","user_id":%d,"username":%q,"expected_balance":"12.35"}`, user.Id, user.Username))
			}
			assertVerified := func(t *testing.T, response *httptest.ResponseRecorder, userID int) {
				t.Helper()
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var envelope struct {
					Success bool
					Data    struct {
						UserID int `json:"user_id"`
					}
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
				require.True(t, envelope.Success, response.Body.String())
				require.Equal(t, userID, envelope.Data.UserID)
			}
			engine := newEngine()
			t.Run("migrations_do_not_consume_login_budget", func(t *testing.T) {
				ip := newIP()
				for i := 0; i < 2*common.CriticalRateLimitNum; i++ {
					user := users[i%len(users)]
					assertVerified(t, inspect(engine, ip, user), user.Id)
				}
				for i := 0; i < common.CriticalRateLimitNum; i++ {
					require.Equal(t, http.StatusOK, send(engine, ip, "/api/user/login", "{").Code)
				}
				require.Equal(t, http.StatusTooManyRequests, send(engine, ip, "/api/user/login", "{").Code)
			})
			t.Run("exhausted_login_budget_does_not_block_migrations", func(t *testing.T) {
				ip := newIP()
				for i := 0; i < common.CriticalRateLimitNum; i++ {
					require.Equal(t, http.StatusOK, send(engine, ip, "/api/user/login", "{").Code)
				}
				require.Equal(t, http.StatusTooManyRequests, send(engine, ip, "/api/user/login", "{").Code)
				for _, user := range users {
					assertVerified(t, inspect(engine, ip, user), user.Id)
				}
			})
			t.Run("global_flood_budget_still_applies", func(t *testing.T) {
				common.GlobalApiRateLimitNum = 5
				limitedEngine, ip := newEngine(), newIP()
				for i := 0; i < common.GlobalApiRateLimitNum; i++ {
					assertVerified(t, inspect(limitedEngine, ip, users[0]), users[0].Id)
				}
				require.Equal(t, http.StatusTooManyRequests, inspect(limitedEngine, ip, users[0]).Code)
			})
			for _, user := range users {
				var current model.User
				require.NoError(t, db.First(&current, user.Id).Error)
				require.Equal(t, user.Quota, current.Quota)
			}
		})
	}
}
