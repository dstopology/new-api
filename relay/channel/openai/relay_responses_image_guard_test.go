package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerRejectsImageGenerationWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{DisableImageGeneration: true},
			UpstreamModelName: "gpt-5.5",
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_image_json",
			"model":"gpt-5.5",
			"output":[{"id":"ig_json_1","type":"image_generation_call","result":"final-image"}],
			"usage":{"input_tokens":7,"output_tokens":3}
		}`)),
	}

	usage, newAPIError := OaiResponsesHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, newAPIError)
	require.Equal(t, http.StatusForbidden, newAPIError.StatusCode)
	require.Equal(t, types.ErrorCodeImageGenerationDisabled, newAPIError.GetErrorCode())
	require.NotContains(t, recorder.Body.String(), "final-image")
}

func TestOaiResponsesStreamHandlerRejectsImageGenerationWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{DisableImageGeneration: true},
			UpstreamModelName: "gpt-5.5",
		},
	}
	resp := newChatCompletionSSE(http.StatusOK, `data: {"type":"response.output_item.done","item":{"id":"ig_stream_1","type":"image_generation_call","result":"final-image"}}

data: {"type":"response.completed","response":{"id":"resp_image_stream","model":"gpt-5.5","output":[{"id":"ig_stream_1","type":"image_generation_call","result":"final-image"}],"usage":{"input_tokens":11,"output_tokens":5}}}

data: [DONE]
`)

	usage, newAPIError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, newAPIError)
	require.Equal(t, http.StatusForbidden, newAPIError.StatusCode)
	require.Equal(t, types.ErrorCodeImageGenerationDisabled, newAPIError.GetErrorCode())
	require.NotContains(t, recorder.Body.String(), "final-image")
}

func TestOaiResponsesStreamHandlerRejectsImageGenerationToolWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{DisableImageGeneration: true},
			UpstreamModelName: "gpt-5.5",
		},
	}
	resp := newChatCompletionSSE(http.StatusOK, `data: {"type":"response.created","response":{"id":"resp_image_tool","model":"gpt-5.5","tools":[{"type":"image_generation","output_format":"png"}]}}

data: [DONE]
`)

	usage, newAPIError := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodeImageGenerationDisabled, newAPIError.GetErrorCode())
	require.NotContains(t, recorder.Body.String(), "image_generation")
}
