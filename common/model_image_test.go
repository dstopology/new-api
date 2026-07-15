package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsImageGenerationModelSupportsGPTImage2(t *testing.T) {
	require.True(t, IsImageGenerationModel("gpt-image-2"))
	require.True(t, IsGPTImageModel("GPT-IMAGE-2"))
	require.False(t, IsGPTImageModel("dall-e-3"))
}
