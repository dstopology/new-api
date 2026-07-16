package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	DefaultImageKeepAliveInterval     = 25 * time.Second
	DefaultNonStreamKeepAliveInterval = 60 * time.Second

	nonStreamKeepAliveContextKey = "http_non_stream_keepalive"
)

type nonStreamKeepAlive struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func isImageRelayMode(mode int) bool {
	return mode == relayconstant.RelayModeImagesGenerations || mode == relayconstant.RelayModeImagesEdits
}

func RelayPingConfig(info *relaycommon.RelayInfo, generalSettings *operation_setting.GeneralSetting) (bool, time.Duration) {
	if info == nil || info.DisablePing {
		return false, DefaultPingInterval
	}

	imageMode := isImageRelayMode(info.RelayMode)
	// Claude CLI (/v1/messages) streams, e.g. context compaction, can stall well past
	// Cloudflare's ~120s origin timeout before the first byte and trip a 524. Keep that
	// format alive like image mode — independent of the global ping switch — so other
	// channels stay completely unaffected unless the operator opts in globally.
	claudeMode := info.RelayFormat == types.RelayFormatClaude
	pingEnabled := imageMode || claudeMode
	if generalSettings != nil && generalSettings.PingIntervalEnabled {
		pingEnabled = true
	}
	if !pingEnabled {
		return false, DefaultPingInterval
	}

	interval := DefaultPingInterval
	if generalSettings != nil && generalSettings.PingIntervalSeconds > 0 {
		interval = time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	}
	if imageMode || claudeMode {
		if generalSettings == nil || !generalSettings.PingIntervalEnabled || interval > time.Minute {
			interval = DefaultImageKeepAliveInterval
		}
	}
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	return true, interval
}

func StartNonStreamKeepAlive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || c.Request == nil || interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	keepAlive := &nonStreamKeepAlive{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	c.Set(nonStreamKeepAliveContextKey, keepAlive)

	go func() {
		defer close(keepAlive.done)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		headersSent := false

		for {
			select {
			case <-ticker.C:
				var err error
				if !headersSent {
					err = WriteNonStreamHeaders(c)
					headersSent = err == nil
				} else {
					err = WriteJSONWhitespace(c)
				}
				if err != nil {
					logger.LogDebug(c, "non-stream keepalive stopped: %s", err.Error())
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() {
		StopNonStreamKeepAlive(c)
	}
}

func StopNonStreamKeepAlive(c *gin.Context) {
	if c == nil {
		return
	}
	value, ok := c.Get(nonStreamKeepAliveContextKey)
	if !ok || value == nil {
		return
	}
	keepAlive, ok := value.(*nonStreamKeepAlive)
	if !ok || keepAlive == nil {
		return
	}
	keepAlive.once.Do(func() {
		keepAlive.cancel()
		select {
		case <-keepAlive.done:
		case <-time.After(5 * time.Second):
			logger.LogDebug(c, "timeout waiting for non-stream keepalive to stop")
		}
		c.Set(nonStreamKeepAliveContextKey, nil)
	})
}

func validateNonStreamKeepAliveContext(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	return nil
}

func WriteNonStreamHeaders(c *gin.Context) error {
	if err := validateNonStreamKeepAliveContext(c); err != nil {
		return err
	}
	if c.Writer.Written() {
		return errors.New("response already written")
	}

	header := c.Writer.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteHeaderNow()
	return FlushWriter(c)
}

func WriteJSONWhitespace(c *gin.Context) error {
	if err := validateNonStreamKeepAliveContext(c); err != nil {
		return err
	}
	if !c.Writer.Written() {
		return errors.New("response headers not written")
	}

	if _, err := c.Writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write JSON whitespace keepalive: %w", err)
	}
	return FlushWriter(c)
}
