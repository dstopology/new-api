package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"sync"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayPingConfigUsesImageStreamInterval(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}
	enabled, interval := RelayPingConfig(info, &operation_setting.GeneralSetting{})

	require.True(t, enabled)
	require.Equal(t, DefaultImageKeepAliveInterval, interval)
}

func TestRelayPingConfigRespectsDisablePing(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		DisablePing: true,
	}
	enabled, interval := RelayPingConfig(info, &operation_setting.GeneralSetting{
		PingIntervalEnabled: true,
		PingIntervalSeconds: 1,
	})

	require.False(t, enabled)
	require.Equal(t, DefaultPingInterval, interval)
	require.NotEqual(t, time.Second, interval)
}

func TestWriteProcessingDoesNotCommitFinalResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	processingResult := make(chan error, 1)
	releaseFinal := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFinal) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		processingResult <- WriteProcessing(c)
		<-releaseFinal
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	}))
	t.Cleanup(func() {
		release()
		server.Close()
	})

	got1xx := make(chan int, 1)
	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, _ textproto.MIMEHeader) error {
			got1xx <- code
			return nil
		},
	}
	request, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(context.Background(), trace),
		http.MethodGet,
		server.URL,
		nil,
	)
	require.NoError(t, err)

	type responseResult struct {
		response *http.Response
		err      error
	}
	responseChan := make(chan responseResult, 1)
	go func() {
		response, requestErr := server.Client().Do(request)
		responseChan <- responseResult{response: response, err: requestErr}
	}()

	select {
	case processingErr := <-processingResult:
		require.NoError(t, processingErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out writing HTTP 102")
	}
	select {
	case code := <-got1xx:
		require.Equal(t, http.StatusProcessing, code)
	case <-time.After(2 * time.Second):
		t.Fatal("client did not receive HTTP 102")
	}
	select {
	case result := <-responseChan:
		if result.response != nil {
			_ = result.response.Body.Close()
		}
		t.Fatal("HTTP 102 committed an early final response")
	case <-time.After(200 * time.Millisecond):
	}

	release()
	var result responseResult
	select {
	case result = <-responseChan:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for final response")
	}
	require.NoError(t, result.err)
	require.NotNil(t, result.response)
	defer result.response.Body.Close()

	body, err := io.ReadAll(result.response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.response.StatusCode)
	require.Contains(t, result.response.Header.Get("Content-Type"), "application/json")
	require.JSONEq(t, `{"ok":true}`, string(body))
}
