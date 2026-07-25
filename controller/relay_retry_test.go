package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func retryTestContext(ctx context.Context) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	return c
}

func TestWaitBeforeRetryDisabled(t *testing.T) {
	previous := common.RetryIntervalMilliseconds
	common.RetryIntervalMilliseconds = 0
	t.Cleanup(func() {
		common.RetryIntervalMilliseconds = previous
	})

	if !waitBeforeRetry(retryTestContext(context.Background())) {
		t.Fatal("zero retry interval should continue immediately")
	}
}

func TestWaitBeforeRetryUsesConfiguredInterval(t *testing.T) {
	previous := common.RetryIntervalMilliseconds
	common.RetryIntervalMilliseconds = 20
	t.Cleanup(func() {
		common.RetryIntervalMilliseconds = previous
	})

	startedAt := time.Now()
	if !waitBeforeRetry(retryTestContext(context.Background())) {
		t.Fatal("retry wait should continue after the interval")
	}
	if elapsed := time.Since(startedAt); elapsed < 15*time.Millisecond {
		t.Fatalf("retry wait returned too early: %s", elapsed)
	}
}

func TestWaitBeforeRetryStopsWhenRequestIsCanceled(t *testing.T) {
	previous := common.RetryIntervalMilliseconds
	common.RetryIntervalMilliseconds = 1000
	t.Cleanup(func() {
		common.RetryIntervalMilliseconds = previous
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startedAt := time.Now()
	if waitBeforeRetry(retryTestContext(ctx)) {
		t.Fatal("canceled request should stop retrying")
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled retry wait returned too slowly: %s", elapsed)
	}
}

func TestShouldRetryChannelErrorRequiresRemainingAttempt(t *testing.T) {
	c := retryTestContext(context.Background())
	channelErr := types.NewError(errors.New("channel unavailable"), types.ErrorCode("channel:test"))

	if shouldRetry(c, channelErr, 0) {
		t.Fatal("channel error should not retry after the final attempt")
	}
	if !shouldRetry(c, channelErr, 1) {
		t.Fatal("channel error should retry while an attempt remains")
	}
}

func TestShouldRetryStopsAfterResponsesOutputStarted(t *testing.T) {
	c := retryTestContext(context.Background())
	helper.MarkResponsesStreamStarted(c)
	channelErr := types.NewError(errors.New("stream interrupted"), types.ErrorCodeUpstreamStreamInterrupted)

	if shouldRetry(c, channelErr, 3) {
		t.Fatal("responses stream must not restart after meaningful output was sent")
	}
}

func TestResponsesStreamFailureCursor(t *testing.T) {
	status := relaycommon.NewStreamStatus()
	status.SetDetail("response_id", "resp_test")
	status.SetDetail("last_sequence_number", int64(41))
	info := &relaycommon.RelayInfo{StreamStatus: status}

	responseID, sequenceNumber := responsesStreamFailureCursor(info)

	if responseID != "resp_test" {
		t.Fatalf("unexpected response id: %s", responseID)
	}
	if sequenceNumber != 42 {
		t.Fatalf("unexpected sequence number: %d", sequenceNumber)
	}
}
