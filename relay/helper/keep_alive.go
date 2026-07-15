package helper

import (
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

const DefaultImageKeepAliveInterval = 25 * time.Second

func isImageRelayMode(mode int) bool {
	return mode == relayconstant.RelayModeImagesGenerations || mode == relayconstant.RelayModeImagesEdits
}

func RelayPingConfig(info *relaycommon.RelayInfo, generalSettings *operation_setting.GeneralSetting) (bool, time.Duration) {
	if info == nil || info.DisablePing {
		return false, DefaultPingInterval
	}

	imageMode := isImageRelayMode(info.RelayMode)
	// Claude CLI (/v1/messages) streams, e.g. context compaction, can stall well past
	// Cloudflare's ~120s origin timeout before the first byte and trip a 524. Keep that
	// format alive like image mode — independent of the global ping switch — so other
	// channels stay completely unaffected unless the operator opts in globally.
	claudeMode := info.RelayFormat == types.RelayFormatClaude
	pingEnabled := imageMode || claudeMode
	if generalSettings != nil && generalSettings.PingIntervalEnabled {
		pingEnabled = true
	}
	if !pingEnabled {
		return false, DefaultPingInterval
	}

	interval := DefaultPingInterval
	if generalSettings != nil && generalSettings.PingIntervalSeconds > 0 {
		interval = time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	}
	if imageMode || claudeMode {
		if generalSettings == nil || !generalSettings.PingIntervalEnabled || interval > time.Minute {
			interval = DefaultImageKeepAliveInterval
		}
	}
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	return true, interval
}
