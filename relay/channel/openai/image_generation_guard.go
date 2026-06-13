package openai

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

const imageGenerationDisabledMessage = "image generation is disabled on this channel"

func imageGenerationDisabledAPIError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(imageGenerationDisabledMessage),
		types.ErrorCodeImageGenerationDisabled,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
	)
}

func channelDisablesImageGeneration(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelSetting.DisableImageGeneration
}

func responsesResponseUsesImageGeneration(resp *dto.OpenAIResponsesResponse) bool {
	if resp == nil {
		return false
	}
	if resp.HasImageGenerationCall() {
		return true
	}
	for _, tool := range resp.Tools {
		if isImageGenerationResponseType(common.Interface2String(tool["type"])) {
			return true
		}
	}
	return false
}

func responsesStreamUsesImageGeneration(streamResp *dto.ResponsesStreamResponse) bool {
	if streamResp == nil {
		return false
	}
	if isImageGenerationResponseEvent(streamResp.Type) {
		return true
	}
	if streamResp.Item != nil && isImageGenerationResponseType(streamResp.Item.Type) {
		return true
	}
	return responsesResponseUsesImageGeneration(streamResp.Response)
}

func isImageGenerationResponseEvent(eventType string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	return strings.HasPrefix(eventType, "image_generation.") ||
		strings.HasPrefix(eventType, "response.image_generation")
}

func isImageGenerationResponseType(outputType string) bool {
	outputType = strings.ToLower(strings.TrimSpace(outputType))
	return strings.HasPrefix(outputType, "image_generation")
}
