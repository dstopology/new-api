package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// RequestFailureLog stores the RAW request body and error detail of FAILED relay
// requests for security troubleshooting (false-positive bans / violating content
// reaching the upstream account pool). It lives on the log DB next to Log, carries
// a per-row ExpiresAt for TTL purging, and stores bodies UNMASKED but truncated.
//
// Rows are written only when failure_record_setting.enabled is on; see
// service.RecordFailureRequestBody and DeleteExpiredRequestFailureLogs.
type RequestFailureLog struct {
	Id                int64  `json:"id" gorm:"primaryKey"`
	CreatedAt         int64  `json:"created_at" gorm:"index:idx_rfl_created"`
	ExpiresAt         int64  `json:"expires_at" gorm:"index:idx_rfl_expires"`
	UserId            int    `json:"user_id" gorm:"index:idx_rfl_user"`
	Username          string `json:"username" gorm:"size:64"`
	TokenName         string `json:"token_name" gorm:"size:64"`
	Group             string `json:"group" gorm:"column:group;size:64"`
	ChannelId         int    `json:"channel_id" gorm:"index:idx_rfl_channel"`
	ChannelName       string `json:"channel_name" gorm:"size:128"`
	ModelName         string `json:"model_name" gorm:"size:128;index:idx_rfl_model"`
	StatusCode        int    `json:"status_code"`
	ErrorType         string `json:"error_type" gorm:"size:64"`
	ErrorCode         string `json:"error_code" gorm:"size:64"`
	RequestId         string `json:"request_id" gorm:"size:64;index:idx_rfl_request_id"`
	UpstreamRequestId string `json:"upstream_request_id" gorm:"size:64"`
	RequestBody       string `json:"request_body" gorm:"type:text"`
	ErrorDetail       string `json:"error_detail" gorm:"type:text"`
}

func (RequestFailureLog) TableName() string {
	return "request_failure_logs"
}

func (l *RequestFailureLog) Insert() error {
	return LOG_DB.Create(l).Error
}

// GetRequestFailureLogByRequestId returns the most recent failure record for a
// request id, or (nil, nil) when none exists (e.g. recording was off, the row
// already expired, or the request did not fail).
func GetRequestFailureLogByRequestId(requestId string) (*RequestFailureLog, error) {
	if strings.TrimSpace(requestId) == "" {
		return nil, nil
	}
	var rec RequestFailureLog
	err := LOG_DB.Where("request_id = ?", requestId).Order("id DESC").First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// DeleteExpiredRequestFailureLogs purges rows whose ExpiresAt has passed, in
// batches to avoid long locks. Rows with ExpiresAt == 0 (retention disabled) are
// never purged. Cross-DB safe via GORM (mirrors DeleteOldLog).
func DeleteExpiredRequestFailureLogs(ctx context.Context, now int64, limit int) (int64, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	if limit <= 0 {
		limit = 1000
	}
	var total int64
	for {
		if ctx != nil && ctx.Err() != nil {
			return total, ctx.Err()
		}
		result := LOG_DB.Where("expires_at > 0 AND expires_at < ?", now).
			Limit(limit).
			Delete(&RequestFailureLog{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}
