package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func RelayImage(c *gin.Context) {
	async, stream, err := imageRequestDelivery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": types.OpenAIError{
			Message: err.Error(),
			Type:    "invalid_request_error",
			Code:    "invalid_request",
		}})
		return
	}
	if shouldBridgeAsyncImageStream(c, async, stream) {
		if !constant.UpdateTask {
			asyncImageError(c, http.StatusServiceUnavailable, "async_task_disabled", "async task polling is disabled")
			return
		}
		if err := prepareAsyncImageIdempotency(c); err != nil {
			asyncImageError(c, http.StatusBadRequest, "invalid_idempotency_key", err.Error())
			return
		}
		c.Set("platform", string(constant.TaskPlatformAsyncImage))
		common.SetContextKey(c, constant.ContextKeyAsyncImageStreamBridge, true)
		relayAsyncImageStream(c)
		return
	}
	if !async {
		Relay(c, types.RelayFormatOpenAIImage)
		return
	}
	if err := prepareAsyncImageIdempotency(c); err != nil {
		asyncImageError(c, http.StatusBadRequest, "invalid_idempotency_key", err.Error())
		return
	}
	if replayAsyncImageIdempotentTask(c) {
		return
	}
	if !constant.UpdateTask {
		asyncImageError(c, http.StatusServiceUnavailable, "async_task_disabled", "async task polling is disabled")
		return
	}
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	RelayTask(c)
}

func imageRequestDelivery(c *gin.Context) (async bool, stream bool, err error) {
	if strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
		form, parseErr := common.ParseMultipartFormReusable(c)
		if parseErr != nil {
			return false, false, fmt.Errorf("parse multipart request: %w", parseErr)
		}
		defer form.RemoveAll()
		async, err = optionalFormBool(form.Value, "async")
		if err != nil {
			return false, false, err
		}
		stream, err = optionalFormBool(form.Value, "stream")
		return async, stream, err
	}
	var request struct {
		Async  *bool `json:"async"`
		Stream *bool `json:"stream"`
	}
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return false, false, err
	}
	return request.Async != nil && *request.Async, request.Stream != nil && *request.Stream, nil
}

func optionalFormBool(values map[string][]string, field string) (bool, error) {
	rawValues := values[field]
	if len(rawValues) == 0 || strings.TrimSpace(rawValues[0]) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(rawValues[0])
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", field)
	}
	return value, nil
}

func shouldBridgeAsyncImageStream(c *gin.Context, async bool, stream bool) bool {
	if !stream {
		return false
	}
	if async {
		return true
	}
	if common.GetContextKeyInt(c, constant.ContextKeyChannelType) != constant.ChannelTypeOpenAI {
		return false
	}
	baseURL := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	if baseURL == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host != "" && !strings.EqualFold(host, "api.openai.com")
}

func relayAsyncImageStream(c *gin.Context) {
	result, relayInfo, taskErr := executeRelayTask(c)
	if taskErr != nil {
		if shouldFallbackAsyncImageStream(taskErr) {
			if relayInfo != nil && relayInfo.PublicTaskID != "" {
				if err := model.DeleteRejectedAsyncImageTask(relayInfo.UserId, relayInfo.PublicTaskID); err != nil {
					logger.LogError(c, fmt.Sprintf("delete rejected async image stream task %s: %v", relayInfo.PublicTaskID, err))
					writeAsyncImageStreamError(c, "stream_fallback_failed", "failed to release rejected async image task")
					return
				}
			}
			c.Writer.Header().Del("X-New-API-Task-ID")
			c.Set("platform", "")
			common.SetContextKey(c, constant.ContextKeyAsyncImageStreamBridge, false)
			Relay(c, types.RelayFormatOpenAIImage)
			return
		}
		writeAsyncImageStreamError(c, taskErr.Code, taskErr.Message)
		return
	}
	if result == nil || result.Task == nil {
		writeAsyncImageStreamError(c, "empty_task_result", "async image task was not persisted")
		return
	}

	taskID := result.Task.TaskID
	c.Header("X-New-API-Task-ID", taskID)
	relayhelper.SetEventStreamHeaders(c)
	if err := relayhelper.PingData(c); err != nil {
		return
	}

	task, err := waitForAsyncImageStreamTask(
		c,
		result.Task.UserId,
		taskID,
		time.Second,
		relayhelper.DefaultImageKeepAliveInterval,
		asyncImageStreamWaitTimeout(),
		model.GetByTaskId,
	)
	if err != nil {
		if c.Request.Context().Err() == nil {
			writeAsyncImageStreamError(c, "stream_wait_failed", err.Error())
		}
		return
	}
	response, err := service.BuildAsyncImageTaskResponse(task)
	if err != nil {
		writeAsyncImageStreamError(c, "task_response_failed", "failed to build image result")
		return
	}
	if response.Status == "failed" {
		code := "generation_failed"
		message := "image generation failed"
		if response.Error != nil {
			if response.Error.Code != "" {
				code = response.Error.Code
			}
			if response.Error.Message != "" {
				message = response.Error.Message
			}
		}
		writeAsyncImageStreamError(c, code, message)
		return
	}
	if response.OutputExpired || len(response.Data) == 0 {
		writeAsyncImageStreamError(c, "output_expired", "temporary image has expired")
		return
	}
	writeAsyncImageStreamResult(c, dto.ImageResponse{
		Created: response.CreatedAt,
		Data:    response.Data,
	})
}

