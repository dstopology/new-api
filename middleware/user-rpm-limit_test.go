package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserRPMLocalWindowEnforcesSlidingWindow(t *testing.T) {
	limiter := userRPMLocalWindow{}
	start := time.Unix(1_700_000_000, 0)

	first := limiter.allow(12, "default", 2, start)
	require.True(t, first.Allowed)
	require.Equal(t, 1, first.Remaining)

	second := limiter.allow(12, "default", 2, start.Add(10*time.Second))
	require.True(t, second.Allowed)
	require.Equal(t, 0, second.Remaining)

	blocked := limiter.allow(12, "default", 2, start.Add(59*time.Second))
	require.False(t, blocked.Allowed)
	require.Equal(t, time.Second, blocked.RetryAfter)

	afterWindow := limiter.allow(12, "default", 2, start.Add(60*time.Second))
	require.True(t, afterWindow.Allowed)
	require.Equal(t, 0, afterWindow.Remaining)
}

func TestUserRPMLocalWindowSeparatesUsersAndGroups(t *testing.T) {
	limiter := userRPMLocalWindow{}
	now := time.Unix(1_700_000_000, 0)

	require.True(t, limiter.allow(1, "default", 1, now).Allowed)
	require.False(t, limiter.allow(1, "default", 1, now).Allowed)
	require.True(t, limiter.allow(1, "vip", 1, now).Allowed)
	require.True(t, limiter.allow(2, "default", 1, now).Allowed)
}

func TestRedisResultInt64(t *testing.T) {
	for _, value := range []any{int64(3), 3, "3", []byte("3")} {
		result, err := redisResultInt64(value)
		require.NoError(t, err)
		require.Equal(t, int64(3), result)
	}

	_, err := redisResultInt64(true)
	require.Error(t, err)
}

func TestEnforceUserRPMRateLimitReturnsPlain429(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	const userId = 998_002
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		if enforceUserRPMRateLimit(c, userId, "default", 1) {
			c.Status(http.StatusNoContent)
		}
	})

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	router.ServeHTTP(firstRecorder, firstRequest)
	require.Equal(t, http.StatusNoContent, firstRecorder.Code)

	recorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	router.ServeHTTP(recorder, secondRequest)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	var response struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, userRPMCapacityText, response.Error.Message)
	require.Equal(t, "server_error", response.Error.Type)
	require.Equal(t, "server_error", response.Error.Code)
	require.NotContains(t, recorder.Body.String(), "RPM")
	require.Empty(t, recorder.Header().Get("Retry-After"))
	require.Empty(t, recorder.Header().Get("X-RateLimit-Limit-Requests"))
}

func TestEnforceUserRPMRateLimitRecordsRejectedRequest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousLogConsumeEnabled := common.LogConsumeEnabled
	previousRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.LogConsumeEnabled = true
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.LogConsumeEnabled = previousLogConsumeEnabled
		common.RedisEnabled = previousRedisEnabled
	})

	const userId = 998_003
	require.NoError(t, db.Create(&model.User{Id: userId, Username: "rpm-limited-user"}).Error)

	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("id", userId)
		c.Set("username", "rpm-limited-user")
		c.Set("token_id", 321)
		c.Set("token_name", "rpm-test-token")
		c.Set("group", "default")
		if enforceUserRPMRateLimit(c, userId, "default", 1) {
			c.Status(http.StatusNoContent)
		}
	})

	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusNoContent, firstRecorder.Code)

	blockedRecorder := httptest.NewRecorder()
	router.ServeHTTP(blockedRecorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusTooManyRequests, blockedRecorder.Code)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, model.LogTypeConsume, logs[0].Type)
	require.Equal(t, userId, logs[0].UserId)
	require.Equal(t, 321, logs[0].TokenId)
	require.Equal(t, "default", logs[0].Group)
	require.Zero(t, logs[0].Quota)

	var other struct {
		Failed     bool   `json:"failed"`
		StatusCode int    `json:"status_code"`
		ErrorType  string `json:"error_type"`
		ErrorCode  string `json:"error_code"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	require.True(t, other.Failed)
	require.Equal(t, http.StatusTooManyRequests, other.StatusCode)
	require.Equal(t, "server_error", other.ErrorType)
	require.Empty(t, other.ErrorCode)
	require.NotContains(t, logs[0].Content, "user_rpm_limit")
	require.NotContains(t, logs[0].Other, "user_rpm_limit")
}

func TestRedisUserRPMSlidingWindowIsAtomic(t *testing.T) {
	redisAddress := os.Getenv("TEST_REDIS_ADDR")
	if redisAddress == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}

	client := redis.NewClient(&redis.Options{Addr: redisAddress})
	require.NoError(t, client.Ping(context.Background()).Err())
	t.Cleanup(func() {
		_ = client.Close()
	})

	previousClient := common.RDB
	common.RDB = client
	t.Cleanup(func() {
		common.RDB = previousClient
	})

	const (
		userId   = 998_001
		limit    = 20
		attempts = 50
	)
	const group = "default"
	key := getUserRPMRedisKey(userId, group)
	require.NoError(t, client.Del(context.Background(), key).Err())
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
	})

	var allowed atomic.Int64
	errors := make(chan error, attempts)
	var waitGroup sync.WaitGroup
	for range attempts {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			decision, err := checkRedisUserRPM(context.Background(), userId, group, limit)
			if err != nil {
				errors <- err
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	require.Equal(t, int64(limit), allowed.Load())
}
