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
	imageGenerationDisabledNotice  = "This channel does not support image generation. Any image generation is not allowed. If the user asks to create or edit images, reply in text that image generation is not supported (暂不支持生图)."
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

func PrepareImageGenerationDisabledJSONBody(data []byte) ([]byte, *types.NewAPIError) {
	if err := RejectImageGenerationJSONBody(data); err != nil {
		return nil, err
	}
	data, _, err := RewriteImageGenerationDisabledJSONBody(data)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return data, nil
}

func RewriteImageGenerationDisabledJSONBody(data []byte) ([]byte, bool, error) {
	var body any
	if err := appcommon.Unmarshal(data, &body); err != nil {
		return data, false, err
	}
	removed := removeImageGenerationToolDeclarations(body)
	sanitized := sanitizeImageGenerationInstructionFields(body)
	injected := false
	if removed {
		injected = injectImageGenerationDisabledNotice(body)
	}
	if !removed && !sanitized && !injected {
		return data, false, nil
	}
	output, err := appcommon.Marshal(body)
	if err != nil {
		return data, true, err
	}
	return output, true, nil
}

func RemoveImageGenerationToolsJSONBody(data []byte) ([]byte, bool, error) {
	var body any
	if err := appcommon.Unmarshal(data, &body); err != nil {
		return data, false, err
	}
	removed := removeImageGenerationToolDeclarations(body)
	if !removed {
		return data, false, nil
	}
	output, err := appcommon.Marshal(body)
	if err != nil {
		return data, true, err
	}
	return output, true, nil
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
	return rawValueUsesImageGenerationToolSelection(body["tool_choice"]) ||
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
	return RawJSONUsesImageGenerationToolChoice(request.ToolChoice)
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
	return rawValueUsesImageGenerationToolSelection(request.ToolChoice)
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

func RawJSONUsesImageGenerationToolChoice(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := appcommon.Unmarshal(raw, &value); err != nil {
		return false
	}
	return rawValueUsesImageGenerationToolSelection(value)
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
	return rawValueUsesImageGenerationToolValue(value, false)
}

func rawValueUsesImageGenerationToolSelection(value any) bool {
	return rawValueExplicitlySelectsImageGenerationTool(value)
}

func removeImageGenerationToolDeclarations(value any) bool {
	switch v := value.(type) {
	case []any:
		removed := false
		for _, item := range v {
			if removeImageGenerationToolDeclarations(item) {
				removed = true
			}
		}
		return removed
	case map[string]any:
		removed := false
		for key, item := range v {
			if isToolListKey(key) {
				filtered, changed := filterImageGenerationToolList(item)
				if changed {
					removed = true
					if items, ok := filtered.([]any); ok && len(items) == 0 {
						delete(v, key)
					} else {
						v[key] = filtered
					}
				}
				if next, ok := v[key]; ok && removeImageGenerationToolDeclarations(next) {
					removed = true
				}
				continue
			}
			if removeImageGenerationToolDeclarations(item) {
				removed = true
			}
		}
		return removed
	default:
		return false
	}
}

func injectImageGenerationDisabledNotice(value any) bool {
	body, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := body["input"]; ok {
		return appendImageGenerationDisabledInstructions(body)
	}
	if _, ok := body["instructions"]; ok {
		return appendImageGenerationDisabledInstructions(body)
	}
	if _, ok := body["messages"]; ok {
		return prependImageGenerationDisabledMessage(body)
	}
	return appendImageGenerationDisabledInstructions(body)
}

func appendImageGenerationDisabledInstructions(body map[string]any) bool {
	current := appcommon.Interface2String(body["instructions"])
	if strings.Contains(current, imageGenerationDisabledNotice) {
		return false
	}
	if strings.TrimSpace(current) == "" {
		body["instructions"] = imageGenerationDisabledNotice
		return true
	}
	body["instructions"] = strings.TrimRight(current, " \t\r\n") + "\n\n" + imageGenerationDisabledNotice
	return true
}

func prependImageGenerationDisabledMessage(body map[string]any) bool {
	messages, ok := body["messages"].([]any)
	if !ok {
		return false
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(appcommon.Interface2String(msg["role"])))
		if role != "system" && role != "developer" {
			continue
		}
		content := appcommon.Interface2String(msg["content"])
		if strings.Contains(content, imageGenerationDisabledNotice) {
			return false
		}
		if strings.TrimSpace(content) == "" {
			msg["content"] = imageGenerationDisabledNotice
		} else {
			msg["content"] = imageGenerationDisabledNotice + "\n\n" + content
		}
		return true
	}
	body["messages"] = append([]any{map[string]any{
		"role":    "system",
		"content": imageGenerationDisabledNotice,
	}}, messages...)
	return true
}

