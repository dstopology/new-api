package sora

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEstimateBillingUsesVideoDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		request     relaycommon.TaskSubmitReq
		wantSeconds float64
		wantSize    float64
	}{
		{
			name:        "seconds field",
			request:     relaycommon.TaskSubmitReq{Seconds: "6", Size: "720x1280"},
			wantSeconds: 6,
			wantSize:    1,
		},
		{
			name:        "duration fallback and large resolution",
			request:     relaycommon.TaskSubmitReq{Duration: 8, Size: "1792x1024"},
			wantSeconds: 8,
			wantSize:    1.666667,
		},
		{
			name:        "defaults",
			request:     relaycommon.TaskSubmitReq{},
			wantSeconds: 4,
			wantSize:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Set("task_request", test.request)

			ratios := (&TaskAdaptor{}).EstimateBilling(context, &relaycommon.RelayInfo{
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			})
			require.InDelta(t, test.wantSeconds, ratios["seconds"], 1e-9)
			require.InDelta(t, test.wantSize, ratios["size"], 1e-9)
		})
	}
}

func TestDoResponseStoresOnlyPublicTaskData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"upstream-private-id",
			"task_id":"upstream-private-id",
			"object":"video.generation",
			"model":"veo-3-1-fast",
			"status":"running",
			"progress":1,
			"video_url":"https://upstream.example/private.mp4",
			"metadata":{"url":"https://upstream.example/private.mp4"}
		}`)),
	}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "upstream-private-id", upstreamID)
	require.Contains(t, string(taskData), `"id":"task_public"`)
	require.Contains(t, string(taskData), `"task_id":"task_public"`)
	require.Contains(t, string(taskData), `"status":"in_progress"`)
	require.NotContains(t, string(taskData), "upstream-private-id")
	require.NotContains(t, string(taskData), "upstream.example")
	require.Equal(t, string(taskData), recorder.Body.String())
}

func TestParseTaskResultTreatsRunningAsInProgress(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"running","progress":12}`))
	require.NoError(t, err)
	require.Equal(t, "IN_PROGRESS", result.Status)
	require.Equal(t, "12%", result.Progress)
}