type asyncImageTaskLoader func(userID int, taskID string) (*model.Task, bool, error)

func waitForAsyncImageStreamTask(
	c *gin.Context,
	userID int,
	taskID string,
	pollInterval time.Duration,
	pingInterval time.Duration,
	waitTimeout time.Duration,
	load asyncImageTaskLoader,
) (*model.Task, error) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if pingInterval <= 0 {
		pingInterval = relayhelper.DefaultImageKeepAliveInterval
	}
	if waitTimeout <= 0 {
		waitTimeout = 15 * time.Minute
	}
	pollTicker := time.NewTicker(pollInterval)
	pingTicker := time.NewTicker(pingInterval)
	timeout := time.NewTimer(waitTimeout)
	defer pollTicker.Stop()
	defer pingTicker.Stop()
	defer timeout.Stop()

	for {
		task, exists, err := load(userID, taskID)
		if err != nil {
			return nil, fmt.Errorf("query async image task: %w", err)
		}
		if !exists || task == nil {
			return nil, errors.New("async image task not found")
		}
		if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
			return task, nil
		}

		select {
		case <-c.Request.Context().Done():
			return nil, c.Request.Context().Err()
		case <-pollTicker.C:
		case <-pingTicker.C:
			if err := relayhelper.PingData(c); err != nil {
				return nil, fmt.Errorf("send stream ping: %w", err)
			}
		case <-timeout.C:
			return nil, fmt.Errorf("async image stream wait exceeded %s", waitTimeout)
		}
	}
}

func asyncImageStreamWaitTimeout() time.Duration {
	defaultMinutes := common.GetEnvOrDefault("ASYNC_IMAGE_TASK_TIMEOUT_MINUTES", 15)
	minutes := common.GetEnvOrDefault("ASYNC_IMAGE_STREAM_WAIT_TIMEOUT_MINUTES", defaultMinutes)
	if minutes <= 0 {
		minutes = 15
	}
	return time.Duration(minutes) * time.Minute
}

func shouldFallbackAsyncImageStream(taskErr *dto.TaskError) bool {
	if taskErr == nil || taskErr.LocalError {
		return false
	}
	switch taskErr.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func writeAsyncImageStreamResult(c *gin.Context, response dto.ImageResponse) {
	body, err := common.Marshal(response)
	if err != nil {
		writeAsyncImageStreamError(c, "marshal_response_failed", "failed to encode image result")
		return
	}
	relayhelper.SetEventStreamHeaders(c)
	if err := relayhelper.StringData(c, string(body)); err == nil {
		relayhelper.Done(c)
	}
}

func writeAsyncImageStreamError(c *gin.Context, code string, message string) {
	if code == "" {
		code = "stream_error"
	}
	if message == "" {
		message = "image stream failed"
	}
	body, err := common.Marshal(gin.H{"error": gin.H{
		"code":    code,
		"message": message,
		"type":    "new_api_error",
	}})
	if err != nil {
		return
	}
	relayhelper.SetEventStreamHeaders(c)
	if err := relayhelper.StringData(c, string(body)); err == nil {
		relayhelper.Done(c)
	}
}

func prepareAsyncImageIdempotency(c *gin.Context) error {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		return nil
	}
	if len(key) > 128 || strings.IndexFunc(key, func(r rune) bool {
		return r <= ' ' || r > '~'
	}) >= 0 {
		return errors.New("Idempotency-Key must be at most 128 visible ASCII characters")
	}
	requestHash, err := asyncImageRequestHash(c)
	if err != nil {
		return fmt.Errorf("hash async image request: %w", err)
	}
	common.SetContextKey(c, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(c, constant.ContextKeyAsyncImageRequestHash, requestHash)
	return nil
}

func asyncImageRequestHash(c *gin.Context) (string, error) {
	payload := map[string]any{
		"method": c.Request.Method,
		"path":   c.Request.URL.Path,
	}
	if strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return "", err
		}
		defer form.RemoveAll()
		files := make(map[string][]map[string]any, len(form.File))
		for field, headers := range form.File {
			for _, header := range headers {
				file, err := header.Open()
				if err != nil {
					return "", err
				}
				hasher := sha256.New()
				_, copyErr := io.Copy(hasher, file)
				closeErr := file.Close()
				if copyErr != nil {
					return "", copyErr
				}
				if closeErr != nil {
					return "", closeErr
				}
				files[field] = append(files[field], map[string]any{
					"sha256": fmt.Sprintf("%x", hasher.Sum(nil)),
					"size":   header.Size,
				})
			}
		}
		payload["values"] = form.Value
		payload["files"] = files
	} else {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return "", err
		}
		body, err := storage.Bytes()
		if err != nil {
			return "", err
		}
		var decoded any
		if err := common.Unmarshal(body, &decoded); err != nil {
			return "", err
		}
		payload["body"] = decoded
	}
	canonical, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", sum), nil
}

