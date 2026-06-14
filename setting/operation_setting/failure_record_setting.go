package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// FailureRecordSetting controls the lightweight failed-request recorder used for
// security troubleshooting — judging whether a block was a false positive and
// whether violating content reached the upstream account pool (which risks the
// account being rate/risk-controlled).
//
// Unlike the heavy Conversation Archive (separate Postgres + R2, records ALL
// requests), this records ONLY failed relay requests, writes the RAW (unmasked)
// request body to the main log DB, and purges each row after RetentionDays.
// It is controlled entirely from the UI — no environment variable gating.
type FailureRecordSetting struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
	MaxBodyKB     int  `json:"max_body_kb"`
}

var failureRecordSetting = FailureRecordSetting{
	Enabled:       false,
	RetentionDays: 7,
	MaxBodyKB:     256,
}

func init() {
	config.GlobalConfig.Register("failure_record_setting", &failureRecordSetting)
}

func GetFailureRecordSetting() *FailureRecordSetting {
	return &failureRecordSetting
}

func IsFailureRecordEnabled() bool {
	return failureRecordSetting.Enabled
}

// FailureRecordRetentionDays returns the per-row retention in days (0 = keep
// forever / no automatic purge).
func FailureRecordRetentionDays() int {
	if failureRecordSetting.RetentionDays < 0 {
		return 0
	}
	return failureRecordSetting.RetentionDays
}

// FailureRecordMaxBodyBytes returns the byte cap applied to the stored raw body
// and error detail. Violating text is small, so the cap mainly bounds large
// base64 image bodies.
func FailureRecordMaxBodyBytes() int {
	kb := failureRecordSetting.MaxBodyKB
	if kb <= 0 {
		kb = 256
	}
	return kb * 1024
}
