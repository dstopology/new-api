package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func userPromptAdditionRelayInfo(addition string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelSetting: dto.ChannelSettings{
			UserPromptAdditionEnabled: true,
			UserPromptAddition:        addition,
		},
	}}
}

func TestApplyUserPromptAdditionToOpenAIMessages(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "second"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/image.png"}},
		}},
	}}

	applied, err := applyUserPromptAddition(userPromptAdditionRelayInfo("use ultracode"), request)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "use ultracode\nfirst", request.Messages[1].StringContent())
	contents := request.Messages[3].ParseContent()
	require.Len(t, contents, 3)
	require.Equal(t, "use ultracode", contents[0].Text)
	require.Equal(t, "second", contents[1].Text)
	require.Equal(t, dto.ContentTypeImageURL, contents[2].Type)
}

func TestApplyUserPromptAdditionToClaudeMessages(t *testing.T) {
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: []any{map[string]any{"type": "text", "text": "second"}}},
	}}

	applied, err := applyUserPromptAddition(userPromptAdditionRelayInfo("use ultracode"), request)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "use ultracode\nfirst", request.Messages[0].GetStringContent())
	contents, err := request.Messages[2].ParseContent()
	require.NoError(t, err)
	require.Len(t, contents, 2)
	require.Equal(t, "use ultracode", contents[0].GetText())
	require.Equal(t, "second", contents[1].GetText())
}

func TestApplyUserPromptAdditionToGeminiContents(t *testing.T) {
	request := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
		{Parts: []dto.GeminiPart{{Text: "first"}}},
		{Role: "model", Parts: []dto.GeminiPart{{Text: "answer"}}},
		{Role: "user", Parts: []dto.GeminiPart{{Text: "second"}}},
	}}

	applied, err := applyUserPromptAddition(userPromptAdditionRelayInfo("use ultracode"), request)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "use ultracode", request.Contents[0].Parts[0].Text)
	require.Equal(t, "first", request.Contents[0].Parts[1].Text)
	require.Equal(t, "answer", request.Contents[1].Parts[0].Text)
	require.Equal(t, "use ultracode", request.Contents[2].Parts[0].Text)
}

func TestApplyUserPromptAdditionToResponsesInput(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "assistant", "content": "answer"},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "second"}}},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}

	applied, err := applyUserPromptAddition(userPromptAdditionRelayInfo("use ultracode"), request)
	require.NoError(t, err)
	require.True(t, applied)

	var output []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &output))
	require.Equal(t, "use ultracode\nfirst", output[0]["content"])
	contents, ok := output[2]["content"].([]any)
	require.True(t, ok)
	require.Len(t, contents, 2)
	require.Equal(t, "use ultracode", contents[0].(map[string]any)["text"])
}

func TestApplyUserPromptAdditionDisabled(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "first"}}}
	info := userPromptAdditionRelayInfo("use ultracode")
	info.ChannelSetting.UserPromptAdditionEnabled = false

	applied, err := applyUserPromptAddition(info, request)
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, "first", request.Messages[0].StringContent())
}
