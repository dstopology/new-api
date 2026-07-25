package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// Count actual output calls, not merely the tools declared in the request.
	for _, output := range responsesResponse.Output {
		recordResponsesBuiltInToolCall(info, output.Type)
	}
	return &usage, nil
}

func recordResponsesBuiltInToolCall(info *relaycommon.RelayInfo, callType string) {
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return
	}

	var toolNames []string
	switch callType {
	case dto.BuildInCallWebSearchCall:
		// Prefer the current stable tool name while retaining preview compatibility.
		toolNames = []string{dto.BuildInToolWebSearch, dto.BuildInToolWebSearchPreview}
	case dto.BuildInCallFileSearchCall:
		toolNames = []string{dto.BuildInToolFileSearch}
	default:
		return
	}

	for _, toolName := range toolNames {
		if toolInfo, ok := info.ResponsesUsageInfo.BuiltInTools[toolName]; ok && toolInfo != nil {
			toolInfo.CallCount++
			return
		}
	}
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	tracker := newResponsesStreamTracker()
	baseRequest := resp.Request
	currentResponse := resp

	for {
		info.StreamStatus = relaycommon.NewStreamStatus()
		tracker.handlerFailure = nil

		helper.StreamScannerHandler(c, currentResponse, info, func(data string, sr *helper.StreamResult) {
			var streamResponse dto.ResponsesStreamResponse
			if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
				logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
				tracker.handlerFailure = newResponsesInterruptedAPIError(
					fmt.Errorf("invalid responses stream event: %w", err),
				)
				sr.Stop(tracker.handlerFailure)
				return
			}

			duplicate, err := tracker.observe(data, streamResponse)
			if err != nil {
				logger.LogError(c, "failed to observe responses stream event: "+err.Error())
				tracker.handlerFailure = newResponsesInterruptedAPIError(
					fmt.Errorf("invalid responses stream event: %w", err),
				)
				sr.Stop(tracker.handlerFailure)
				return
			}
			if duplicate {
				return
			}

			if isResponsesFailureTerminal(streamResponse.Type) {
				failure := responsesFailureFromEvent(streamResponse)
				if tracker.downstreamStarted {
					tracker.forward(c, streamResponse, data)
				}
				tracker.markTerminal(c, streamResponse.Type, failure)
				sr.Stop(failure)
				return
			}

			tracker.forward(c, streamResponse, data)
			switch {
			case isResponsesSuccessTerminal(streamResponse.Type):
				applyResponsesStreamResponse(c, usage, streamResponse.Response)
				tracker.markTerminal(c, streamResponse.Type, nil)
				sr.Done()
			case isResponsesIncompleteTerminal(streamResponse.Type):
				applyResponsesStreamResponse(c, usage, streamResponse.Response)
				tracker.markTerminal(c, streamResponse.Type, nil)
				sr.Done()
			case streamResponse.Type == "response.output_text.delta":
				responseTextBuilder.WriteString(streamResponse.Delta)
			case streamResponse.Type == dto.ResponsesOutputTypeItemDone:
				if streamResponse.Item != nil {
					recordResponsesBuiltInToolCall(info, streamResponse.Item.Type)
				}
			}
		}, helper.StreamScannerOptions{
			Finalize: tracker.finalize,
		})

		tracker.applyStatus(info.StreamStatus)
		if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
			break
		}
		if tracker.handlerFailure != nil {
			if tracker.downstreamStarted && !helper.IsStreamDownstreamGone(c) {
				if err := tracker.sendFailure(c, info, tracker.handlerFailure); err != nil {
					logger.LogError(c, "failed to send responses stream failure: "+err.Error())
				}
			}
			return nil, tracker.handlerFailure
		}
		if tracker.terminalFailure != nil {
			return nil, tracker.terminalFailure
		}
		if tracker.terminalSeen {
			break
		}

		if tracker.completionFallback.ShouldSynthesize(info) && !helper.IsStreamDownstreamGone(c) {
			if err := tracker.completionFallback.SendCompleted(c, info); err != nil {
				logger.LogError(c, "failed to synthesize response.completed: "+err.Error())
				info.StreamStatus.RecordError(err.Error())
			} else {
				tracker.syntheticTerminal = true
				tracker.terminalSeen = true
				tracker.terminalType = responsesCompletedEventType
				helper.MarkResponsesStreamTerminalSent(c)
				tracker.applyStatus(info.StreamStatus)
				break
			}
		}

		if tracker.canResume(info, baseRequest) && tracker.resumeAttempts < maxResponsesStreamResumeAttempts {
			var resumedResponse *http.Response
			for tracker.resumeAttempts < maxResponsesStreamResumeAttempts {
				nextResponse, retryable, err := resumeResponsesStream(c, info, baseRequest, tracker)
				tracker.applyStatus(info.StreamStatus)
				if err == nil {
					resumedResponse = nextResponse
					break
				}
				logger.LogError(c, "failed to resume responses stream: "+err.Error())
				if !retryable {
					break
				}
			}
			if resumedResponse != nil {
				currentResponse = resumedResponse
				continue
			}
		}

		interrupted := newResponsesInterruptedAPIError(tracker.interruptionError())
		if tracker.downstreamStarted && !helper.IsStreamDownstreamGone(c) {
			if err := tracker.sendFailure(c, info, interrupted); err != nil {
				logger.LogError(c, "failed to send responses stream failure: "+err.Error())
			}
		}
		return nil, interrupted
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

func applyResponsesStreamResponse(c *gin.Context, usage *dto.Usage, response *dto.OpenAIResponsesResponse) {
	if usage == nil || response == nil {
		return
	}
	if response.Usage != nil {
		usage.PromptTokens = response.Usage.InputTokens
		usage.CompletionTokens = response.Usage.OutputTokens
		usage.TotalTokens = response.Usage.TotalTokens
		if response.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = response.Usage.InputTokensDetails.CachedTokens
		}
	}
	if response.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", response.GetQuality())
		c.Set("image_generation_call_size", response.GetSize())
	}
}
