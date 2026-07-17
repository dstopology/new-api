package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestImageRequestStreamPreservesExplicitValues(t *testing.T) {
	var streamTrue ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw","stream":true}`), &streamTrue))
	require.NotNil(t, streamTrue.Stream)
	require.True(t, *streamTrue.Stream)
	require.True(t, streamTrue.IsStream(nil))

	body, err := common.Marshal(streamTrue)
	require.NoError(t, err)
	require.Contains(t, string(body), `"stream":true`)

	var streamFalse ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw","stream":false}`), &streamFalse))
	require.NotNil(t, streamFalse.Stream)
	require.False(t, *streamFalse.Stream)
	require.False(t, streamFalse.IsStream(nil))

	body, err = common.Marshal(streamFalse)
	require.NoError(t, err)
	require.Contains(t, string(body), `"stream":false`)

	var streamAbsent ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw"}`), &streamAbsent))
	require.Nil(t, streamAbsent.Stream)
	require.False(t, streamAbsent.IsStream(nil))
}

func TestImageRequestAsyncPreservesExplicitValues(t *testing.T) {
	var asyncTrue ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw","async":true}`), &asyncTrue))
	require.NotNil(t, asyncTrue.Async)
	require.True(t, *asyncTrue.Async)
	body, err := common.Marshal(asyncTrue)
	require.NoError(t, err)
	require.Contains(t, string(body), `"async":true`)

	var asyncFalse ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw","async":false}`), &asyncFalse))
	require.NotNil(t, asyncFalse.Async)
	require.False(t, *asyncFalse.Async)
	body, err = common.Marshal(asyncFalse)
	require.NoError(t, err)
	require.Contains(t, string(body), `"async":false`)

	var asyncAbsent ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"draw"}`), &asyncAbsent))
	require.Nil(t, asyncAbsent.Async)
}
