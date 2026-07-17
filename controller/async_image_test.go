package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageRequestWantsAsyncJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "true", body: `{"model":"test","async":true}`, want: true},
		{name: "false", body: `{"model":"test","async":false}`, want: false},
		{name: "absent", body: `{"model":"test"}`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			async, err := imageRequestWantsAsync(c)
			require.NoError(t, err)
			require.Equal(t, test.want, async)
		})
	}
}

func TestImageRequestWantsAsyncMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "test"))
	require.NoError(t, writer.WriteField("async", "true"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	async, err := imageRequestWantsAsync(c)
	require.NoError(t, err)
	require.True(t, async)
}

func TestAsyncImageTaskSubmissionIsNotRetried(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{StatusCode: http.StatusTooManyRequests}, 2))
}
