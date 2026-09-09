package openai

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestRealtimeClosesUpstreamBeforeReturning(t *testing.T) {
	pair := func() (*websocket.Conn, *websocket.Conn) {
		accepted := make(chan *websocket.Conn, 1)
		upgrader := websocket.Upgrader{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err == nil {
				accepted <- conn
			}
		}))
		t.Cleanup(server.Close)
		client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		peer := <-accepted
		t.Cleanup(func() { client.Close(); peer.Close() })
		return client, peer
	}
	user, proxyClient := pair()
	proxyTarget, upstream := pair()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{ClientWs: proxyClient, TargetWs: proxyTarget}
	done := make(chan struct{})
	go func() { OpenaiRealtimeHandler(c, info); close(done) }()
	require.NoError(t, user.Close())
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reader shutdown did not finish")
	}
	require.NoError(t, upstream.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := upstream.ReadMessage()
	require.Error(t, err)
	if timeout, ok := err.(net.Error); ok {
		require.False(t, timeout.Timeout(), "handler returned with its upstream reader still alive")
	}
}
