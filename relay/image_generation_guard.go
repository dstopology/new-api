package relay

import (
	"errors"
	"net/http"
	"strings"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

const (
	imageGenerationDisabledMessage = "image generation is disabled on this channel"
	imageGenerationToolPrefix      = "image_generation"
)

var blockedImageGenerationModelHints = []string{
	"dall-e",
	"gpt-image",
	"qwen-image",
	"z-image",
	"imagen-",
	"image-generation",
	"gemini-2.0-flash-exp-image-generation",
	"gemini-2.5-flash-image",
	"gemini-3-pro-image",
	"gemini-3.1-flash-image",
	"flux-",
	"flux.1-",
	"stable-diffusion",
	"sdxl",
	"ideogram",
	"recraft",
	"midjourney",
}

func ImageGenerationDisabledAPIError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(imageGenerationDisabledMessage),
		types.ErrorCodeImageGenerationDisabled,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
	)
}

func ImageGenerationDisabledTaskError() *dto.TaskError {
	return service.TaskErrorWrapperLocal(
		errors.New(imageGenerationDisabledMessage),
		string(types.ErrorCodeImageGenerationDisabled),
		http.StatusForbidden,
	)
}

func RejectImageGenerationRequest(info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil {
		return nil
	}
	if !ChannelDisablesImageGeneration(info) {
		return nil
	}
	if info.RelayFormat == types.RelayFormatOpenAIImage || IsImageGenerationRelayMode(info.RelayMode) {
		return ImageGenerationDisabledAPIError()
	}
	if isBlockedImageGenerationModel(info.OriginModelName) || isBlockedImageGenerationModel(info.UpstreamModelName) {
		return ImageGenerationDisabledAPIError()
	}
	if requestUsesImageGeneration(info.Request) {
		return ImageGenerationDisabledAPIError()
	}
	return nil
}

func RejectImageGenerationJSONBody(data []byte) *types.NewAPIError {
	if JSONBodyUsesImageGeneration(data) {
		return ImageGenerationDisabledAPIError()
	}
	return nil
}

func ChannelDisablesImageGeneration(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelSetting.DisableImageGeneration
}

func IsImageGenerationRelayMode(relayMode int) bool {
	return relayMode == relayconstant.RelayModeImagesGenerations ||
		relayMode == relayconstant.RelayModeImagesEdits ||
		relayMode == relayconstant.RelayModeEdits
}

func IsMidjourneyGenerationMode(relayMode int) bool {
	switch relayMode {
	case relayconstant.RelayModeMidjourneyImagine,
		relayconstant.RelayModeMidjourneyBlend,
		relayconstant.RelayModeMidjourneyChange,
		relayconstant.RelayModeMidjourneySimpleChange,
		relayconstant.RelayModeMidjourneyAction,
		relayconstant.RelayModeMidjourneyModal,
		relayconstant.RelayModeSwapFace,
		relayconstant.RelayModeMidjourneyEdits:
		return true
	default:
		return false
	}
}

func JSONBodyUsesImageGeneration(data []byte) bool {
	var body map[string]any
	if err := appcommon.Unmarshal(data, &body); err != nil {
		return false
	}
	if isBlockedImageGenerationModel(appcommon.Interface2String(body["model"])) {
		return true
	}
	return rawValueUsesImageGenerationTool(body["tools"]) ||
		rawValueUsesImageGenerationTool(body["tool_choice"]) ||
		rawValueHasImageOutputModality(body["modalities"]) ||
		rawValueHasImageOutputModality(body["output_modalities"]) ||
		rawValueHasImageOutputModality(body["response_modalities"])
}

func ResponsesRequestUsesImageGeneration(request *dto.OpenAIResponsesRequest) bool {
	if request == nil {
		return false
	}
	if isBlockedImageGenerationModel(request.Model) {
		return true
	}
	return RawJSONUsesImageGenerationTool(request.Tools) ||
		RawJSONUsesImageGenerationTool(request.ToolChoice)
}

func GeneralOpenAIRequestUsesImageGeneration(request *dto.GeneralOpenAIRequest) bool {
	if request == nil {
		return false
	}
	if isBlockedImageGenerationModel(request.Model) {
		return true
	}
	if RawJSONHasImageOutputModality(request.Modalities) {
		return true
	}
	for _, tool := range request.Tools {
		if isImageGenerationToolType(tool.Type) {
			return true
		}
	}
	return rawValueUsesImageGenerationTool(request.ToolChoice)
}

func RawJSONUsesImageGenerationTool(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := appcommon.Unmarshal(raw, &value); err != nil {
		return false
	}
	return rawValueUsesImageGenerationTool(value)
}

func RawJSONHasImageOutputModality(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := appcommon.Unmarshal(raw, &value); err != nil {
		return false
	}
	return rawValueHasImageOutputModality(value)
}

func requestUsesImageGeneration(request dto.Request) bool {
	switch r := request.(type) {
	case *dto.ImageRequest:
		return true
	case *dto.OpenAIResponsesRequest:
		return ResponsesRequestUsesImageGeneration(r)
	case *dto.GeneralOpenAIRequest:
		return GeneralOpenAIRequestUsesImageGeneration(r)
	default:
		return false
	}
}

func rawValueUsesImageGenerationTool(value any) bool {
	switch v := value.(type) {
	case string:
		return isImageGenerationToolType(v)
	case []any:
		for _, item := range v {
			if rawValueUsesImageGenerationTool(item) {
				return true
			}
		}
	case map[string]any:
		if isImageGenerationToolType(appcommon.Interface2String(v["type"])) {
			return true
		}
		if tools, ok := v["tools"]; ok && rawValueUsesImageGenerationTool(tools) {
			return true
		}
		if toolChoice, ok := v["tool_choice"]; ok && rawValueUsesImageGenerationTool(toolChoice) {
			return true
		}
	}
	return false
}

func rawValueHasImageOutputModality(value any) bool {
	switch v := value.(type) {
	case string:
		modality := strings.ToLower(strings.TrimSpace(v))
		return modality == "image" || modality == "images"
	case []any:
		for _, item := range v {
			if rawValueHasImageOutputModality(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if rawValueHasImageOutputModality(item) {
				return true
			}
		}
	}
	return false
}

func isImageGenerationToolType(toolType string) bool {
	toolType = strings.ToLower(strings.TrimSpace(toolType))
	return strings.HasPrefix(toolType, imageGenerationToolPrefix)
}

func isBlockedImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "" {
		return false
	}
	if appcommon.IsImageGenerationModel(modelName) {
		return true
	}
	for _, hint := range blockedImageGenerationModelHints {
		if strings.Contains(modelName, hint) {
			return true
		}
	}
	return false
}
