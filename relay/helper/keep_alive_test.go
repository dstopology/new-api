package helper

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestRelayPingConfigUsesImageStreamInterval(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	enabled, interval := RelayPingConfig(info, &operation_setting.GeneralSetting{})

	require.True(t, enabled)
	require.Equal(t, DefaultImageKeepAliveInterval, interval)
}

func TestRelayPingConfigRespectsDisablePing(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		DisablePing: true,
	}
	enabled, interval := RelayPingConfig(info, &operation_setting.GeneralSetting{
		PingIntervalEnabled: true,
		PingIntervalSeconds: 1,
	})

	require.False(t, enabled)
	require.Equal(t, DefaultPingInterval, interval)
	require.NotEqual(t, time.Second, interval)
}
