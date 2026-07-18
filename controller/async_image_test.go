package controller

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageRequestWantsAsyncJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "true", body: `{"model":"test","async":true}`, want: true},
		{name: "false", body: `{"model":"test","async":false}`, want: false},
		{name: "absent", body: `{"model":"test"}`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			async, err := imageRequestWantsAsync(c)
			require.NoError(t, err)
			require.Equal(t, test.want, async)
		})
	}
}

func TestImageRequestWantsAsyncMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "test"))
	require.NoError(t, writer.WriteField("async", "true"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	async, err := imageRequestWantsAsync(c)
	require.NoError(t, err)
	require.True(t, async)
}

func TestImageRequestDeliveryPreservesStreamFlag(t *testing.T) {
	c := asyncImageHashJSONContext(t, `{"model":"test","async":false,"stream":true}`)
	async, stream, err := imageRequestDelivery(c)
	require.NoError(t, err)
	require.False(t, async)
	require.True(t, stream)
}

func TestShouldBridgeAsyncImageStream(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		baseURL     string
		async       bool
		stream      bool
		want        bool
	}{
		{name: "custom OpenAI upstream", channelType: constant.ChannelTypeOpenAI, baseURL: "https://upstream.example.com", stream: true, want: true},
		{name: "official OpenAI", channelType: constant.ChannelTypeOpenAI, baseURL: "https://api.openai.com", stream: true, want: false},
		{name: "non OpenAI channel", channelType: constant.ChannelTypeGemini, baseURL: "https://example.com", stream: true, want: false},
		{name: "explicit async", channelType: constant.ChannelTypeGemini, baseURL: "https://example.com", async: true, stream: true, want: true},
		{name: "non stream", channelType: constant.ChannelTypeOpenAI, baseURL: "https://upstream.example.com", async: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(c, constant.ContextKeyChannelType, test.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, test.baseURL)
			require.Equal(t, test.want, shouldBridgeAsyncImageStream(c, test.async, test.stream))
		})
	}
}

func TestWaitForAsyncImageStreamTaskPingsUntilCompletion(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	calls := 0
	load := func(userID int, taskID string) (*model.Task, bool, error) {
		require.Equal(t, 7, userID)
		require.Equal(t, "task_stream", taskID)
		calls++
		status := model.TaskStatus(model.TaskStatusQueued)
		if calls >= 4 {
			status = model.TaskStatus(model.TaskStatusSuccess)
		}
		return &model.Task{TaskID: taskID, UserId: userID, Status: status}, true, nil
	}

	task, err := waitForAsyncImageStreamTask(c, 7, "task_stream", 10*time.Millisecond, time.Millisecond, time.Second, load)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Contains(t, recorder.Body.String(), ": PING\n\n")
}

func TestWaitForAsyncImageStreamTaskErrors(t *testing.T) {
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		return c
	}

	_, err := waitForAsyncImageStreamTask(newContext(), 1, "missing", time.Millisecond, time.Second, time.Second,
		func(int, string) (*model.Task, bool, error) { return nil, false, nil })
	require.EqualError(t, err, "async image task not found")

	_, err = waitForAsyncImageStreamTask(newContext(), 1, "broken", time.Millisecond, time.Second, time.Second,
		func(int, string) (*model.Task, bool, error) { return nil, false, errors.New("database unavailable") })
	require.ErrorContains(t, err, "database unavailable")

	_, err = waitForAsyncImageStreamTask(newContext(), 1, "slow", time.Millisecond, time.Second, 5*time.Millisecond,
		func(userID int, taskID string) (*model.Task, bool, error) {
			return &model.Task{TaskID: taskID, UserId: userID, Status: model.TaskStatusQueued}, true, nil
		})
	require.ErrorContains(t, err, "stream wait exceeded")
}

