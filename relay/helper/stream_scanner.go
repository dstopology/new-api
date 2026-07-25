package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 128MB default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
	DefaultImageStreamTimeout   = 15 * time.Minute
	// Bound a single blocked downstream write so cleanup can wait for every
	// stream goroutine without leaving stale writers behind.
	streamWriteTimeout = 30 * time.Second
)

type StreamScannerOptions struct {
	// Finalize runs after every scanner/handler goroutine has stopped and before
	// the final stream status is logged. Returning an error reclassifies a
	// transport-level normal end as an upstream truncation.
	Finalize func(status *relaycommon.StreamStatus) error
}

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func getStreamingTimeout(info *relaycommon.RelayInfo) time.Duration {
	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if streamingTimeout <= 0 {
		streamingTimeout = 30 * time.Second
	}
	if info != nil && isImageRelayMode(info.RelayMode) && streamingTimeout < DefaultImageStreamTimeout {
		streamingTimeout = DefaultImageStreamTimeout
	}
	return streamingTimeout
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func copyCodexSSEHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

// ExtendWriteDeadline pushes the connection write deadline forward before each
// stream write. Writers that do not support deadlines (for example httptest
// recorders) are intentionally ignored.
func ExtendWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult), options ...StreamScannerOptions) {
	if c == nil || c.Writer == nil || resp == nil || resp.Body == nil || info == nil || dataHandler == nil {
		return
	}

	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}

	ctx, cancel := context.WithCancel(context.Background())
	streamingTimeout := getStreamingTimeout(info)

	var (
		stopChan    = make(chan bool, 3)
		scanner     = NewStreamScanner(resp.Body)
		ticker      = time.NewTicker(streamingTimeout)
		pingTicker  *time.Ticker
		writeMutex  sync.Mutex
		wg          sync.WaitGroup
		cleanupOnce sync.Once
		stopOnce    sync.Once
	)

	stop := func() {
		stopOnce.Do(func() {
			close(stopChan)
		})
	}

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled, pingInterval := RelayPingConfig(info, generalSettings)
	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			stop()
			_ = resp.Body.Close()

			ticker.Stop()
			if pingTicker != nil {
				pingTicker.Stop()
			}

			wg.Wait()
		})
	}
	// Do not return the Gin context to its pool while a stream goroutine can
	// still read or write through it.
	defer cleanup()

	scanner.Split(bufio.ScanLines)
	copyCodexSSEHeaders(c, resp)
	SetEventStreamHeaders(c)

	ctx = context.WithValue(ctx, "stop_chan", stopChan)
	var requestDone <-chan struct{}
	if c.Request != nil {
		requestDone = c.Request.Context().Done()
	}

	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					stop()
				}
				logger.LogDebug(c, "ping goroutine exited")
				wg.Done()
			}()

			maxPingDuration := 30 * time.Minute
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					var err error
					func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						ExtendWriteDeadline(c)
						err = PingData(c)
					}()
					if err != nil {
						logger.LogError(c, "ping data error: "+err.Error())
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
						return
					}
					logger.LogDebug(c, "ping data sent")
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-requestDone:
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			stop()
			wg.Done()
		}()

		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			func() {
				writeMutex.Lock()
				defer writeMutex.Unlock()
				ExtendWriteDeadline(c)
				dataHandler(data, sr)
			}()
			if sr.IsStopped() {
				return
			}
		}
	})

	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			stop()
			logger.LogDebug(c, "scanner goroutine exited")
			wg.Done()
		}()

		for scanner.Scan() {
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = strings.TrimSpace(data[5:])
			if data == "" {
				continue
			}
			if strings.HasPrefix(data, "[DONE]") {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}

			info.SetFirstResponseTime()
			info.ReceivedResponseCount++
			select {
			case dataChan <- data:
			case <-ctx.Done():
				return
			case <-stopChan:
				return
			}
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
			logger.LogError(c, "scanner error: "+err.Error())
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	select {
	case <-ticker.C:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	case <-stopChan:
		// EndReason was set by the goroutine that stopped the stream.
	case <-requestDone:
		MarkStreamDownstreamGone(c)
		var requestErr error
		if c.Request != nil {
			requestErr = c.Request.Context().Err()
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, requestErr)
	}

	cleanup()
	if len(options) > 0 && options[0].Finalize != nil {
		if err := options[0].Finalize(info.StreamStatus); err != nil {
			endReason := info.StreamStatus.EndReason
			if info.StreamStatus.IsNormalEnd() {
				info.StreamStatus.ReplaceEndReason(endReason, relaycommon.StreamEndReasonUpstreamTruncated, err)
			} else {
				info.StreamStatus.RecordError(err.Error())
			}
		}
	}
	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
