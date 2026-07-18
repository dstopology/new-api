package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func UsesLocalOpenAIVideoAssets(channelType int) bool {
	return channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeSora
}

// PersistOpenAIVideoTaskResult downloads the completed upstream object before
// the public task is allowed to transition to completed.
func PersistOpenAIVideoTaskResult(ctx context.Context, channel *model.Channel, task *model.Task) error {
	if channel == nil || task == nil {
		return errors.New("invalid video task result")
	}
	if !UsesLocalOpenAIVideoAssets(channel.Type) {
		return fmt.Errorf("channel type %d does not use local OpenAI video assets", channel.Type)
	}
	if task.GetUpstreamTaskID() == "" {
		return errors.New("upstream video task id is missing")
	}

	root, err := asyncMediaRoot()
	if err != nil {
		return fmt.Errorf("initialize async media directory: %w", err)
	}
	media, exists, err := model.GetTemporaryMediaByTaskPosition(task.TaskID, 0)
	if err != nil {
		return fmt.Errorf("query temporary video: %w", err)
	}
	if exists && media.DeletedAt == 0 && temporaryMediaFileExists(root, media.FileName) {
		expiresAt := time.Now().Unix() + asyncMediaRetentionSeconds()
		if err := model.RefreshTemporaryMediaExpiry(task.TaskID, expiresAt); err != nil {
			return fmt.Errorf("refresh temporary video expiry: %w", err)
		}
		return nil
	}

	mediaID, err := newTemporaryMediaID()
	if err != nil {
		return err
	}
	reader, contentType, closeSource, err := openOpenAIVideoSource(ctx, channel, task)
	if err != nil {
		return err
	}
	fileData, writeErr := writeTemporaryVideoFile(root, mediaID, reader, contentType)
	closeErr := closeSource()
	if writeErr != nil {
		return fmt.Errorf("store video: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(filepath.Join(root, fileData.FileName))
		return fmt.Errorf("close video source: %w", closeErr)
	}

	now := time.Now().Unix()
	oldFileName := ""
	if exists {
		oldFileName = media.FileName
	} else {
		media = &model.TemporaryMedia{}
		media.CreatedAt = now
	}
	media.MediaID = mediaID
	media.TaskID = task.TaskID
	media.Position = 0
	media.UserID = task.UserId
	media.FileName = fileData.FileName
	media.ContentType = fileData.ContentType
	media.Size = fileData.Size
	media.SHA256 = fileData.SHA256
	media.RevisedPrompt = ""
	media.ExpiresAt = now + asyncMediaRetentionSeconds()
	media.DownloadedAt = 0
	media.DeletedAt = 0
	media.UpdatedAt = now

	if exists {
		err = media.Update()
	} else {
		err = media.Insert()
	}
	if err != nil {
		_ = os.Remove(filepath.Join(root, fileData.FileName))
		return fmt.Errorf("save temporary video metadata: %w", err)
	}
	if oldFileName != "" && oldFileName != fileData.FileName {
		_ = removeTemporaryMediaFile(root, oldFileName)
	}
	return nil
}

func newTemporaryMediaID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return "media_" + key, nil
}

func openOpenAIVideoSource(ctx context.Context, channel *model.Channel, task *model.Task) (io.Reader, string, func() error, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(constant.ChannelBaseURLs[channel.Type], "/")
	}
	if baseURL == "" {
		return nil, "", nil, errors.New("video channel base url is missing")
	}
	videoURL := fmt.Sprintf("%s/v1/videos/%s/content", baseURL, url.PathEscape(task.GetUpstreamTaskID()))
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", nil, fmt.Errorf("video url blocked: %w", err)
	}

	timeout := common.GetEnvOrDefault("ASYNC_VIDEO_DOWNLOAD_TIMEOUT_SECONDS", defaultAsyncVideoDownloadTimeout)
	if timeout <= 0 {
		timeout = defaultAsyncVideoDownloadTimeout
	}
	downloadCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, videoURL, nil)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	key := task.PrivateData.Key
	if key == "" {
		key = channel.Key
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client, err := GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	if client == nil {
		cancel()
		return nil, "", nil, errors.New("video download client is not initialized")
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, "", nil, fmt.Errorf("download completed video: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		cancel()
		return nil, "", nil, fmt.Errorf("video server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > asyncVideoMaxBytes() {
		_ = resp.Body.Close()
		cancel()
		return nil, "", nil, fmt.Errorf("video exceeds %d bytes", asyncVideoMaxBytes())
	}
	closeSource := func() error {
		err := resp.Body.Close()
		cancel()
		return err
	}
	return resp.Body, resp.Header.Get("Content-Type"), closeSource, nil
}

func BuildOpenAIVideoTaskResponse(task *model.Task) (*dto.OpenAIVideo, error) {
	if task == nil {
		return nil, ErrTemporaryMediaNotFound
	}
	response := dto.NewOpenAIVideo()
	response.ID = task.TaskID
	response.TaskID = task.TaskID
	response.Model = task.Properties.OriginModelName
	if response.Model == "" {
		response.Model = task.Properties.UpstreamModelName
	}
	response.Status = task.Status.ToVideoStatus()
	if task.Status == model.TaskStatusNotStart {
		response.Status = dto.VideoStatusQueued
	}
	response.SetProgressStr(task.Progress)
	response.CreatedAt = task.SubmitTime
	if response.CreatedAt == 0 {
		response.CreatedAt = task.CreatedAt
	}
	response.CompletedAt = task.FinishTime

	var previous dto.OpenAIVideo
	if len(task.Data) > 0 && common.Unmarshal(task.Data, &previous) == nil {
		response.Seconds = previous.Seconds
		response.Size = previous.Size
		response.RemixedFromVideoID = previous.RemixedFromVideoID
	}
	if task.Status == model.TaskStatusFailure {
		response.Error = &dto.OpenAIVideoError{Code: "generation_failed", Message: task.FailReason}
		return response, nil
	}
	if task.Status != model.TaskStatusSuccess {
		return response, nil
	}

	media, exists, err := model.GetTemporaryMediaByTaskPosition(task.TaskID, 0)
	if err != nil {
		return nil, err
	}
	root, rootErr := asyncMediaRoot()
	if !exists || media == nil || media.DeletedAt != 0 || media.ExpiresAt <= time.Now().Unix() || rootErr != nil || !temporaryMediaFileExists(root, media.FileName) {
		response.SetMetadata("output_expired", true)
		return response, nil
	}
	contentURL := temporaryVideoContentURL(task.TaskID)
	response.ExpiresAt = media.ExpiresAt
	response.VideoURL = contentURL
	response.SetMetadata("url", contentURL)
	response.SetMetadata("asset_id", media.MediaID)
	response.SetMetadata("content_type", media.ContentType)
	response.SetMetadata("size", media.Size)
	response.SetMetadata("sha256", media.SHA256)
	return response, nil
}

func BuildOpenAIVideoTaskResponseData(task *model.Task) ([]byte, error) {
	response, err := BuildOpenAIVideoTaskResponse(task)
	if err != nil {
		return nil, err
	}
	return common.Marshal(response)
}

func temporaryVideoContentURL(taskID string) string {
	path := fmt.Sprintf("/v1/videos/%s/content", url.PathEscape(taskID))
	return strings.TrimRight(system_setting.ServerAddress, "/") + path
}