func TestWriteAsyncImageStreamResultAndError(t *testing.T) {
	resultRecorder := httptest.NewRecorder()
	resultContext, _ := gin.CreateTestContext(resultRecorder)
	resultContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	writeAsyncImageStreamResult(resultContext, dto.ImageResponse{
		Created: 123,
		Data:    []dto.ImageData{{Url: "https://example.com/local.png"}},
	})
	require.Equal(t, "text/event-stream", resultRecorder.Header().Get("Content-Type"))
	require.Contains(t, resultRecorder.Body.String(), `data: {"data":[{"url":"https://example.com/local.png"`)
	require.Contains(t, resultRecorder.Body.String(), "data: [DONE]")

	errorRecorder := httptest.NewRecorder()
	errorContext, _ := gin.CreateTestContext(errorRecorder)
	errorContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	writeAsyncImageStreamError(errorContext, "generation_failed", "provider rejected the prompt")
	require.Contains(t, errorRecorder.Body.String(), `"code":"generation_failed"`)
	require.Contains(t, errorRecorder.Body.String(), "data: [DONE]")
}

func TestShouldFallbackAsyncImageStream(t *testing.T) {
	require.True(t, shouldFallbackAsyncImageStream(&dto.TaskError{StatusCode: http.StatusBadRequest}))
	require.True(t, shouldFallbackAsyncImageStream(&dto.TaskError{StatusCode: http.StatusNotFound}))
	require.False(t, shouldFallbackAsyncImageStream(&dto.TaskError{StatusCode: http.StatusInternalServerError}))
	require.False(t, shouldFallbackAsyncImageStream(&dto.TaskError{StatusCode: http.StatusBadRequest, LocalError: true}))
}

