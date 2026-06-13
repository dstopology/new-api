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
		Model:      "gpt-5",
		ToolChoice: []byte(`{"type":"allowed_tools","tools":[{"type":"image_generation"},{"type":"web_search_preview"}]}`),
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

func TestPrepareImageGenerationDisabledJSONBodyNeutralizesExplicitToolChoice(t *testing.T) {
	// A forced image_generation tool_choice must degrade gracefully (downgraded to
	// "auto" + tool stripped + notice injected) rather than 403, so a live client
	// session is not killed by an endless retry loop.
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-5",
		"tool_choice":{"type":"image_generation"},
		"tools":[{"type":"image_generation"}]
	}`))

	require.Nil(t, err)
	require.NotContains(t, string(prepared), "image_generation")
	require.Contains(t, string(prepared), `"tool_choice":"auto"`)
	require.Contains(t, string(prepared), "暂不支持生图")
}

func TestPrepareImageGenerationDisabledJSONBodyStripsImageModalities(t *testing.T) {
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-4o",
		"messages":[{"role":"user","content":"draw a cat"}],
		"modalities":["text","image"]
	}`))

	require.Nil(t, err)
	require.Contains(t, string(prepared), `"modalities":["text"]`)
	require.Contains(t, string(prepared), "暂不支持生图")
}

func TestPrepareImageGenerationDisabledJSONBodyPreservesMultimodalSystemMessage(t *testing.T) {
	// Regression guard: a system message with multimodal (array) content must NOT be
	// stringified/corrupted when the notice is injected. The notice is added as a
	// fresh system message; the original array content is left intact.
	body := []byte(`{
		"model":"gpt-4o",
		"modalities":["text","image"],
		"messages":[
			{"role":"system","content":[{"type":"text","text":"be concise"}]},
			{"role":"user","content":"draw a cat"}
		]
	}`)

	prepared, err := PrepareImageGenerationDisabledJSONBody(body)
	require.Nil(t, err)
	require.Contains(t, string(prepared), `"text":"be concise"`)
	require.NotContains(t, string(prepared), "map[") // no Go %v stringification of the array
	require.Contains(t, string(prepared), "暂不支持生图")
	require.Contains(t, string(prepared), `"modalities":["text"]`)
}

func TestRejectImageGenerationRequestAllowsChatAndResponsesPaths(t *testing.T) {
	// Core invariant: chat / responses requests are NEVER hard-rejected even when
	// the channel disables image generation — they degrade gracefully via the JSON
	// rewrite. Only the dedicated image endpoints get a 403 (no session to keep alive).
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		Request:     &dto.GeneralOpenAIRequest{Model: "gpt-image-1", Modalities: []byte(`["image"]`)},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	info.ChannelSetting.DisableImageGeneration = true
	require.Nil(t, RejectImageGenerationRequest(info))

	info.RelayMode = relayconstant.RelayModeResponses
	require.Nil(t, RejectImageGenerationRequest(info))

	// Dedicated image endpoint still rejected.
	info.RelayFormat = types.RelayFormatOpenAIImage
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	require.NotNil(t, RejectImageGenerationRequest(info))
}

func TestPrepareImageGenerationDisabledJSONBodyCleansAllowedToolChoice(t *testing.T) {
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-5",
		"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"image_generation"},{"type":"web_search_preview"}]},
		"tools":[{"type":"image_generation"},{"type":"web_search_preview"}]
	}`))

	require.Nil(t, err)
	require.NotContains(t, string(prepared), "image_generation")
	require.Contains(t, string(prepared), `"type":"allowed_tools"`)
	require.Contains(t, string(prepared), `"type":"web_search_preview"`)
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

func TestPrepareImageGenerationDisabledJSONBodyDowngradesEmptiedAllowedTools(t *testing.T) {
	// allowed_tools containing ONLY image_generation becomes empty after stripping;
	// it must be downgraded to "auto" so the upstream does not reject an empty list.
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-5",
		"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"image_generation"}]},
		"tools":[{"type":"image_generation"}]
	}`))

	require.Nil(t, err)
	require.NotContains(t, string(prepared), "image_generation")
	require.NotContains(t, string(prepared), "allowed_tools")
	require.Contains(t, string(prepared), `"tool_choice":"auto"`)
}

func TestPrepareImageGenerationDisabledJSONBodyStripsAlternateModalityKeys(t *testing.T) {
	prepared, err := PrepareImageGenerationDisabledJSONBody([]byte(`{
		"model":"gpt-4o",
		"messages":[{"role":"user","content":"hi"}],
		"output_modalities":["text","image"],
		"response_modalities":["IMAGE","TEXT"]
	}`))

	require.Nil(t, err)
	require.Contains(t, string(prepared), `"output_modalities":["text"]`)
	require.Contains(t, string(prepared), `"response_modalities":["TEXT"]`)
	require.Contains(t, string(prepared), "暂不支持生图")
}
