package openai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	maxResponsesPreludeEvents = 64
	maxResponsesPreludeBytes  = 512 << 10
)

type responsesBufferedEvent struct {
	response dto.ResponsesStreamResponse
	data     string
}

type responsesStreamTracker struct {
	completionFallback *responsesStreamCompletionFallback

	responseID         string
	responseModel      string
	responseCreatedAt  int64
	lastEventType      string
	hasSequenceNumber  bool
	lastSequenceNumber int64

	receivedEvents   int
	forwardedEvents  int
	duplicateEvents  int
	meaningfulEvents int
	outputTextBytes  int
	reasoningBytes   int
	toolCallBytes    int
	resumeAttempts   int
	resumeStatus     string

	terminalSeen      bool
	terminalType      string
	terminalFailure   *types.NewAPIError
	handlerFailure    *types.NewAPIError
	downstreamStarted bool
	syntheticTerminal bool

	buffered      []responsesBufferedEvent
	bufferedBytes int
}

func newResponsesStreamTracker() *responsesStreamTracker {
	return &responsesStreamTracker{
		completionFallback: newResponsesStreamCompletionFallback(),
		lastSequenceNumber: -1,
	}
}

func (t *responsesStreamTracker) observe(data string, response dto.ResponsesStreamResponse) (bool, error) {
	t.receivedEvents++
	if response.SequenceNumber != nil {
		sequenceNumber := *response.SequenceNumber
		if t.hasSequenceNumber && sequenceNumber <= t.lastSequenceNumber {
			t.duplicateEvents++
			return true, nil
		}
		t.hasSequenceNumber = true
		t.lastSequenceNumber = sequenceNumber
	}

	t.lastEventType = response.Type
	if response.Response != nil {
		if response.Response.ID != "" {
			t.responseID = response.Response.ID
		}
		if response.Response.Model != "" {
			t.responseModel = response.Response.Model
		}
		if response.Response.CreatedAt > 0 {
			t.responseCreatedAt = int64(response.Response.CreatedAt)
		}
	}
	if err := t.completionFallback.Observe(data); err != nil {
		return false, err
	}

	switch response.Type {
	case "response.output_text.delta":
		t.outputTextBytes += len(response.Delta)
	case "response.function_call_arguments.delta":
		t.toolCallBytes += len(response.Delta)
	default:
		if strings.Contains(response.Type, "reasoning") && strings.HasSuffix(response.Type, ".delta") {
			t.reasoningBytes += len(response.Delta)
		}
	}
	if isMeaningfulResponsesEvent(response.Type) {
		t.meaningfulEvents++
	}
	return false, nil
}

func (t *responsesStreamTracker) forward(c *gin.Context, response dto.ResponsesStreamResponse, data string) {
	meaningful := isMeaningfulResponsesEvent(response.Type)
	if !t.downstreamStarted && !meaningful &&
		len(t.buffered) < maxResponsesPreludeEvents &&
		t.bufferedBytes+len(data) <= maxResponsesPreludeBytes {
		t.buffered = append(t.buffered, responsesBufferedEvent{response: response, data: data})
		t.bufferedBytes += len(data)
		return
	}

	if !t.downstreamStarted {
		t.downstreamStarted = true
		helper.MarkResponsesStreamStarted(c)
		for _, event := range t.buffered {
			sendResponsesStreamData(c, event.response, event.data)
			t.forwardedEvents++
		}
		t.buffered = nil
		t.bufferedBytes = 0
	}
	sendResponsesStreamData(c, response, data)
	t.forwardedEvents++
}

func (t *responsesStreamTracker) markTerminal(c *gin.Context, eventType string, failure *types.NewAPIError) {
	t.terminalSeen = true
	t.terminalType = eventType
	t.terminalFailure = failure
	if t.downstreamStarted {
		helper.MarkResponsesStreamTerminalSent(c)
	}
}

func (t *responsesStreamTracker) finalize(status *relaycommon.StreamStatus) error {
	t.applyStatus(status)
	if t.terminalSeen {
		if t.terminalFailure == nil && status != nil {
			status.ReplaceEndReason(relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonDone, nil)
		}
		return nil
	}
	if t.completionFallback.CanSynthesize() {
		if status != nil {
			status.ReplaceEndReason(relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonDone, nil)
		}
		return nil
	}
	if status == nil || status.EndReason == relaycommon.StreamEndReasonClientGone {
		return nil
	}
	return t.interruptionError()
}

