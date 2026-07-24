package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