func replayAsyncImageIdempotentTask(c *gin.Context) bool {
	key := common.GetContextKeyString(c, constant.ContextKeyAsyncImageIdempotencyKey)
	if key == "" {
		return false
	}
	task, exists, err := model.GetAsyncImageTaskByIdempotency(c.GetInt("id"), key)
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "idempotency_query_failed", "failed to query idempotent task")
		return true
	}
	if !exists || task == nil {
		return false
	}
	requestHash := common.GetContextKeyString(c, constant.ContextKeyAsyncImageRequestHash)
	if task.RequestHash == "" || task.RequestHash != requestHash {
		asyncImageError(c, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with a different request")
		return true
	}
	response, err := service.BuildAsyncImageTaskResponse(task)
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "idempotency_replay_failed", "failed to replay idempotent task")
		return true
	}
	body, err := common.Marshal(response)
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "idempotency_replay_failed", "failed to replay idempotent task")
		return true
	}
	c.Header("Idempotent-Replayed", "true")
	c.Data(http.StatusOK, "application/json", body)
	return true
}

func imageRequestWantsAsync(c *gin.Context) (bool, error) {
	async, _, err := imageRequestDelivery(c)
	return async, err
}

func RelayImageTaskFetch(c *gin.Context) {
	task, ok := getOwnedAsyncImageTask(c)
	if !ok {
		return
	}
	response, err := service.BuildAsyncImageTaskResponse(task)
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "task_query_failed", "failed to query task")
		return
	}
	c.JSON(http.StatusOK, response)
}

func RelayImageContent(c *gin.Context) {
	if _, ok := getOwnedAsyncImageTask(c); !ok {
		return
	}
	media, file, err := service.OpenTemporaryMedia(c.GetInt("id"), c.Param("task_id"), c.Param("media_id"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTemporaryMediaExpired):
			asyncImageError(c, http.StatusGone, "output_expired", "temporary image has expired")
		case errors.Is(err, service.ErrTemporaryMediaNotFound):
			asyncImageError(c, http.StatusNotFound, "output_not_found", "temporary image not found")
		default:
			asyncImageError(c, http.StatusInternalServerError, "output_read_failed", "failed to read temporary image")
		}
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "output_read_failed", "failed to read temporary image")
		return
	}
	c.Header("Content-Type", media.ContentType)
	c.Header("Content-Disposition", "inline")
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, media.MediaID, stat.ModTime(), file)
}

func getOwnedAsyncImageTask(c *gin.Context) (*model.Task, bool) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		asyncImageError(c, http.StatusBadRequest, "invalid_request", "task_id is required")
		return nil, false
	}
	task, exists, err := model.GetByTaskId(c.GetInt("id"), taskID)
	if err != nil {
		asyncImageError(c, http.StatusInternalServerError, "task_query_failed", "failed to query task")
		return nil, false
	}
	if !exists || task == nil || task.Platform != constant.TaskPlatformAsyncImage || !asyncImageActionMatchesPath(task, c.Request.URL.Path) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "task_not_exist", "message": "task_not_exist", "data": nil})
		return nil, false
	}
	return task, true
}

func asyncImageActionMatchesPath(task *model.Task, path string) bool {
	if strings.Contains(path, "/images/edits/") {
		return task.Action == constant.TaskActionImageEdits
	}
	return task.Action == constant.TaskActionImageGenerations
}

func asyncImageError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "type": "new_api_error"}})
}
