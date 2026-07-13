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

	require.Nil(t, newAPIError)
	require.NotContains(t, recorder.Body.String(), "event: response.completed")
}

func runResponsesStreamHandlerTest(t *testing.T, body string) (*httptest.ResponseRecorder, *dto.Usage, *types.NewAPIError) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-sol",
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
