package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesStreamHandlerSynthesizesCompletedForCompactionSummary(t *testing.T) {
	body := `data: {"type":"response.output_item.done","sequence_number":0,"output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"kept","annotations":[]}]}}

data: {"type":"response.output_item.done","sequence_number":1,"output_index":1,"item":{"id":"cmp_1","type":"compaction_summary","encrypted_content":"ciphertext"}}

`
	recorder, usage, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed"))

	completed := findResponsesStreamEvent(t, recorder.Body.String(), responsesCompletedEventType)
	require.EqualValues(t, 2, completed["sequence_number"])
	response, ok := completed["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "response", response["object"])
	require.Equal(t, "completed", response["status"])
	require.Equal(t, "gpt-5.6-sol", response["model"])
	require.True(t, strings.HasPrefix(common.Interface2String(response["id"]), "resp_"))

	output, ok := response["output"].([]any)
	require.True(t, ok)
	require.Len(t, output, 2)
	summary, ok := output[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "compaction_summary", summary["type"])
	require.Equal(t, "ciphertext", summary["encrypted_content"])
}

func TestOaiResponsesStreamHandlerDoesNotDuplicateCompleted(t *testing.T) {
	body := `data: {"type":"response.output_item.done","sequence_number":0,"output_index":0,"item":{"id":"cmp_1","type":"compaction_summary","encrypted_content":"ciphertext"}}

data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_upstream","object":"response","created_at":123,"status":"completed","model":"gpt-5.6-sol","output":[{"id":"cmp_1","type":"compaction_summary","encrypted_content":"ciphertext"}],"usage":null}}

`
	recorder, _, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.Nil(t, newAPIError)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed"))
	completed := findResponsesStreamEvent(t, recorder.Body.String(), responsesCompletedEventType)
	response, ok := completed["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "resp_upstream", response["id"])
}

func TestOaiResponsesStreamHandlerDoesNotCompleteRegularIncompleteStream(t *testing.T) {
	body := `data: {"type":"response.output_item.done","sequence_number":0,"output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[]}}

`
	recorder, _, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodeUpstreamStreamInterrupted, newAPIError.GetErrorCode())
	require.False(t, types.IsSkipRetryError(newAPIError))
	require.NotContains(t, recorder.Body.String(), "event: response.completed")
	require.Contains(t, recorder.Body.String(), "event: response.failed")
}

func TestOaiResponsesStreamHandlerCompatibilityAcceptsMissingTerminal(t *testing.T) {
	body := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

data: {"type":"response.output_item.done","sequence_number":1,"output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[]}}

`
	recorder, usage, newAPIError := runResponsesStreamHandlerTest(t, body, false)

	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "event: response.created")
	require.Contains(t, recorder.Body.String(), "event: response.output_item.done")
	require.NotContains(t, recorder.Body.String(), "event: response.failed")
}

func TestOaiResponsesStreamHandlerRetriesSafelyBeforeMeaningfulOutput(t *testing.T) {
	body := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

`
	recorder, _, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodeUpstreamStreamInterrupted, newAPIError.GetErrorCode())
	require.False(t, types.IsSkipRetryError(newAPIError))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerEmitsFailureAfterPartialOutput(t *testing.T) {
	body := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

data: {"type":"response.output_text.delta","sequence_number":1,"output_index":0,"content_index":0,"item_id":"msg_1","delta":"partial"}

`
	recorder, _, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodeUpstreamStreamInterrupted, newAPIError.GetErrorCode())
	require.False(t, types.IsSkipRetryError(newAPIError))
	require.Contains(t, recorder.Body.String(), `"delta":"partial"`)
	require.Contains(t, recorder.Body.String(), "event: response.failed")
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
}

func TestOaiResponsesStreamHandlerAcceptsIncompleteTerminal(t *testing.T) {
	body := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

data: {"type":"response.incomplete","sequence_number":1,"response":{"id":"resp_test","object":"response","created_at":123,"status":"incomplete","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}

`
	recorder, usage, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.Nil(t, newAPIError)
	require.Equal(t, 7, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 10, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), "event: response.incomplete")
	require.NotContains(t, recorder.Body.String(), "event: response.failed")
}

func TestOaiResponsesStreamHandlerRetriesFailureBeforeOutput(t *testing.T) {
	body := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

data: {"type":"response.failed","sequence_number":1,"response":{"id":"resp_test","object":"response","created_at":123,"status":"failed","model":"gpt-5.6-sol","error":{"code":"server_error","message":"failed upstream"},"output":[]}}

`
	recorder, _, newAPIError := runResponsesStreamHandlerTest(t, body)

	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCode("server_error"), newAPIError.GetErrorCode())
	require.False(t, types.IsSkipRetryError(newAPIError))
	require.Empty(t, recorder.Body.String())
}

func runResponsesStreamHandlerTest(t *testing.T, body string, recoveryEnabled ...bool) (*httptest.ResponseRecorder, *dto.Usage, *types.NewAPIError) {
	t.Helper()
	enableRecovery := true
	if len(recoveryEnabled) > 0 {
		enableRecovery = recoveryEnabled[0]
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-sol",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			StreamResumeEnabled: enableRecovery,
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.6-sol-openai-compact",
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")

	usage, newAPIError := OaiResponsesStreamHandler(ctx, info, resp)
	return recorder, usage, newAPIError
}

func findResponsesStreamEvent(t *testing.T, body string, eventType string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event map[string]any
		require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &event))
		if common.Interface2String(event["type"]) == eventType {
			return event
		}
	}
	t.Fatalf("event %q not found in stream: %s", eventType, body)
	return nil
}
