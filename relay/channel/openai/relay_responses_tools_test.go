package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordResponsesBuiltInToolCallSupportsStableWebSearch(t *testing.T) {
	info := &relaycommon.RelayInfo{ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearch: &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolWebSearch},
		},
	}}

	recordResponsesBuiltInToolCall(info, dto.BuildInCallWebSearchCall)

	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
}

func TestRecordResponsesBuiltInToolCallFallsBackToPreview(t *testing.T) {
	info := &relaycommon.RelayInfo{ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolWebSearchPreview},
		},
	}}

	recordResponsesBuiltInToolCall(info, dto.BuildInCallWebSearchCall)

	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOaiResponsesHandlerCountsActualCallsInsteadOfDeclaredTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearch:  &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolWebSearch},
			dto.BuildInToolFileSearch: &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolFileSearch},
		},
	}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"tools":[{"type":"web_search"},{"type":"file_search"}],
			"output":[{"type":"web_search_call"}],
			"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}
		}`)),
	}

	usage, apiErr := OaiResponsesHandler(ctx, info, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.TotalTokens)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	require.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}
