package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNonStreamKeepAlivePreservesJSONResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	writeResult := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		if err := WriteNonStreamHeaders(c); err != nil {
			writeResult <- err
			return
		}
		if err := WriteJSONWhitespace(c); err != nil {
			writeResult <- err
			return
		}
		writeResult <- nil
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}))
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.NoError(t, <-writeResult)

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, response.Header.Get("Content-Type"), "application/json")
	require.True(t, strings.HasPrefix(string(body), "\n"))
	require.NotContains(t, string(body), ": PING")
	require.NotContains(t, string(body), "data:")
	require.JSONEq(t, `{"ok":true}`, string(body))
}
