package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func TestPersistAsyncImageTaskResultAndExpire(t *testing.T) {
	truncate(t)
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	t.Setenv("ASYNC_MEDIA_RETENTION_SECONDS", "300")

	task := &model.Task{
		TaskID:     "task_local_image",
		UserId:     42,
		Action:     constant.TaskActionImageGenerations,
		Status:     model.TaskStatusInProgress,
		Progress:   "30%",
		SubmitTime: time.Now().Unix() - 5,
		Properties: model.Properties{OriginModelName: "nano-banana-pro-1k"},
	}
	result := &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess),
		Images: []dto.ImageData{{
			B64Json:       base64.StdEncoding.EncodeToString(testPNG(t)),
			RevisedPrompt: "revised",
		}},
	}

	require.NoError(t, PersistAsyncImageTaskResult(context.Background(), nil, task, result))
	require.Equal(t, "100%", result.Progress)
	require.Contains(t, result.Url, "/v1/images/generations/task_local_image/content/media_")
	require.NotEmpty(t, result.ResponseData)

	items, err := model.GetTemporaryMediaByTask(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "image/png", items[0].ContentType)
	require.Equal(t, "revised", items[0].RevisedPrompt)
	require.GreaterOrEqual(t, items[0].ExpiresAt, time.Now().Unix()+295)

	media, file, err := OpenTemporaryMedia(task.UserId, task.TaskID, items[0].MediaID)
	require.NoError(t, err)
	require.Equal(t, items[0].MediaID, media.MediaID)
	stored, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, testPNG(t), stored)

	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	response, err := BuildAsyncImageTaskResponse(task)
	require.NoError(t, err)
	require.Equal(t, "completed", response.Status)
	require.False(t, response.OutputExpired)
	require.Len(t, response.Data, 1)

	past := time.Now().Unix() - 1
	require.NoError(t, model.DB.Model(&model.TemporaryMedia{}).
		Where("id = ?", items[0].ID).Update("expires_at", past).Error)
	deleted, err := CleanupExpiredTemporaryMedia(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	response, err = BuildAsyncImageTaskResponse(task)
	require.NoError(t, err)
	require.True(t, response.OutputExpired)
	require.Empty(t, response.Data)
	_, _, err = OpenTemporaryMedia(task.UserId, task.TaskID, items[0].MediaID)
	require.ErrorIs(t, err, ErrTemporaryMediaExpired)
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func TestNormalizeImageContentTypeRejectsNonImage(t *testing.T) {
	require.Empty(t, normalizeImageContentType("text/plain", "text/plain; charset=utf-8"))
	require.Equal(t, "image/png", normalizeImageContentType("application/octet-stream", "image/png"))
}

func TestResolveAsyncImageURL(t *testing.T) {
	baseURL := "https://upstream.example/api"
	channel := &model.Channel{BaseURL: &baseURL}

	resolved, sameOrigin, err := resolveAsyncImageURL(channel, "/generated/image.png")
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example/generated/image.png", resolved)
	require.True(t, sameOrigin)

	resolved, sameOrigin, err = resolveAsyncImageURL(channel, "https://cdn.example/image.png")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/image.png", resolved)
	require.False(t, sameOrigin)
}

func TestAsyncImageResponseDoesNotContainUpstreamURL(t *testing.T) {
	var response dto.AsyncImageTaskResponse
	require.NoError(t, common.Unmarshal([]byte(`{
		"id":"task_public",
		"object":"image.generation",
		"model":"test",
		"status":"completed",
		"progress":"100%",
		"data":[{"url":"/v1/images/generations/task_public/content/media_public"}]
	}`), &response))
	require.True(t, strings.HasPrefix(response.Data[0].Url, "/v1/images/"))
}

type fakeAsyncImagePollingAdaptor struct {
	imageBase64 string
}

func (a *fakeAsyncImagePollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *fakeAsyncImagePollingAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"completed"}`)),
	}, nil
}

func (a *fakeAsyncImagePollingAdaptor) ParseTaskResult(_ []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{
		Status:   string(model.TaskStatusSuccess),
		Progress: "100%",
		Images:   []dto.ImageData{{B64Json: a.imageBase64}},
	}, nil
}

func (a *fakeAsyncImagePollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateAsyncImageTasksCompletesStoredResult(t *testing.T) {
	truncate(t)
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	baseURL := "https://upstream.example"
	channel := &model.Channel{
		Id:      801,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "channel-key",
		Name:    "async-image-test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := &model.Task{
		TaskID:     "task_queue_test",
		Platform:   constant.TaskPlatformAsyncImage,
		UserId:     42,
		ChannelId:  channel.Id,
		Action:     constant.TaskActionImageGenerations,
		Status:     model.TaskStatusQueued,
		Progress:   "20%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "image-model"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
			Key:            "submission-key",
		},
	}
	require.NoError(t, task.Insert())

	previousFactory := GetTaskAdaptorFunc
	imageBase64 := base64.StdEncoding.EncodeToString(testPNG(t))
	GetTaskAdaptorFunc = func(_ constant.TaskPlatform) TaskPollingAdaptor {
		return &fakeAsyncImagePollingAdaptor{imageBase64: imageBase64}
	}
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	updateAsyncImageTasks(context.Background(), []*model.Task{task}, 2)
	updated, exists, err := model.GetByTaskId(task.UserId, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, "100%", updated.Progress)
	media, err := model.GetTemporaryMediaByTask(task.TaskID)
	require.NoError(t, err)
	require.Len(t, media, 1)
}
