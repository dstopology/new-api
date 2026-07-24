package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOpenAIRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"stream":false,
		"max_tokens":0,
		"max_completion_tokens":0,
		"top_p":0,
		"top_k":0,
		"n":0,
		"frequency_penalty":0,
		"presence_penalty":0,
		"seed":0,
		"logprobs":false,
		"top_logprobs":0,
		"dimensions":0,
		"return_images":false,
		"return_related_questions":false
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_completion_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_k").Exists())
	require.True(t, gjson.GetBytes(encoded, "n").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "seed").Exists())
	require.True(t, gjson.GetBytes(encoded, "logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "dimensions").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_images").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_related_questions").Exists())
}

func TestOpenAIResponsesRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"max_output_tokens":0,
		"max_tool_calls":0,
		"stream":false,
		"top_p":0,
		"service_tier":""
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "service_tier").Exists())
}

func TestOpenAIResponsesRequestPreservesOfficialCodexFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5",
		"input":[{
			"role":"user",
			"content":[{
				"type":"input_image",
				"image_url":"data:image/png;base64,AA=="
			}]
		}],
		"client_metadata":{"thread_id":"thread-123","turn_id":"turn-456"},
		"moderation":{"enabled":true},
		"prompt_cache_options":{"retention":"24h"},
		"reasoning":{
			"effort":"low",
			"mode":"minimal",
			"context":{"previous_turn":"turn-455"}
		}
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.Equal(t, "data:image/png;base64,AA==", gjson.GetBytes(encoded, "input.0.content.0.image_url").String())
	require.Equal(t, "thread-123", gjson.GetBytes(encoded, "client_metadata.thread_id").String())
	require.True(t, gjson.GetBytes(encoded, "moderation.enabled").Bool())
	require.Equal(t, "24h", gjson.GetBytes(encoded, "prompt_cache_options.retention").String())
	require.Equal(t, "minimal", gjson.GetBytes(encoded, "reasoning.mode").String())
	require.Equal(t, "turn-455", gjson.GetBytes(encoded, "reasoning.context.previous_turn").String())
}

func TestOpenAIResponsesCompactionRequestPreservesOfficialFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5",
		"parallel_tool_calls":false,
		"service_tier":"priority",
		"prompt_cache_key":"cache-123",
		"prompt_cache_options":{"retention":"24h"},
		"prompt_cache_retention":"in-memory",
		"reasoning":{"mode":"minimal"},
		"tools":[{"type":"function","name":"lookup"}],
		"text":{"format":{"type":"text"}}
	}`)

	var req OpenAIResponsesCompactionRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "parallel_tool_calls").Exists())
	require.False(t, gjson.GetBytes(encoded, "parallel_tool_calls").Bool())
	require.Equal(t, "priority", gjson.GetBytes(encoded, "service_tier").String())
	require.Equal(t, "cache-123", gjson.GetBytes(encoded, "prompt_cache_key").String())
	require.Equal(t, "24h", gjson.GetBytes(encoded, "prompt_cache_options.retention").String())
	require.Equal(t, "in-memory", gjson.GetBytes(encoded, "prompt_cache_retention").String())
	require.Equal(t, "minimal", gjson.GetBytes(encoded, "reasoning.mode").String())
	require.Equal(t, "lookup", gjson.GetBytes(encoded, "tools.0.name").String())
	require.Equal(t, "text", gjson.GetBytes(encoded, "text.format.type").String())
}
