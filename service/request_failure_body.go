package service

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const failureRequestBodyRecordedKey = "failure_request_body_recorded"

const secondsPerDay = 86400

// RecordFailureRequestBody captures the RAW inbound request body and raw error of
// a failed relay request into request_failure_logs, gated by
// failure_record_setting.enabled. Used purely for security troubleshooting; the
// body is stored UNMASKED and truncated, with a per-row TTL. The synchronous part
// only reads the already-buffered body and truncates it; the DB write is async.
//
// RPM rate-limit 429s never reach this path (they abort in middleware before the
// relay), and client-side non-failures (insufficient quota, etc.) carry status
// < 400 and are skipped, so neither pollutes the records.
func RecordFailureRequestBody(c *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil {
		return
	}
	if !operation_setting.IsFailureRecordEnabled() {
		return
	}
	if c.GetBool(failureRequestBodyRecordedKey) {
		return
	}

	statusCode := apiErr.StatusCode
	if statusCode > 0 && statusCode < 400 {
		return
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return
	}
	c.Set(failureRequestBodyRecordedKey, true)

	maxBytes := operation_setting.FailureRecordMaxBodyBytes()
	requestBody := truncateForTextColumn(rawBody, maxBytes)
	errorDetail := truncateForTextColumn([]byte(apiErr.Error()), maxBytes)

	now := common.GetTimestamp()
	expiresAt := int64(0)
	if days := operation_setting.FailureRecordRetentionDays(); days > 0 {
		expiresAt = now + int64(days)*secondsPerDay
	}

	rec := &model.RequestFailureLog{
		CreatedAt:         now,
		ExpiresAt:         expiresAt,
		UserId:            failedLogUserId(c, relayInfo),
		Username:          c.GetString("username"),
		TokenName:         c.GetString("token_name"),
		Group:             failedLogGroup(c, relayInfo),
		ChannelId:         failedLogChannelId(c, relayInfo),
		ChannelName:       c.GetString("channel_name"),
		ModelName:         failedLogModelName(c, relayInfo),
		StatusCode:        statusCode,
		ErrorType:         string(apiErr.GetErrorType()),
		ErrorCode:         string(apiErr.GetErrorCode()),
		RequestId:         c.GetString(common.RequestIdKey),
		UpstreamRequestId: c.GetString(common.UpstreamRequestIdKey),
		RequestBody:       requestBody,
		ErrorDetail:       errorDetail,
	}
	gopool.Go(func() {
		if err := rec.Insert(); err != nil {
			common.SysError("failed to record request failure body: " + err.Error())
		}
	})
}

// truncateForTextColumn bounds b to maxBytes and makes it safe for a TEXT column
// on PostgreSQL (which rejects invalid UTF-8 and NUL) as well as MySQL/SQLite:
// drops any invalid UTF-8 sequences (including a rune split by the cut) and NULs.
func truncateForTextColumn(b []byte, maxBytes int) string {
	if maxBytes > 0 && len(b) > maxBytes {
		b = b[:maxBytes]
	}
	s := strings.ToValidUTF8(string(b), "")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return s
}

// StartRequestFailureLogCleanup purges expired request-failure records on a timer.
// It runs regardless of the enable flag (so disabling recording still lets old
// rows expire) but is a no-op when retention is set to 0 (keep forever).
func StartRequestFailureLogCleanup() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Hour)
			if operation_setting.FailureRecordRetentionDays() <= 0 {
				continue
			}
			if _, err := model.DeleteExpiredRequestFailureLogs(context.Background(), common.GetTimestamp(), 1000); err != nil {
				common.SysError("failed to clean expired request failure logs: " + err.Error())
			}
		}
	})
}
