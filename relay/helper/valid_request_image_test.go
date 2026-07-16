package helper

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newImageEditMultipartContext(t *testing.T, fields map[string]string) *gin.Context {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return ctx
}

func TestImageEditMultipartPreservesExplicitScalarValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := newImageEditMultipartContext(t, map[string]string{
		"model":     "gpt-image-2",
		"prompt":    "edit",
		"n":         "0",
		"stream":    "false",
		"watermark": "false",
	})

	request, err := GetAndValidOpenAIImageRequest(ctx, relayconstant.RelayModeImagesEdits)

	require.NoError(t, err)
	require.NotNil(t, request.N)
	require.Zero(t, *request.N)
	require.NotNil(t, request.Stream)
	require.False(t, *request.Stream)
	require.NotNil(t, request.Watermark)
	require.False(t, *request.Watermark)
}

func TestImageEditMultipartDefaultsNOnlyWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := newImageEditMultipartContext(t, map[string]string{
		"model":  "gpt-image-2",
		"prompt": "edit",
	})

	request, err := GetAndValidOpenAIImageRequest(ctx, relayconstant.RelayModeImagesEdits)

	require.NoError(t, err)
	require.NotNil(t, request.N)
	require.Equal(t, uint(1), *request.N)
	require.Nil(t, request.Stream)
}

func TestImageGenerationPreservesStreamSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name       string
		body       string
		wantNil    bool
		wantStream bool
	}{
		{name: "absent remains non-stream", body: `{"model":"gpt-image-2","prompt":"draw"}`, wantNil: true},
		{name: "explicit false preserved", body: `{"model":"gpt-image-2","prompt":"draw","stream":false}`, wantStream: false},
		{name: "explicit true preserved", body: `{"model":"gpt-image-2","prompt":"draw","stream":true}`, wantStream: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, err := GetAndValidOpenAIImageRequest(ctx, relayconstant.RelayModeImagesGenerations)

			require.NoError(t, err)
			if test.wantNil {
				require.Nil(t, request.Stream)
				return
			}
			require.NotNil(t, request.Stream)
			require.Equal(t, test.wantStream, *request.Stream)
		})
	}
}
