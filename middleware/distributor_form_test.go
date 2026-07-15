package middleware

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetModelFromMultipartRequestParsesStreamBoolean(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("stream", "true"))
	part, err := writer.CreateFormFile("image[]", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("png-data"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	defer common.CleanupBodyStorage(ctx)

	request, err := getModelFromRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", request.Model)
	require.NotNil(t, request.Stream)
	require.True(t, *request.Stream)
}

func TestGetModelFromURLEncodedRequestParsesStreamBoolean(t *testing.T) {
	gin.SetMode(gin.TestMode)
	form := url.Values{
		"model":  {"gpt-image-2"},
		"stream": {"false"},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	defer common.CleanupBodyStorage(ctx)

	request, err := getModelFromRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", request.Model)
	require.NotNil(t, request.Stream)
	require.False(t, *request.Stream)
}

func TestGetModelRequestDefaultsGPTImageEditToStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	defer common.CleanupBodyStorage(ctx)

	request, _, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", request.Model)
	require.NotNil(t, request.Stream)
	require.True(t, *request.Stream)
}
