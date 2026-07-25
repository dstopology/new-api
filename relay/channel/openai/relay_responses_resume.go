package openai

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const maxResponsesStreamResumeAttempts = 3

func (t *responsesStreamTracker) canResume(info *relaycommon.RelayInfo, baseRequest *http.Request) bool {
	return t != nil &&
		info != nil &&
		info.ResponsesUsageInfo != nil &&
		info.ResponsesUsageInfo.StreamResumeEnabled &&
		t.responseID != "" &&
		t.hasSequenceNumber &&
		baseRequest != nil &&
		baseRequest.URL != nil
}

func resumeResponsesStream(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	baseRequest *http.Request,
	tracker *responsesStreamTracker,
) (*http.Response, bool, error) {
	if !tracker.canResume(info, baseRequest) {
		return nil, false, fmt.Errorf("responses stream is not resumable")
	}
	if !waitBeforeResponsesResume(c) {
		return nil, false, fmt.Errorf("responses stream resume cancelled by downstream")
	}

	tracker.resumeAttempts++
	request, err := buildResponsesResumeRequest(c, baseRequest, tracker.responseID, tracker.lastSequenceNumber)
	if err != nil {
		tracker.resumeStatus = "request_error"
		return nil, false, err
	}
	response, err := channel.DoRequest(c, request, info)
	if err != nil {
		tracker.resumeStatus = "transport_error"
		return nil, true, err
	}
	if response.StatusCode != http.StatusOK {
		statusCode := response.StatusCode
		apiErr := service.RelayErrorHandler(c.Request.Context(), response, false)
		tracker.resumeStatus = fmt.Sprintf("http_%d", statusCode)
		retryable := statusCode == http.StatusRequestTimeout ||
			statusCode == http.StatusTooManyRequests ||
			statusCode >= http.StatusInternalServerError
		return nil, retryable, apiErr
	}
	tracker.resumeStatus = "connected"
	logger.LogInfo(c, fmt.Sprintf(
		"resumed responses stream response_id=%s starting_after=%d attempt=%d",
		tracker.responseID,
		tracker.lastSequenceNumber,
		tracker.resumeAttempts,
	))
	return response, false, nil
}

func buildResponsesResumeRequest(
	c *gin.Context,
	baseRequest *http.Request,
	responseID string,
	startingAfter int64,
) (*http.Request, error) {
	if baseRequest == nil || baseRequest.URL == nil {
		return nil, fmt.Errorf("responses resume base request is unavailable")
	}
	if responseID == "" {
		return nil, fmt.Errorf("responses resume response id is empty")
	}

	resumeURL := *baseRequest.URL
	resumeURL.Path = strings.TrimRight(resumeURL.Path, "/") + "/" + url.PathEscape(responseID)
	resumeURL.RawPath = ""
	query := resumeURL.Query()
	query.Set("stream", "true")
	query.Set("starting_after", strconv.FormatInt(startingAfter, 10))
	resumeURL.RawQuery = query.Encode()

	var requestContext = baseRequest.Context()
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, resumeURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build responses resume request failed: %w", err)
	}
	request.Body = http.NoBody
	request.Header = baseRequest.Header.Clone()
	request.Header.Del("Content-Length")
	request.Header.Del("Content-Type")
	request.Header.Set("Accept", "text/event-stream")
	return request, nil
}

func waitBeforeResponsesResume(c *gin.Context) bool {
	interval := time.Duration(common.RetryIntervalMilliseconds) * time.Millisecond
	if interval <= 0 {
		return true
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()

	var done <-chan struct{}
	if c != nil && c.Request != nil {
		done = c.Request.Context().Done()
	}
	select {
	case <-timer.C:
		return true
	case <-done:
		return false
	}
}
