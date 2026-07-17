package doubao

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetVideoInputRatio(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		resolution string
		hasVideo   bool
		want       float64
		wantOK     bool
	}{
		{name: "base", model: "doubao-seedance-2-0-260128", resolution: "720p", want: 1, wantOK: true},
		{name: "base video input", model: "doubao-seedance-2-0-260128", resolution: "720p", hasVideo: true, want: 28.0 / 46.0, wantOK: true},
		{name: "1080p", model: "doubao-seedance-2-0-260128", resolution: "1080P", want: 51.0 / 46.0, wantOK: true},
		{name: "1080p video input", model: "doubao-seedance-2-0-260128", resolution: "1080p", hasVideo: true, want: 31.0 / 46.0, wantOK: true},
		{name: "4k", model: "doubao-seedance-2-0-260128", resolution: " 4K ", want: 26.0 / 46.0, wantOK: true},
		{name: "4k video input", model: "doubao-seedance-2-0-260128", resolution: "4k", hasVideo: true, want: 16.0 / 46.0, wantOK: true},
		{name: "fast video input", model: "doubao-seedance-2-0-fast-260128", resolution: "720p", hasVideo: true, want: 22.0 / 37.0, wantOK: true},
		{name: "unsupported fast resolution uses base", model: "doubao-seedance-2-0-fast-260128", resolution: "1080p", want: 1, wantOK: true},
		{name: "unknown model", model: "unknown", resolution: "720p", want: 0, wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := GetVideoInputRatio(test.model, test.resolution, test.hasVideo)
			require.Equal(t, test.wantOK, ok)
			require.InDelta(t, test.want, got, 1e-12)
		})
	}
}

func TestEstimateBillingUsesResolutionAndVideoInput(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{
			"resolution": "1080p",
			"content": []interface{}{
				map[string]interface{}{"type": "video_url"},
			},
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(context, &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	})

	require.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-12)
}

func TestConvertToRequestPayloadPreservesPriorityZero(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "A cinematic city scene",
		Model:  "doubao-seedance-2-0-260128",
		Metadata: map[string]interface{}{
			"safety_identifier": "end-user-42",
			"priority":          0,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "end-user-42", payload.SafetyIdentifier)
	require.NotNil(t, payload.Priority)
	require.Equal(t, dto.IntValue(0), *payload.Priority)

	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	var body map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &body))
	priority, exists := body["priority"]
	require.True(t, exists)
	require.Equal(t, float64(0), priority)
}
