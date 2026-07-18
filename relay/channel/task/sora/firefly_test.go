package sora

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateFireflyVideoRequest(t *testing.T) {
	falseValue := false
	trueValue := true
	zeroDuration := 0
	fourSeconds := "4"
	threeImages := []string{
		"https://assets.example/one.png",
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2nQAAAABJRU5ErkJggg==",
		"https://assets.example/three.webp",
	}

	tests := []struct {
		name        string
		model       string
		request     relaycommon.TaskSubmitReq
		fields      fireflyVideoFields
		jsonRequest bool
		wantError   string
	}{
		{
			name:  "sora accepts one frame and negative prompt",
			model: "sora-2",
			request: relaycommon.TaskSubmitReq{
				Prompt:   "rotate the triangle",
				Duration: 4,
				Images:   threeImages[:1],
			},
			fields: fireflyVideoFields{
				AspectRatio:    "16:9",
				GenerateAudio:  &trueValue,
				NegativePrompt: "watermark",
				ReferenceMode:  "frame",
			},
			jsonRequest: true,
		},
		{
			name:  "sora rejects a second frame",
			model: "sora-2-pro",
			request: relaycommon.TaskSubmitReq{
				Prompt: "rotate the triangle",
				Images: threeImages[:2],
			},
			jsonRequest: true,
			wantError:   "at most 1 reference image",
		},
		{
			name:  "sora rejects audio disable that upstream ignores",
			model: "sora-2",
			request: relaycommon.TaskSubmitReq{
				Prompt: "rotate the triangle",
			},
			fields:      fireflyVideoFields{GenerateAudio: &falseValue},
			jsonRequest: true,
			wantError:   "cannot disable generated audio",
		},
		{
			name:  "veo frame accepts two ordered frames",
			model: "veo-3-1-fast",
			request: relaycommon.TaskSubmitReq{
				Prompt:   "transition between frames",
				Duration: 6,
				Images:   threeImages[:2],
			},
			fields: fireflyVideoFields{
				GenerateAudio: &falseValue,
				ReferenceMode: "frame",
				Resolution:    "720p",
			},
			jsonRequest: true,
		},
		{
			name:  "explicit zero duration is preserved and rejected",
			model: "veo-3-1-fast",
			request: relaycommon.TaskSubmitReq{
				Prompt: "transition between frames",
			},
			fields:      fireflyVideoFields{Duration: &zeroDuration},
			jsonRequest: true,
			wantError:   "only supports duration values",
		},
		{
			name:  "duration and seconds cannot be combined",
			model: "veo-3-1-fast",
			request: relaycommon.TaskSubmitReq{
				Prompt: "transition between frames",
			},
			fields: fireflyVideoFields{
				Duration: &zeroDuration,
				Seconds:  &fourSeconds,
			},
			jsonRequest: true,
			wantError:   "must not be provided together",
		},
		{
			name:  "veo rejects negative prompt",
			model: "veo-3-1",
			request: relaycommon.TaskSubmitReq{
				Prompt: "transition between frames",
			},
			fields:      fireflyVideoFields{NegativePrompt: "watermark"},
			jsonRequest: true,
			wantError:   "does not support negative_prompt",
		},
		{
			name:  "veo ref requires image mode",
			model: "veo-3-1-ref",
			request: relaycommon.TaskSubmitReq{
				Prompt:   "combine the references",
				Duration: 8,
				Images:   threeImages,
			},
			fields:      fireflyVideoFields{ReferenceMode: "frame"},
			jsonRequest: true,
			wantError:   `requires reference_mode "image"`,
		},
		{
			name:  "veo ref rejects advertised but broken four seconds",
			model: "veo-3-1-ref",
			request: relaycommon.TaskSubmitReq{
				Prompt:   "combine the references",
				Duration: 4,
				Images:   threeImages,
			},
			jsonRequest: true,
			wantError:   "only supports duration values [8]",
		},
		{
			name:  "veo ref accepts three material references",
			model: "veo-3-1-ref",
			request: relaycommon.TaskSubmitReq{
				Prompt:   "combine the references",
				Duration: 8,
				Images:   threeImages,
			},
			fields: fireflyVideoFields{
				ReferenceMode: "image",
				Resolution:    "1080p",
			},
			jsonRequest: true,
		},
		{
			name:  "json input_reference is rejected",
			model: "sora-2",
			request: relaycommon.TaskSubmitReq{
				Prompt:         "rotate the triangle",
				InputReference: "https://assets.example/one.png",
			},
			jsonRequest: true,
			wantError:   "images array",
		},
		{
			name:  "invalid image source is rejected",
			model: "veo-3-1-fast",
			request: relaycommon.TaskSubmitReq{
				Prompt: "transition between frames",
				Images: []string{"file:///private/reference.png"},
			},
			jsonRequest: true,
			wantError:   "HTTP(S) URL or image data URI",
		},
		{
			name:  "invalid data URI image bytes are rejected",
			model: "veo-3-1-fast",
			request: relaycommon.TaskSubmitReq{
				Prompt: "transition between frames",
				Images: []string{"data:image/png;base64,aW1hZ2U="},
			},
			jsonRequest: true,
			wantError:   "invalid image data",
		},
		{
			name:  "unrelated model is unchanged",
			model: "another-video-model",
			request: relaycommon.TaskSubmitReq{
				Prompt: "video",
				Images: []string{"file:///accepted-by-provider"},
			},
			jsonRequest: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFireflyVideoRequest(test.model, test.request, test.fields, test.jsonRequest)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestBuildRequestBodyNormalizesFireflyReferenceModeAndPreservesFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"public-veo",
		"prompt":"transition",
		"images":["https://assets.example/first.png","https://assets.example/last.png"],
		"reference_mode":"image",
		"generate_audio":false
	}`))
	context.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(context) })

	body, err := (&TaskAdaptor{}).BuildRequestBody(context, &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "veo-3-1-fast"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	require.NoError(t, err)
	payload, err := io.ReadAll(body)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, common.Unmarshal(payload, &decoded))
	require.Equal(t, "veo-3-1-fast", decoded["model"])
	require.Equal(t, "frame", decoded["reference_mode"])
	require.Equal(t, false, decoded["generate_audio"])
	require.Len(t, decoded["images"], 2)
}

func TestNormalizeFireflyVideoBodyUsesMaterialMode(t *testing.T) {
	body := map[string]interface{}{
		"images": []interface{}{"https://assets.example/material.png"},
	}
	normalizeFireflyVideoBody(body, "veo-3-1-ref")
	require.Equal(t, "image", body["reference_mode"])
}
