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

// RejectImageGenerationRequest hard-rejects only the dedicated image endpoints
// (/v1/images/*), which are one-shot requests with no conversational session to
// keep alive, so a 403 there is harmless.
//
// Chat / Responses requests are intentionally NOT rejected here. Returning a 403
// to a live agent session kills the session: the client keeps the conversation
// context and retries the same request forever. Those paths are degraded
// gracefully instead — see PrepareImageGenerationDisabledJSONBody, which strips
// the image_generation capability before the request reaches the upstream and
// tells the model to decline in text.
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
	return nil
}

// PrepareImageGenerationDisabledJSONBody sanitizes an outbound request body so the
// upstream can never produce an image: it strips image_generation tool
// declarations, neutralizes any tool_choice that forces image generation, drops
// image output modalities, and injects an instruction telling the model to decline
// in text. It never rejects the request — the call is always allowed through
// (text-only), which keeps the client session alive instead of triggering an
// endless retry loop on a 403.
func PrepareImageGenerationDisabledJSONBody(data []byte) ([]byte, *types.NewAPIError) {
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
	choiceNeutralized := neutralizeImageGenerationToolChoice(body)
	modalityStripped := stripImageOutputModalities(body)
	sanitized := sanitizeImageGenerationInstructionFields(body)
	injected := false
	if removed || choiceNeutralized || modalityStripped {
		injected = injectImageGenerationDisabledNotice(body)
	}
	if !removed && !choiceNeutralized && !modalityStripped && !sanitized && !injected {
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
	current, ok := body["instructions"].(string)
	if !ok {
		// Absent → set the notice. Present but non-string (array/object) → leave the
		// structured value intact instead of stringifying it, which would corrupt the
		// request body.
		if _, present := body["instructions"]; present {
			return false
		}
		body["instructions"] = imageGenerationDisabledNotice
		return true
	}
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
		// Already carries the notice → no-op (idempotent).
		if messageContentContainsText(msg["content"], imageGenerationDisabledNotice) {
			return false
		}
		switch content := msg["content"].(type) {
		case string:
			if strings.TrimSpace(content) == "" {
				msg["content"] = imageGenerationDisabledNotice
			} else {
				msg["content"] = imageGenerationDisabledNotice + "\n\n" + content
			}
		case []any:
			// Multimodal content: prepend a text part instead of stringifying the
			// array (which would corrupt the request body).
			msg["content"] = append([]any{map[string]any{
				"type": "text",
				"text": imageGenerationDisabledNotice,
			}}, content...)
		default:
			msg["content"] = imageGenerationDisabledNotice
		}
		return true
	}
	// No system/developer message present → prepend a fresh one.
	body["messages"] = append([]any{map[string]any{
		"role":    "system",
		"content": imageGenerationDisabledNotice,
	}}, messages...)
	return true
}

// messageContentContainsText reports whether a chat message content (a string, or
// a multimodal array of content parts) already contains substr in its text.
func messageContentContainsText(content any, substr string) bool {
	switch v := content.(type) {
	case string:
		return strings.Contains(v, substr)
	case []any:
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok && strings.Contains(text, substr) {
				return true
			}
		}
	}
	return false
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

// neutralizeImageGenerationToolChoice downgrades any tool_choice that would force
// the upstream to generate an image to "auto". It runs after the image_generation
// tool declarations have already been stripped, so an allowed_tools choice whose
// only entry was image_generation (now empty) is also downgraded — an empty
// allowed_tools list would otherwise be rejected by the upstream.
func neutralizeImageGenerationToolChoice(value any) bool {
	switch v := value.(type) {
	case []any:
		changed := false
		for _, item := range v {
			if neutralizeImageGenerationToolChoice(item) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range v {
			if isToolChoiceKey(key) && shouldDowngradeImageGenerationToolChoice(item) {
				v[key] = "auto"
				changed = true
				continue
			}
			if neutralizeImageGenerationToolChoice(item) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func isToolChoiceKey(key string) bool {
	return strings.ToLower(strings.TrimSpace(key)) == "tool_choice"
}

func shouldDowngradeImageGenerationToolChoice(value any) bool {
	switch v := value.(type) {
	case string:
		return isImageGenerationToolType(v)
	case map[string]any:
		choiceType := strings.ToLower(strings.TrimSpace(appcommon.Interface2String(v["type"])))
		// Bare {"type":"image_generation"} directly forces the built-in image tool.
		if isImageGenerationToolType(choiceType) {
			return true
		}
		// {"type":"function","function":{"name":"image_generation"}}
		if function, ok := v["function"].(map[string]any); ok &&
			isImageGenerationToolType(appcommon.Interface2String(function["name"])) {
			return true
		}
		// allowed_tools whose image_generation entry was just stripped, leaving the
		// list empty or absent — an empty allowed_tools choice is invalid upstream.
		if choiceType == "allowed_tools" {
			if tools, ok := v["tools"].([]any); !ok || len(tools) == 0 {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// stripImageOutputModalities removes "image" from any modalities list so the model
// is asked for text-only output (e.g. gpt-4o image output via chat completions).
func stripImageOutputModalities(value any) bool {
	switch v := value.(type) {
	case []any:
		changed := false
		for _, item := range v {
			if stripImageOutputModalities(item) {
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range v {
			if isModalityKey(key) {
				if filtered, ok := filterImageModalityList(item); ok {
					v[key] = filtered
					changed = true
				}
				continue
			}
			if stripImageOutputModalities(item) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func isModalityKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "modalities", "output_modalities", "response_modalities":
		return true
	default:
		return false
	}
}

func filterImageModalityList(value any) (any, bool) {
	list, ok := value.([]any)
	if !ok {
		return value, false
	}
	filtered := make([]any, 0, len(list))
	removed := false
	for _, item := range list {
		modality := strings.ToLower(strings.TrimSpace(appcommon.Interface2String(item)))
		if modality == "image" || modality == "images" {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		return value, false
	}
	if len(filtered) == 0 {
		filtered = append(filtered, "text")
	}
	return filtered, true
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
