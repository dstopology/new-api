package relay

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func applyUserPromptAddition(info *relaycommon.RelayInfo, request any) (bool, error) {
	if info == nil || !info.ChannelSetting.UserPromptAdditionEnabled {
		return false, nil
	}
	addition := strings.TrimSpace(info.ChannelSetting.UserPromptAddition)
	if addition == "" {
		return false, nil
	}

	switch typed := request.(type) {
	case *dto.GeneralOpenAIRequest:
		prependGeneralOpenAIUserPrompt(typed, addition)
	case *dto.ClaudeRequest:
		if err := prependClaudeUserPrompt(typed, addition); err != nil {
			return false, err
		}
	case *dto.GeminiChatRequest:
		prependGeminiUserPrompt(typed, addition)
	case *dto.OpenAIResponsesRequest:
		if err := prependResponsesUserPrompt(typed, addition); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported request type for user prompt addition: %T", request)
	}
	return true, nil
}

func prependText(addition, original string) string {
	if original == "" {
		return addition
	}
	return addition + "\n" + original
}

func prependGeneralOpenAIUserPrompt(request *dto.GeneralOpenAIRequest, addition string) {
	if request == nil {
		return
	}
	if len(request.Messages) == 0 {
		prependLegacyPrompt(&request.Prompt, addition)
		return
	}

	for i := range request.Messages {
		message := &request.Messages[i]
		if message.Role != "user" {
			continue
		}
		if message.IsStringContent() {
			message.SetStringContent(prependText(addition, message.StringContent()))
			continue
		}
		contents := message.ParseContent()
		contents = append([]dto.MediaContent{{Type: dto.ContentTypeText, Text: addition}}, contents...)
		message.SetMediaContent(contents)
	}
}

func prependLegacyPrompt(prompt *any, addition string) {
	if prompt == nil || *prompt == nil {
		return
	}
	switch value := (*prompt).(type) {
	case string:
		*prompt = prependText(addition, value)
	case []string:
		for i := range value {
			value[i] = prependText(addition, value[i])
		}
		*prompt = value
	case []any:
		for i, item := range value {
			if text, ok := item.(string); ok {
				value[i] = prependText(addition, text)
			}
		}
		*prompt = value
	}
}

func prependClaudeUserPrompt(request *dto.ClaudeRequest, addition string) error {
	if request == nil {
		return nil
	}
	for i := range request.Messages {
		message := &request.Messages[i]
		if message.Role != "user" {
			continue
		}
		if message.IsStringContent() {
			message.SetStringContent(prependText(addition, message.GetStringContent()))
			continue
		}
		contents, err := message.ParseContent()
		if err != nil {
			return fmt.Errorf("parse Claude user content: %w", err)
		}
		textPart := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
		textPart.SetText(addition)
		message.SetContent(append([]dto.ClaudeMediaMessage{textPart}, contents...))
	}
	return nil
}

func prependGeminiUserPrompt(request *dto.GeminiChatRequest, addition string) {
	if request == nil {
		return
	}
	for i := range request.Requests {
		prependGeminiUserPrompt(&request.Requests[i], addition)
	}
	for i := range request.Contents {
		content := &request.Contents[i]
		if content.Role != "user" && !(i == 0 && content.Role == "") {
			continue
		}
		content.Parts = append([]dto.GeminiPart{{Text: addition}}, content.Parts...)
	}
}

func prependResponsesUserPrompt(request *dto.OpenAIResponsesRequest, addition string) error {
	if request == nil || len(request.Input) == 0 {
		return nil
	}

	var input any
	if err := common.Unmarshal(request.Input, &input); err != nil {
		return fmt.Errorf("parse Responses input: %w", err)
	}
	input = prependResponsesInput(input, addition)
	data, err := common.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal Responses input: %w", err)
	}
	request.Input = data
	return nil
}

func prependResponsesInput(input any, addition string) any {
	switch value := input.(type) {
	case string:
		return prependText(addition, value)
	case []any:
		hasUserMessage := false
		for _, item := range value {
			message, ok := item.(map[string]any)
			if !ok || message["role"] != "user" {
				continue
			}
			hasUserMessage = true
			message["content"] = prependResponsesContent(message["content"], addition)
		}
		if hasUserMessage {
			return value
		}
		return append([]any{map[string]any{"type": "input_text", "text": addition}}, value...)
	default:
		return input
	}
}

func prependResponsesContent(content any, addition string) any {
	switch value := content.(type) {
	case nil:
		return addition
	case string:
		return prependText(addition, value)
	case []any:
		return append([]any{map[string]any{"type": "input_text", "text": addition}}, value...)
	default:
		return content
	}
}
