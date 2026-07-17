package sora

import (
	"net/http/httptest"
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
