package relay

import (
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestPrepareResponsesStreamRecoveryRemovesBackgroundWhenDisabled(t *testing.T) {
	for _, background := range []bool{false, true} {
		request := &dto.OpenAIResponsesRequest{Background: &background}
		info := newResponsesStreamRecoveryTestInfo(false)

		prepareResponsesStreamRecovery(info, request, false)

		require.Nil(t, request.Background)
		require.False(t, info.ResponsesUsageInfo.Background)
		require.False(t, info.ResponsesUsageInfo.StreamResumeEnabled)
	}
}

func TestPrepareResponsesStreamRecoveryForcesBackgroundWhenEnabled(t *testing.T) {
	background := false
	request := &dto.OpenAIResponsesRequest{Background: &background}
	info := newResponsesStreamRecoveryTestInfo(true)

	prepareResponsesStreamRecovery(info, request, false)

	require.NotNil(t, request.Background)
	require.True(t, *request.Background)
	require.True(t, info.ResponsesUsageInfo.Background)
	require.True(t, info.ResponsesUsageInfo.StreamResumeEnabled)
}

func TestPrepareResponsesStreamRecoveryPassThroughRequiresClientBackground(t *testing.T) {
	info := newResponsesStreamRecoveryTestInfo(true)
	request := &dto.OpenAIResponsesRequest{}

	prepareResponsesStreamRecovery(info, request, true)

	require.Nil(t, request.Background)
	require.False(t, info.ResponsesUsageInfo.StreamResumeEnabled)

	background := true
	request.Background = &background
	prepareResponsesStreamRecovery(info, request, true)

	require.Same(t, &background, request.Background)
	require.True(t, info.ResponsesUsageInfo.StreamResumeEnabled)
}

func TestPrepareResponsesStreamRecoveryRemovesBackgroundOutsideResponsesStream(t *testing.T) {
	background := true
	request := &dto.OpenAIResponsesRequest{Background: &background}
	info := newResponsesStreamRecoveryTestInfo(true)
	info.IsStream = false

	prepareResponsesStreamRecovery(info, request, false)

	require.Nil(t, request.Background)
	require.False(t, info.ResponsesUsageInfo.StreamResumeEnabled)
}

func newResponsesStreamRecoveryTestInfo(enabled bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:           true,
		RelayMode:          relayconstant.RelayModeResponses,
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: appconstant.APITypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				EnableResponsesStreamResume: enabled,
			},
		},
	}
}
