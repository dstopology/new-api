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
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

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

func TestPersistOpenAIVideoTaskResultAndExpire(t *testing.T) {
	truncate(t)
	InitHttpClient()
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	t.Setenv("ASYNC_MEDIA_RETENTION_SECONDS", "300")
	t.Setenv("ASYNC_VIDEO_MAX_FILE_MB", "1")
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = previousFetchSetting })

	video := testMP4()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/upstream-video/content" {
			t.Errorf("unexpected video path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer task-key" {
			t.Errorf("unexpected authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(video)
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := &model.Channel{Type: constant.ChannelTypeSora, BaseURL: &baseURL, Key: "channel-key"}
	task := &model.Task{
		TaskID:     "task_local_video",
		UserId:     42,
		Status:     model.TaskStatusInProgress,
		Progress:   "30%",
		SubmitTime: time.Now().Unix() - 5,
		Properties: model.Properties{OriginModelName: "veo-3-1-fast"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-video",
			Key:            "task-key",
		},
	}

	require.NoError(t, PersistOpenAIVideoTaskResult(context.Background(), channel, task))
	items, err := model.GetTemporaryMediaByTask(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "video/mp4", items[0].ContentType)
	require.Equal(t, int64(len(video)), items[0].Size)
	require.GreaterOrEqual(t, items[0].ExpiresAt, time.Now().Unix()+295)

	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	response, err := BuildOpenAIVideoTaskResponse(task)
	require.NoError(t, err)
	require.Equal(t, task.TaskID, response.ID)
	require.Equal(t, task.TaskID, response.TaskID)
	require.Equal(t, dto.VideoStatusCompleted, response.Status)
	require.Contains(t, response.VideoURL, "/v1/videos/task_local_video/content")
	require.Equal(t, response.VideoURL, response.Metadata["url"])
	require.NotContains(t, response.VideoURL, upstream.URL)

	media, file, err := OpenTemporaryMediaByTaskPosition(task.UserId, task.TaskID, 0)
	require.NoError(t, err)
	require.Equal(t, items[0].MediaID, media.MediaID)
	stored, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, video, stored)

	past := time.Now().Unix() - 1
	require.NoError(t, model.DB.Model(&model.TemporaryMedia{}).
		Where("id = ?", items[0].ID).Update("expires_at", past).Error)
	response, err = BuildOpenAIVideoTaskResponse(task)
	require.NoError(t, err)
	require.Empty(t, response.VideoURL)
	require.Equal(t, true, response.Metadata["output_expired"])
	_, _, err = OpenTemporaryMediaByTaskPosition(task.UserId, task.TaskID, 0)
	require.ErrorIs(t, err, ErrTemporaryMediaExpired)
}

func testMP4() []byte {
	return append(
		[]byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00, 'i', 's', 'o', 'm', 'i', 's', 'o', '2', 'a', 'v', 'c', '1', 'm', 'p', '4', '1'},
		bytes.Repeat([]byte{0x00}, 256)...,
	)
}

type completedVideoPollingAdaptor struct{}

func (a *completedVideoPollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *completedVideoPollingAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"completed","progress":100}`)),
	}, nil
}

func (a *completedVideoPollingAdaptor) ParseTaskResult(_ []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), Progress: "100%"}, nil
}

func (a *completedVideoPollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func TestVideoPollingCompletesOnlyWithLocalAsset(t *testing.T) {
	truncate(t)
	InitHttpClient()
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	t.Setenv("ASYNC_MEDIA_RETENTION_SECONDS", "300")
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = previousFetchSetting })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/upstream-poll-video/content" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(testMP4())
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := &model.Channel{
		Type:    constant.ChannelTypeSora,
		Name:    "local-video-polling-test",
		Key:     "channel-key",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := &model.Task{
		TaskID:     "task_video_poll",
		Platform:   constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)),
		UserId:     42,
		ChannelId:  channel.Id,
		Status:     model.TaskStatusQueued,
		Progress:   "20%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "veo-3-1-fast"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-poll-video",
			Key:            "task-key",
		},
	}
	require.NoError(t, task.Insert())

	err := updateVideoSingleTask(
		context.Background(),
		&completedVideoPollingAdaptor{},
		channel,
		task.GetUpstreamTaskID(),
		map[string]*model.Task{task.GetUpstreamTaskID(): task},
	)
	require.NoError(t, err)

	updated, exists, err := model.GetByTaskId(task.UserId, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, "100%", updated.Progress)
	require.Contains(t, updated.PrivateData.ResultURL, "/v1/videos/task_video_poll/content")
	var response dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(updated.Data, &response))
	require.Equal(t, updated.TaskID, response.ID)
	require.Equal(t, updated.TaskID, response.TaskID)
	require.Contains(t, response.VideoURL, "/v1/videos/task_video_poll/content")
	require.NotContains(t, string(updated.Data), "upstream-poll-video")
	media, err := model.GetTemporaryMediaByTask(updated.TaskID)
	require.NoError(t, err)
	require.Len(t, media, 1)
}

func TestVideoPollingDoesNotCompleteWhenLocalAssetDownloadFails(t *testing.T) {
	truncate(t)
	InitHttpClient()
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = previousFetchSetting })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	baseURL := upstream.URL
	channel := &model.Channel{
		Type:    constant.ChannelTypeSora,
		Name:    "failed-local-video-polling-test",
		Key:     "channel-key",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := &model.Task{
		TaskID:     "task_video_download_failure",
		Platform:   constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)),
		UserId:     42,
		ChannelId:  channel.Id,
		Status:     model.TaskStatusQueued,
		Progress:   "20%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "veo-3-1-fast"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-failed-video",
			Key:            "task-key",
		},
	}
	require.NoError(t, task.Insert())

	err := updateVideoSingleTask(
		context.Background(),
		&completedVideoPollingAdaptor{},
		channel,
		task.GetUpstreamTaskID(),
		map[string]*model.Task{task.GetUpstreamTaskID(): task},
	)
	require.ErrorContains(t, err, "persist completed video")
	updated, exists, err := model.GetByTaskId(task.UserId, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), updated.Status)
	require.Equal(t, "20%", updated.Progress)
	media, err := model.GetTemporaryMediaByTask(updated.TaskID)
	require.NoError(t, err)
	require.Empty(t, media)
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

func TestNormalizeVideoContentTypeRejectsNonVideo(t *testing.T) {
	require.Equal(t, "video/mp4", normalizeVideoContentType("application/octet-stream", "video/mp4"))
	require.Equal(t, "video/webm", normalizeVideoContentType("video/webm", "application/octet-stream"))
	require.Empty(t, normalizeVideoContentType("text/html", "text/html; charset=utf-8"))
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

func TestBuildAsyncImageTaskResponseExposesSubmissionFailureKind(t *testing.T) {
	for _, test := range []struct {
		state string
		code  string
	}{
		{state: model.TaskSubmissionStateUnknown, code: "submission_unknown"},
		{state: model.TaskSubmissionStateRejected, code: "submission_rejected"},
	} {
		t.Run(test.state, func(t *testing.T) {
			response, err := BuildAsyncImageTaskResponse(&model.Task{
				TaskID:          "task_failure",
				Platform:        constant.TaskPlatformAsyncImage,
				Status:          model.TaskStatusFailure,
				Progress:        "100%",
				FailReason:      "submission failed",
				SubmissionState: test.state,
			})
			require.NoError(t, err)
			require.NotNil(t, response.Error)
			require.Equal(t, test.code, response.Error.Code)
		})
	}
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
