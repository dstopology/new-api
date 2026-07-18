package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func RelayImage(c *gin.Context) {
	async, err := imageRequestWantsAsync(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": types.OpenAIError{
			Message: err.Error(),
			Type:    "invalid_request_error",
			Code:    "invalid_request",
		}})
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
	if strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return false, fmt.Errorf("parse multipart request: %w", err)
		}
		defer form.RemoveAll()
		values := form.Value["async"]
		if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			return false, nil
		}
		async, err := strconv.ParseBool(values[0])
		if err != nil {
			return false, errors.New("async must be true or false")
		}
		return async, nil
	}
	var request struct {
		Async *bool `json:"async"`
	}
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return false, err
	}
	return request.Async != nil && *request.Async, nil
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
