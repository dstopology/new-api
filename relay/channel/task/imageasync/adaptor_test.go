package imageasync

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyPreservesExtensionFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{
		"model":"public-model",
		"prompt":"draw",
		"async":true,
		"aspect_ratio":"7:6",
		"seed":0,
		"watermark":false,
		"custom":{"strength":0.25}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "upstream-model"}}
	adaptor := &TaskAdaptor{}
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(encoded, &fields))
	require.JSONEq(t, `"upstream-model"`, string(fields["model"]))
	require.JSONEq(t, `true`, string(fields["async"]))
	require.JSONEq(t, `"7:6"`, string(fields["aspect_ratio"]))
	require.JSONEq(t, `0`, string(fields["seed"]))
	require.JSONEq(t, `false`, string(fields["watermark"]))
	require.JSONEq(t, `{"strength":0.25}`, string(fields["custom"]))
}

func TestDoResponseHidesUpstreamTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"upstream-secret-id",
			"object":"image.generation",
			"model":"nano-banana-pro-1k",
			"status":"queued",
			"progress":20,
			"created_at":123
		}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "nano-banana-pro-1k",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "upstream-secret-id", upstreamID)
	require.NotContains(t, string(taskData), "upstream-secret-id")
	require.Empty(t, recorder.Body.String(), "controller writes only after the task is durable")

	var response dto.AsyncImageTaskResponse
	require.NoError(t, common.Unmarshal(taskData, &response))
	require.Equal(t, "task_public", response.ID)
	require.Equal(t, "20%", response.Progress)
}

func TestParseTaskResultSupportsNumericProgress(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"upstream-id",
		"status":"completed",
		"progress":100,
		"data":[{"url":"https://example.com/image.png"}]
	}`))
	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), result.Status)
	require.Equal(t, "100%", result.Progress)
	require.Len(t, result.Images, 1)
	require.Equal(t, "https://example.com/image.png", result.Images[0].Url)
}

func TestBuildRequestURLUsesTaskAction(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://example.com"}
	url, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionImageEdits}})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/images/edits", url)
}

func TestFetchTaskTreatsRateLimitAsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	service.InitHttpClient()

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "key", map[string]any{
		"task_id": "task_upstream",
		"action":  constant.TaskActionImageGenerations,
	}, "")
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestIntegrationFetchAsyncImageTask(t *testing.T) {
	baseURL := os.Getenv("ASYNC_IMAGE_TEST_BASE_URL")
	apiKey := os.Getenv("ASYNC_IMAGE_TEST_API_KEY")
	taskID := os.Getenv("ASYNC_IMAGE_TEST_TASK_ID")
	if baseURL == "" || apiKey == "" || taskID == "" {
		t.Skip("async image integration environment is not configured")
	}

	service.InitHttpClient()
	resp, err := (&TaskAdaptor{}).FetchTask(baseURL, apiKey, map[string]any{
		"task_id": taskID,
		"action":  constant.TaskActionImageGenerations,
	}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	result, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), result.Status)
	require.NotEmpty(t, result.Images)
}
