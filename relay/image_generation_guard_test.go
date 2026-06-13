package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRejectImageGenerationRequestRespectsChannelSetting(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIImage,
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		Request:     &dto.ImageRequest{Model: "gpt-image-1", Prompt: "draw"},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	require.Nil(t, RejectImageGenerationRequest(info))

	info.ChannelSetting.DisableImageGeneration = true
	err := RejectImageGenerationRequest(info)
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.Equal(t, types.ErrorCodeImageGenerationDisabled, err.GetErrorCode())
}

func TestResponsesRequestAllowsImageGenerationToolDeclaration(t *testing.T) {
	require.False(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Tools: []byte(`[{"type":"image_generation"}]`),
	}))
	require.True(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		ToolChoice: []byte(`{"type":"image_generation"}`),
	}))
	require.False(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Tools: []byte(`[{"type":"image_generation_preview"}]`),
	}))
	require.False(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}]
	}`)))
	require.False(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"tools":[{"type":"function","function":{"name":"helper","parameters":{"allowed_tools":[{"type":"image_generation_preview"}]}}}]
	}`)))
	require.False(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"input":[{"role":"user","content":[{"type":"input_text","text":"please explain image_generation"}]}]
	}`)))
}

func TestPrepareImageGenerationDisabledJSONBodyRemovesToolsAndAddsNotice(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"instructions":"You may call image_generation when useful.",
		"tools":[
			{"type":"web_search_preview"},
			{"type":"image_generation","output_format":"png"}
		],
		"metadata":{"allowed_tools":[{"type":"image_generation_preview"},{"type":"file_search"}]}
	}`)

	prepared, err := PrepareImageGenerationDisabledJSONBody(body)
	require.Nil(t, err)
	require.NotContains(t, string(prepared), "image_generation")
	require.Contains(t, string(prepared), "暂不支持生图")
	require.Contains(t, string(prepared), `"type":"web_search_preview"`)
	require.Contains(t, string(prepared), `"type":"file_search"`)
}

func TestPrepareImageGenerationDisabledJSONBodyRejectsExplicitToolChoice(t *testing.T) {
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-5",
		"tool_choice":{"type":"image_generation"},
		"tools":[{"type":"image_generation"}]
	}`))

	require.Nil(t, prepared)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeImageGenerationDisabled, err.GetErrorCode())
}

func TestGeneralOpenAIRequestUsesImageOutputModality(t *testing.T) {
	require.True(t, GeneralOpenAIRequestUsesImageGeneration(&dto.GeneralOpenAIRequest{
		Model:      "gpt-5",
		Modalities: []byte(`["image"]`),
	}))
	require.True(t, GeneralOpenAIRequestUsesImageGeneration(&dto.GeneralOpenAIRequest{
		Model:      "gpt-5",
		ToolChoice: map[string]any{"type": "image_generation"},
	}))
	require.False(t, GeneralOpenAIRequestUsesImageGeneration(&dto.GeneralOpenAIRequest{
		Model: "gpt-5",
		Messages: []dto.Message{
			{Role: "user", Content: []dto.MediaContent{{Type: dto.ContentTypeImageURL}}},
		},
	}))
}

func TestIsMidjourneyGenerationMode(t *testing.T) {
	require.True(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyImagine))
	require.True(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyEdits))
	require.True(t, IsMidjourneyGenerationMode(relayconstant.RelayModeSwapFace))

	require.False(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyDescribe))
	require.False(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyShorten))
	require.False(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyTaskFetch))
	require.False(t, IsMidjourneyGenerationMode(relayconstant.RelayModeMidjourneyNotify))
}
