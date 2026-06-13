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

func TestResponsesRequestUsesImageGenerationTool(t *testing.T) {
	require.True(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Tools: []byte(`[{"type":"image_generation"}]`),
	}))
	require.True(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		ToolChoice: []byte(`{"type":"image_generation"}`),
	}))
	require.True(t, ResponsesRequestUsesImageGeneration(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Tools: []byte(`[{"type":"image_generation_preview"}]`),
	}))
	require.False(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}]
	}`)))
	require.True(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"tools":[{"type":"function","function":{"name":"helper","parameters":{"allowed_tools":[{"type":"image_generation_preview"}]}}}]
	}`)))
	require.False(t, JSONBodyUsesImageGeneration([]byte(`{
		"model":"gpt-5",
		"input":[{"role":"user","content":[{"type":"input_text","text":"please explain image_generation"}]}]
	}`)))
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