func sanitizeImageGenerationInstructionFields(value any) bool {
	switch v := value.(type) {
	case []any:
		changed := false
		for _, item := range v {
			if sanitizeImageGenerationInstructionFields(item) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		if sanitizeImageGenerationMessageContent(v) {
			changed = true
		}
		for key, item := range v {
			if isInstructionTextKey(key) {
				if text, ok := item.(string); ok {
					if sanitized := sanitizeImageGenerationTokenText(text); sanitized != text {
						v[key] = sanitized
						changed = true
					}
				}
				continue
			}
			if sanitizeImageGenerationInstructionFields(item) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func sanitizeImageGenerationMessageContent(message map[string]any) bool {
	role := strings.ToLower(strings.TrimSpace(appcommon.Interface2String(message["role"])))
	if role != "system" && role != "developer" {
		return false
	}
	content, ok := message["content"]
	if !ok {
		return false
	}
	return sanitizeImageGenerationContentValue(&content, func(next any) {
		message["content"] = next
	})
}

func sanitizeImageGenerationContentValue(value *any, set func(any)) bool {
	switch v := (*value).(type) {
	case string:
		sanitized := sanitizeImageGenerationTokenText(v)
		if sanitized == v {
			return false
		}
		set(sanitized)
		return true
	case []any:
		changed := false
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text, ok := part["text"].(string)
			if !ok {
				continue
			}
			sanitized := sanitizeImageGenerationTokenText(text)
			if sanitized != text {
				part["text"] = sanitized
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func sanitizeImageGenerationTokenText(text string) string {
	text = strings.ReplaceAll(text, "image_generation_call", "image generation disabled")
	text = strings.ReplaceAll(text, "image_generation", "image generation disabled")
	return text
}

func isInstructionTextKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "instructions", "system", "system_prompt", "developer", "developer_message":
		return true
	default:
		return false
	}
}

func filterImageGenerationToolList(value any) (any, bool) {
	tools, ok := value.([]any)
	if !ok {
		return value, false
	}
	filtered := make([]any, 0, len(tools))
	removed := false
	for _, tool := range tools {
		if isImageGenerationToolDeclaration(tool) {
			removed = true
			continue
		}
		filtered = append(filtered, tool)
	}
	if !removed {
		return value, false
	}
	return filtered, true
}

func isImageGenerationToolDeclaration(value any) bool {
	switch v := value.(type) {
	case string:
		return isImageGenerationToolType(v)
	case map[string]any:
		return isImageGenerationToolType(appcommon.Interface2String(v["type"]))
	default:
		return false
	}
}

func isToolListKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "tools", "allowed_tools", "available_tools", "builtin_tools", "built_in_tools":
		return true
	default:
		return false
	}
}

func rawValueExplicitlySelectsImageGenerationTool(value any) bool {
	switch v := value.(type) {
	case string:
		return isImageGenerationToolType(v)
	case map[string]any:
		if isImageGenerationToolType(appcommon.Interface2String(v["type"])) {
			return true
		}
		if tool, ok := v["tool"].(map[string]any); ok && isImageGenerationToolType(appcommon.Interface2String(tool["type"])) {
			return true
		}
		if function, ok := v["function"].(map[string]any); ok && isImageGenerationToolType(appcommon.Interface2String(function["name"])) {
			return true
		}
	}
	return false
}

func rawValueUsesImageGenerationToolValue(value any, allowBareString bool) bool {
	switch v := value.(type) {
	case string:
		return allowBareString && isImageGenerationToolType(v)
	case []any:
		for _, item := range v {
			if rawValueUsesImageGenerationToolValue(item, allowBareString) {
				return true
			}
		}
	case map[string]any:
		if isImageGenerationToolType(appcommon.Interface2String(v["type"])) {
			return true
		}
		if function, ok := v["function"].(map[string]any); ok && isImageGenerationToolType(appcommon.Interface2String(function["name"])) {
			return true
		}
		if tools, ok := v["tools"]; ok && rawValueUsesImageGenerationToolValue(tools, true) {
			return true
		}
		if toolChoice, ok := v["tool_choice"]; ok && rawValueUsesImageGenerationToolValue(toolChoice, true) {
			return true
		}
		if tool, ok := v["tool"]; ok && rawValueUsesImageGenerationToolValue(tool, true) {
			return true
		}
		for _, item := range v {
			switch item.(type) {
			case []any, map[string]any:
				if rawValueUsesImageGenerationToolValue(item, false) {
					return true
				}
			}
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