func TestRelayAsyncImageStreamReplaysCompletedLocalTask(t *testing.T) {
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "async-image-stream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TemporaryMedia{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	mediaRoot := t.TempDir()
	t.Setenv("ASYNC_MEDIA_DIR", mediaRoot)
	fileName := "stream-result.png"
	require.NoError(t, os.WriteFile(filepath.Join(mediaRoot, fileName), []byte("local-image"), 0o600))

	const (
		userID    = 23
		taskID    = "task_completed_stream"
		modelName = "stream-bridge-test-model"
		key       = "stream-bridge-key"
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{
		"model":"stream-bridge-test-model",
		"prompt":"draw",
		"stream":true
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", key)
	requestHash, err := asyncImageRequestHash(c)
	require.NoError(t, err)

	idempotencyKey := key
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Task{
		TaskID:          taskID,
		UserId:          userID,
		ChannelId:       9,
		Platform:        constant.TaskPlatformAsyncImage,
		Action:          constant.TaskActionImageGenerations,
		Status:          model.TaskStatusSuccess,
		Progress:        "100%",
		SubmitTime:      now - 5,
		FinishTime:      now,
		Properties:      model.Properties{OriginModelName: modelName, UpstreamModelName: modelName},
		IdempotencyKey:  &idempotencyKey,
		RequestHash:     requestHash,
		SubmissionState: model.TaskSubmissionStateSubmitted,
	}).Error)
	require.NoError(t, db.Create(&model.TemporaryMedia{
		MediaID:     "media_completed_stream",
		TaskID:      taskID,
		Position:    0,
		UserID:      userID,
		FileName:    fileName,
		ContentType: "image/png",
		Size:        int64(len("local-image")),
		ExpiresAt:   now + 300,
	}).Error)

	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyChannelId, 9)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example.com")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(c, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(c, constant.ContextKeyAsyncImageRequestHash, requestHash)
	common.SetContextKey(c, constant.ContextKeyAsyncImageStreamBridge, true)
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	oldPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"stream-bridge-test-model":0}`))
	oldFreePreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	t.Cleanup(func() {
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFreePreConsume
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})

	relayAsyncImageStream(c)
	require.Equal(t, taskID, recorder.Header().Get("X-New-Api-Task-Id"))
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), ": PING\n\n")
	require.Contains(t, recorder.Body.String(), "/v1/images/generations/"+taskID+"/content/media_completed_stream")
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func asyncImageHashJSONContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestAsyncImageRequestHashCanonicalJSON(t *testing.T) {
	a := asyncImageHashJSONContext(t, `{"model":"m","prompt":"draw","async":true,"seed":0}`)
	b := asyncImageHashJSONContext(t, `{
		"seed": 0,
		"async": true,
		"prompt": "draw",
		"model": "m"
	}`)
	c := asyncImageHashJSONContext(t, `{"model":"m","prompt":"different","async":true,"seed":0}`)

	hashA, err := asyncImageRequestHash(a)
	require.NoError(t, err)
	hashB, err := asyncImageRequestHash(b)
	require.NoError(t, err)
	hashC, err := asyncImageRequestHash(c)
	require.NoError(t, err)
	require.Equal(t, hashA, hashB)
	require.NotEqual(t, hashA, hashC)
}

func asyncImageHashMultipartContext(t *testing.T, filename, prompt, content string) *gin.Context {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "m"))
	require.NoError(t, writer.WriteField("prompt", prompt))
	require.NoError(t, writer.WriteField("async", "true"))
	part, err := writer.CreateFormFile("image", filename)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestAsyncImageRequestHashCanonicalMultipart(t *testing.T) {
	a := asyncImageHashMultipartContext(t, "first.png", "edit", "same pixels")
	b := asyncImageHashMultipartContext(t, "renamed.png", "edit", "same pixels")
	c := asyncImageHashMultipartContext(t, "first.png", "edit", "changed pixels")

	hashA, err := asyncImageRequestHash(a)
	require.NoError(t, err)
	hashB, err := asyncImageRequestHash(b)
	require.NoError(t, err)
	hashC, err := asyncImageRequestHash(c)
	require.NoError(t, err)
	require.Equal(t, hashA, hashB, "multipart boundaries and filenames are not request semantics")
	require.NotEqual(t, hashA, hashC)
}

func TestPrepareAsyncImageIdempotencyRejectsInvalidKey(t *testing.T) {
	c := asyncImageHashJSONContext(t, `{"model":"m","prompt":"draw","async":true}`)
	c.Request.Header.Set("Idempotency-Key", "contains whitespace")
	require.Error(t, prepareAsyncImageIdempotency(c))
}

func TestReplayAsyncImageIdempotentTask(t *testing.T) {
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "async-image-replay.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	key := "replay-key"
	require.NoError(t, db.Create(&model.Task{
		TaskID:          "task_replayed",
		UserId:          12,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          model.TaskStatusQueued,
		Progress:        "20%",
		SubmitTime:      123,
		IdempotencyKey:  &key,
		RequestHash:     "request-hash",
		SubmissionState: model.TaskSubmissionStateSubmitted,
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 12)
	common.SetContextKey(c, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(c, constant.ContextKeyAsyncImageRequestHash, "request-hash")
	require.True(t, replayAsyncImageIdempotentTask(c))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "true", recorder.Header().Get("Idempotent-Replayed"))
	require.Contains(t, recorder.Body.String(), `"id":"task_replayed"`)

	conflictRecorder := httptest.NewRecorder()
	conflict, _ := gin.CreateTestContext(conflictRecorder)
	conflict.Set("id", 12)
	common.SetContextKey(conflict, constant.ContextKeyAsyncImageIdempotencyKey, key)
	common.SetContextKey(conflict, constant.ContextKeyAsyncImageRequestHash, "different-hash")
	require.True(t, replayAsyncImageIdempotentTask(conflict))
	require.Equal(t, http.StatusConflict, conflictRecorder.Code)
	require.Contains(t, conflictRecorder.Body.String(), "idempotency_conflict")
}

func TestAsyncImageTaskSubmissionIsNotRetried(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{StatusCode: http.StatusTooManyRequests}, 2))
}