func (t *responsesStreamTracker) interruptionError() error {
	cursor := "none"
	if t.hasSequenceNumber {
		cursor = fmt.Sprintf("%d", t.lastSequenceNumber)
	}
	return fmt.Errorf(
		"responses stream ended without a terminal event (last_event=%q, response_id_present=%t, sequence=%s, received=%d)",
		t.lastEventType,
		t.responseID != "",
		cursor,
		t.receivedEvents,
	)
}

func (t *responsesStreamTracker) applyStatus(status *relaycommon.StreamStatus) {
	if status == nil {
		return
	}
	status.SetDetail("protocol", "responses")
	status.SetDetail("received_events", t.receivedEvents)
	status.SetDetail("forwarded_events", t.forwardedEvents)
	status.SetDetail("meaningful_events", t.meaningfulEvents)
	status.SetDetail("downstream_started", t.downstreamStarted)
	status.SetDetail("last_event_type", t.lastEventType)
	status.SetDetail("output_text_bytes", t.outputTextBytes)
	status.SetDetail("reasoning_bytes", t.reasoningBytes)
	status.SetDetail("tool_call_bytes", t.toolCallBytes)
	if t.responseID != "" {
		status.SetDetail("response_id", t.responseID)
	}
	if t.hasSequenceNumber {
		status.SetDetail("last_sequence_number", t.lastSequenceNumber)
	}
	if t.duplicateEvents > 0 {
		status.SetDetail("duplicate_events", t.duplicateEvents)
	}
	if t.resumeAttempts > 0 {
		status.SetDetail("resume_attempts", t.resumeAttempts)
		status.SetDetail("resume_status", t.resumeStatus)
	}
	if t.terminalType != "" {
		status.SetDetail("terminal_event", t.terminalType)
	}
	if t.syntheticTerminal {
		status.SetDetail("synthetic_terminal", true)
	}
}

func (t *responsesStreamTracker) sendFailure(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) error {
	if apiErr == nil {
		apiErr = newResponsesInterruptedAPIError(t.interruptionError())
	}
	sequenceNumber := int64(0)
	if t.hasSequenceNumber {
		sequenceNumber = t.lastSequenceNumber + 1
	}
	model := t.responseModel
	if model == "" {
		model = responsesFallbackModel(info)
	}
	createdAt := t.responseCreatedAt
	if createdAt == 0 && info != nil && !info.StartTime.IsZero() {
		createdAt = info.StartTime.Unix()
	}
	if err := helper.ResponsesFailureData(c, t.responseID, sequenceNumber, model, createdAt, apiErr.ToOpenAIError()); err != nil {
		return err
	}
	t.syntheticTerminal = true
	t.terminalSeen = true
	t.terminalType = "response.failed"
	t.terminalFailure = apiErr
	t.forwardedEvents++
	if info != nil {
		t.applyStatus(info.StreamStatus)
	}
	return nil
}

func isMeaningfulResponsesEvent(eventType string) bool {
	switch eventType {
	case "response.created", "response.queued", "response.in_progress":
		return false
	default:
		return true
	}
}

func isResponsesSuccessTerminal(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done":
		return true
	default:
		return false
	}
}

func isResponsesIncompleteTerminal(eventType string) bool {
	return eventType == "response.incomplete"
}

func isResponsesFailureTerminal(eventType string) bool {
	switch eventType {
	case "response.failed", "response.error", "response.cancelled", "response.canceled", "error":
		return true
	default:
		return false
	}
}

func responsesFailureFromEvent(response dto.ResponsesStreamResponse) *types.NewAPIError {
	var openAIError *types.OpenAIError
	if response.Response != nil {
		openAIError = response.Response.GetOpenAIError()
	}
	if openAIError == nil {
		openAIError = dto.GetOpenAIError(response.Error)
	}
	if openAIError == nil && response.Message != "" {
		openAIError = &types.OpenAIError{
			Message: response.Message,
			Type:    "upstream_error",
			Param:   response.Param,
			Code:    response.Code,
		}
	}
	if openAIError == nil {
		openAIError = &types.OpenAIError{
			Message: fmt.Sprintf("upstream emitted terminal event %s", response.Type),
			Type:    "upstream_error",
			Code:    response.Type,
		}
	}
	if openAIError.Message == "" {
		openAIError.Message = fmt.Sprintf("upstream emitted terminal event %s", response.Type)
	}
	return types.WithOpenAIError(*openAIError, http.StatusBadGateway)
}

func newResponsesInterruptedAPIError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeUpstreamStreamInterrupted, http.StatusBadGateway)
}
