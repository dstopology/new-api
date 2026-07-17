package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	userRPMWindow       = time.Minute
	userRPMRedisKeyMark = "rateLimit:URPM:user:"
	userRPMCapacityText = "Selected model is at capacity. Please try a different model."
)

var userRPMSlidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local current_time = redis.call('TIME')
local now = tonumber(current_time[1]) * 1000 + math.floor(tonumber(current_time[2]) / 1000)
local cutoff = now - window

redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)

if count >= limit then
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  local retry_after = window
  if oldest[2] then
    retry_after = math.max(1, tonumber(oldest[2]) + window - now)
  end
  redis.call('PEXPIRE', key, window)
  return {0, 0, retry_after}
end

local member = tostring(now) .. ':' .. tostring(count)
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)
return {1, limit - count - 1, 0}
`)

type userRPMDecision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

type userRPMLocalWindow struct {
	mutex       sync.Mutex
	requests    map[userRPMKey][]int64
	lastCleanup int64
}

type userRPMKey struct {
	userId int
	group  string
}

var inMemoryUserRPMLimiter userRPMLocalWindow

func (l *userRPMLocalWindow) allow(userId int, group string, limit int, now time.Time) userRPMDecision {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.requests == nil {
		l.requests = make(map[userRPMKey][]int64)
	}

	key := userRPMKey{userId: userId, group: group}
	nowMillis := now.UnixMilli()
	windowMillis := userRPMWindow.Milliseconds()
	cutoff := nowMillis - windowMillis
	queue := l.requests[key]
	firstActive := 0
	for firstActive < len(queue) && queue[firstActive] <= cutoff {
		firstActive++
	}
	if firstActive > 0 {
		queue = append(queue[:0], queue[firstActive:]...)
	}

	if l.lastCleanup == 0 || nowMillis-l.lastCleanup >= windowMillis {
		for key, requests := range l.requests {
			if len(requests) == 0 || requests[len(requests)-1] <= cutoff {
				delete(l.requests, key)
			}
		}
		l.lastCleanup = nowMillis
	}

	if len(queue) >= limit {
		l.requests[key] = queue
		retryMillis := queue[0] + windowMillis - nowMillis
		if retryMillis < 1 {
			retryMillis = 1
		}
		return userRPMDecision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: time.Duration(retryMillis) * time.Millisecond,
		}
	}

	queue = append(queue, nowMillis)
	l.requests[key] = queue
	return userRPMDecision{
		Allowed:   true,
		Remaining: limit - len(queue),
	}
}

func redisResultInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis result type %T", value)
	}
}

func getUserRPMRedisKey(userId int, group string) string {
	encodedGroup := base64.RawURLEncoding.EncodeToString([]byte(group))
	return fmt.Sprintf("%s%d:group:%s", userRPMRedisKeyMark, userId, encodedGroup)
}

func checkRedisUserRPM(ctx context.Context, userId int, group string, limit int) (userRPMDecision, error) {
	if common.RDB == nil {
		return userRPMDecision{}, fmt.Errorf("redis client is not initialized")
	}

	key := getUserRPMRedisKey(userId, group)
	result, err := userRPMSlidingWindowScript.Run(
		ctx,
		common.RDB,
		[]string{key},
		userRPMWindow.Milliseconds(),
		limit,
	).Result()
	if err != nil {
		return userRPMDecision{}, err
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return userRPMDecision{}, fmt.Errorf("unexpected Redis rate limit result %T", result)
	}
	allowed, err := redisResultInt64(values[0])
	if err != nil {
		return userRPMDecision{}, err
	}
	remaining, err := redisResultInt64(values[1])
	if err != nil {
		return userRPMDecision{}, err
	}
	retryMillis, err := redisResultInt64(values[2])
	if err != nil {
		return userRPMDecision{}, err
	}

	return userRPMDecision{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryMillis) * time.Millisecond,
	}, nil
}

func enforceUserRPMRateLimit(c *gin.Context, userId int, group string, limit int) bool {
	if group == "" || limit <= 0 {
		return true
	}

	var decision userRPMDecision
	var err error
	if common.RedisEnabled {
		decision, err = checkRedisUserRPM(c.Request.Context(), userId, group, limit)
	} else {
		decision = inMemoryUserRPMLimiter.allow(userId, group, limit, time.Now())
	}
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to check RPM limit for user %d group %q: %v", userId, group, err))
		return true
	}

	if decision.Allowed {
		return true
	}

	service.RecordFailedRelayConsumeLogWithMessage(
		c,
		nil,
		http.StatusTooManyRequests,
		"server_error",
		"",
		userRPMCapacityText,
	)
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error": gin.H{
			"message": userRPMCapacityText,
			"type":    "server_error",
			"param":   nil,
			"code":    "server_error",
		},
	})
	c.Abort()
	return false
}
