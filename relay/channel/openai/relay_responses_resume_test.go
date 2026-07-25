package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesStreamHandlerResumesBackgroundResponse(t *testing.T) {
	service.InitHttpClient()
	previousInterval := common.RetryIntervalMilliseconds
	common.RetryIntervalMilliseconds = 0
	defer func() {
		common.RetryIntervalMilliseconds = previousInterval
	}()

	var resumedMethod string
	var resumedPath string
	var resumedQuery string
	var resumedAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		resumedMethod = request.Method
		resumedPath = request.URL.Path
		resumedQuery = request.URL.RawQuery
		resumedAuthorization = request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, `data: {"type":"response.output_text.delta","sequence_number":1,"output_index":0,"content_index":0,"item_id":"msg_1","delta":"OK"}

data: {"type":"response.completed","sequence_number":2,"response":{"id":"resp_test","object":"response","created_at":123,"status":"completed","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}

`)
	}))
	defer server.Close()

	baseRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", nil)
	require.NoError(t, err)
	baseRequest.Header.Set("Authorization", "Bearer test-key")

	initialBody := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

`
	upstreamResponse := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(initialBody)),
		Request:    baseRequest,
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		DisablePing:     true,
		StartTime:       time.Now(),
		OriginModelName: "gpt-5.6-sol",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools:        map[string]*relaycommon.BuildInToolInfo{},
			Background:          true,
			StreamResumeEnabled: true,
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.6-sol",
		},
	}

	usage, newAPIError := OaiResponsesStreamHandler(ctx, info, upstreamResponse)

	require.Nil(t, newAPIError)
	require.Equal(t, 2, usage.PromptTokens)
	require.Equal(t, 1, usage.CompletionTokens)
	require.Equal(t, 3, usage.TotalTokens)
	require.Equal(t, http.MethodGet, resumedMethod)
	require.Equal(t, "/v1/responses/resp_test", resumedPath)
	require.Contains(t, resumedQuery, "starting_after=0")
	require.Contains(t, resumedQuery, "stream=true")
	require.Equal(t, "Bearer test-key", resumedAuthorization)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"response.created"`))
	require.Contains(t, recorder.Body.String(), `"delta":"OK"`)
	require.Contains(t, recorder.Body.String(), "event: response.completed")
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.EqualValues(t, 1, info.StreamStatus.DetailsSnapshot()["resume_attempts"])
}

func TestBuildResponsesResumeRequestPreservesHeadersAndEscapesID(t *testing.T) {
	baseRequest, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses?api-version=preview", nil)
	require.NoError(t, err)
	baseRequest.Header.Set("Authorization", "Bearer test-key")
	baseRequest.Header.Set("Content-Type", "application/json")

	request, err := buildResponsesResumeRequest(nil, baseRequest, "resp_test", 42)

	require.NoError(t, err)
	require.Equal(t, http.MethodGet, request.Method)
	require.Equal(t, "/v1/responses/resp_test", request.URL.Path)
	require.Equal(t, "preview", request.URL.Query().Get("api-version"))
	require.Equal(t, "true", request.URL.Query().Get("stream"))
	require.Equal(t, "42", request.URL.Query().Get("starting_after"))
	require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
	require.Empty(t, request.Header.Get("Content-Type"))
	require.Equal(t, "text/event-stream", request.Header.Get("Accept"))
}

func TestOaiResponsesStreamHandlerCapsResumeAttemptsAtThree(t *testing.T) {
	service.InitHttpClient()
	previousInterval := common.RetryIntervalMilliseconds
	common.RetryIntervalMilliseconds = 0
	defer func() {
		common.RetryIntervalMilliseconds = previousInterval
	}()

	resumeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		resumeRequests++
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, `{"error":{"type":"server_error","code":"server_error","message":"temporarily unavailable"}}`)
	}))
	defer server.Close()

	baseRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", nil)
	require.NoError(t, err)
	initialBody := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","created_at":123,"status":"in_progress","model":"gpt-5.6-sol","output":[]}}

`
	upstreamResponse := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(initialBody)),
		Request:    baseRequest,
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		DisablePing:     true,
		StartTime:       time.Now(),
		OriginModelName: "gpt-5.6-sol",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools:        map[string]*relaycommon.BuildInToolInfo{},
			Background:          true,
			StreamResumeEnabled: true,
		},
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	_, newAPIError := OaiResponsesStreamHandler(ctx, info, upstreamResponse)

	require.NotNil(t, newAPIError)
	require.Equal(t, maxResponsesStreamResumeAttempts, resumeRequests)
	require.Empty(t, recorder.Body.String())
	require.EqualValues(t, maxResponsesStreamResumeAttempts, info.StreamStatus.DetailsSnapshot()["resume_attempts"])
}
