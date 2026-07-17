package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	defaultAsyncMediaRetentionSeconds = 300
	defaultAsyncMediaSweepSeconds     = 30
	defaultAsyncMediaMaxFileMB        = 64
	defaultAsyncMediaDownloadTimeout  = 90
	defaultAsyncMediaMetadataHours    = 24
)

var (
	ErrTemporaryMediaNotFound = errors.New("temporary media not found")
	ErrTemporaryMediaExpired  = errors.New("temporary media expired")
)

type temporaryMediaFile struct {
	FileName    string
	ContentType string
	Size        int64
	SHA256      string
}

func asyncMediaRoot() (string, error) {
	root := strings.TrimSpace(common.GetEnvOrDefaultString("ASYNC_MEDIA_DIR", "./async-media"))
	if root == "" {
		root = "./async-media"
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func asyncMediaRetentionSeconds() int64 {
	seconds := common.GetEnvOrDefault("ASYNC_MEDIA_RETENTION_SECONDS", defaultAsyncMediaRetentionSeconds)
	if seconds <= 0 {
		return defaultAsyncMediaRetentionSeconds
	}
	return int64(seconds)
}

func asyncMediaMaxBytes() int64 {
	megabytes := common.GetEnvOrDefault("ASYNC_MEDIA_MAX_FILE_MB", defaultAsyncMediaMaxFileMB)
	if megabytes <= 0 {
		megabytes = defaultAsyncMediaMaxFileMB
	}
	return int64(megabytes) * 1024 * 1024
}

func PersistAsyncImageTaskResult(ctx context.Context, channel *model.Channel, task *model.Task, result *relaycommon.TaskInfo) error {
	if task == nil || result == nil {
		return errors.New("invalid async image task result")
	}
	if len(result.Images) == 0 {
		return errors.New("upstream completed without image data")
	}
	root, err := asyncMediaRoot()
	if err != nil {
		return fmt.Errorf("initialize async media directory: %w", err)
	}

	stored := make([]*model.TemporaryMedia, 0, len(result.Images))
	for index, image := range result.Images {
		media, exists, err := model.GetTemporaryMediaByTaskPosition(task.TaskID, index)
		if err != nil {
			return fmt.Errorf("query temporary media: %w", err)
		}
		if exists && media.DeletedAt == 0 && temporaryMediaFileExists(root, media.FileName) {
			stored = append(stored, media)
			continue
		}

		mediaID := ""
		if exists {
			mediaID = media.MediaID
		}
		if mediaID == "" {
			key, err := common.GenerateRandomCharsKey(32)
			if err != nil {
				return fmt.Errorf("generate media id: %w", err)
			}
			mediaID = "media_" + key
		}

		reader, contentType, closeSource, err := openAsyncImageSource(ctx, channel, task.PrivateData.Key, image)
		if err != nil {
			return fmt.Errorf("open image %d: %w", index, err)
		}
		fileData, writeErr := writeTemporaryMediaFile(root, mediaID, reader, contentType)
		closeErr := closeSource()
		if writeErr != nil {
			return fmt.Errorf("store image %d: %w", index, writeErr)
		}
		if closeErr != nil {
			_ = os.Remove(filepath.Join(root, fileData.FileName))
			return fmt.Errorf("close image %d source: %w", index, closeErr)
		}

		now := time.Now().Unix()
		oldFileName := ""
		if exists {
			oldFileName = media.FileName
		} else {
			media = &model.TemporaryMedia{}
		}
		media.MediaID = mediaID
		media.TaskID = task.TaskID
		media.Position = index
		media.UserID = task.UserId
		media.FileName = fileData.FileName
		media.ContentType = fileData.ContentType
		media.Size = fileData.Size
		media.SHA256 = fileData.SHA256
		media.RevisedPrompt = image.RevisedPrompt
		media.ExpiresAt = now + asyncMediaRetentionSeconds()
		media.DeletedAt = 0
		media.UpdatedAt = now

		if exists {
			err = media.Update()
		} else {
			err = media.Insert()
		}
		if err != nil {
			_ = os.Remove(filepath.Join(root, fileData.FileName))
			return fmt.Errorf("save temporary media metadata: %w", err)
		}
		if oldFileName != "" && oldFileName != fileData.FileName {
			_ = removeTemporaryMediaFile(root, oldFileName)
		}
		stored = append(stored, media)
	}

	expiresAt := time.Now().Unix() + asyncMediaRetentionSeconds()
	if err := model.RefreshTemporaryMediaExpiry(task.TaskID, expiresAt); err != nil {
		return fmt.Errorf("refresh temporary media expiry: %w", err)
	}
	for _, media := range stored {
		media.ExpiresAt = expiresAt
	}

	responseData := make([]dto.ImageData, 0, len(stored))
	for _, media := range stored {
		responseData = append(responseData, temporaryMediaImageData(task, media))
	}
	response := dto.AsyncImageTaskResponse{
		ID:          task.TaskID,
		Object:      "image.generation",
		Model:       task.Properties.OriginModelName,
		Status:      "completed",
		Progress:    "100%",
		CreatedAt:   task.SubmitTime,
		CompletedAt: time.Now().Unix(),
		ExpiresAt:   expiresAt,
		Data:        responseData,
	}
	responseBytes, err := common.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode async image response: %w", err)
	}
	result.ResponseData = responseBytes
	result.Progress = "100%"
	result.Url = responseData[0].Url
	return nil
}

func openAsyncImageSource(ctx context.Context, channel *model.Channel, taskAPIKey string, image dto.ImageData) (io.Reader, string, func() error, error) {
	if image.B64Json == "" && strings.HasPrefix(strings.TrimSpace(image.Url), "data:") {
		image.B64Json = strings.TrimSpace(image.Url)
		image.Url = ""
	}
	if strings.TrimSpace(image.B64Json) != "" {
		payload := strings.TrimSpace(image.B64Json)
		contentType := ""
		if strings.HasPrefix(payload, "data:") {
			if comma := strings.IndexByte(payload, ','); comma >= 0 {
				header := payload[:comma]
				payload = payload[comma+1:]
				if semicolon := strings.IndexByte(header, ';'); semicolon > len("data:") {
					contentType = header[len("data:"):semicolon]
				}
			}
		}
		encoding := base64.StdEncoding
		if len(payload)%4 != 0 {
			encoding = base64.RawStdEncoding
		}
		return base64.NewDecoder(encoding, strings.NewReader(payload)), contentType, func() error { return nil }, nil
	}

	imageURL := strings.TrimSpace(image.Url)
	if imageURL == "" {
		return nil, "", nil, errors.New("image contains neither url nor b64_json")
	}
	imageURL, sameUpstreamOrigin, err := resolveAsyncImageURL(channel, imageURL)
	if err != nil {
		return nil, "", nil, err
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(imageURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", nil, fmt.Errorf("image url blocked: %w", err)
	}

	timeout := common.GetEnvOrDefault("ASYNC_MEDIA_DOWNLOAD_TIMEOUT_SECONDS", defaultAsyncMediaDownloadTimeout)
	downloadCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, imageURL, nil)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	if sameUpstreamOrigin {
		if taskAPIKey == "" && channel != nil {
			taskAPIKey = channel.Key
		}
		if taskAPIKey != "" {
			req.Header.Set("Authorization", "Bearer "+taskAPIKey)
		}
	}
	proxy := ""
	if channel != nil {
		proxy = channel.GetSetting().Proxy
	}
	client, err := GetHttpClientWithProxy(proxy)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		cancel()
		return nil, "", nil, fmt.Errorf("image server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > asyncMediaMaxBytes() {
		_ = resp.Body.Close()
		cancel()
		return nil, "", nil, fmt.Errorf("image exceeds %d bytes", asyncMediaMaxBytes())
	}
	closeSource := func() error {
		err := resp.Body.Close()
		cancel()
		return err
	}
	return resp.Body, resp.Header.Get("Content-Type"), closeSource, nil
}

func resolveAsyncImageURL(channel *model.Channel, imageURL string) (string, bool, error) {
	parsed, err := url.Parse(imageURL)
	if err != nil {
		return "", false, fmt.Errorf("parse image url: %w", err)
	}
	baseURL := ""
	if channel != nil {
		baseURL = strings.TrimSpace(channel.GetBaseURL())
	}
	if !parsed.IsAbs() {
		if baseURL == "" {
			return "", false, errors.New("relative image url requires channel base url")
		}
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", false, fmt.Errorf("parse channel base url: %w", err)
		}
		parsed = base.ResolveReference(parsed)
	}
	sameOrigin := false
	if baseURL != "" {
		if base, err := url.Parse(baseURL); err == nil {
			sameOrigin = strings.EqualFold(base.Scheme, parsed.Scheme) && strings.EqualFold(base.Host, parsed.Host)
		}
	}
	return parsed.String(), sameOrigin, nil
}

func writeTemporaryMediaFile(root, mediaID string, reader io.Reader, declaredContentType string) (_ *temporaryMediaFile, retErr error) {
	temporary, err := os.CreateTemp(root, ".async-image-*.part")
	if err != nil {
		return nil, err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if retErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()

	hash := sha256.New()
	maxBytes := asyncMediaMaxBytes()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if written == 0 {
		return nil, errors.New("image is empty")
	}
	if written > maxBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", maxBytes)
	}
	if err := temporary.Sync(); err != nil {
		return nil, err
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	header := make([]byte, 512)
	headerLength, _ := io.ReadFull(temporary, header)
	sniffedContentType := http.DetectContentType(header[:headerLength])
	contentType := normalizeImageContentType(declaredContentType, sniffedContentType)
	if contentType == "" {
		return nil, fmt.Errorf("unsupported image content type %q", declaredContentType)
	}
	if err := temporary.Close(); err != nil {
		return nil, err
	}

	fileName := mediaID + imageExtension(contentType)
	finalPath := filepath.Join(root, fileName)
	if err := os.Rename(temporaryName, finalPath); err != nil {
		return nil, err
	}
	return &temporaryMediaFile{
		FileName:    fileName,
		ContentType: contentType,
		Size:        written,
		SHA256:      fmt.Sprintf("%x", hash.Sum(nil)),
	}, nil
}

func normalizeImageContentType(declared, sniffed string) string {
	if parsed, _, err := mime.ParseMediaType(declared); err == nil {
		declared = strings.ToLower(parsed)
	} else {
		declared = ""
	}
	if strings.HasPrefix(strings.ToLower(sniffed), "image/") {
		return strings.ToLower(sniffed)
	}
	switch declared {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/avif":
		return declared
	default:
		return ""
	}
}

func imageExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	default:
		return ".png"
	}
}

func temporaryMediaFileExists(root, fileName string) bool {
	if filepath.Base(fileName) != fileName || fileName == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, fileName))
	return err == nil && !info.IsDir()
}

func removeTemporaryMediaFile(root, fileName string) error {
	if filepath.Base(fileName) != fileName || fileName == "" {
		return errors.New("invalid temporary media filename")
	}
	err := os.Remove(filepath.Join(root, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func temporaryMediaContentURL(task *model.Task, mediaID string) string {
	segment := "generations"
	if task.Action == constant.TaskActionImageEdits {
		segment = "edits"
	}
	path := fmt.Sprintf("/v1/images/%s/%s/content/%s", segment, url.PathEscape(task.TaskID), url.PathEscape(mediaID))
	return strings.TrimRight(system_setting.ServerAddress, "/") + path
}

func temporaryMediaImageData(task *model.Task, media *model.TemporaryMedia) dto.ImageData {
	return dto.ImageData{
		Url:           temporaryMediaContentURL(task, media.MediaID),
		B64Json:       "",
		RevisedPrompt: media.RevisedPrompt,
		ExpiresAt:     media.ExpiresAt,
	}
}

func BuildAsyncImageTaskResponse(task *model.Task) (*dto.AsyncImageTaskResponse, error) {
	if task == nil {
		return nil, ErrTemporaryMediaNotFound
	}
	response := &dto.AsyncImageTaskResponse{
		ID:          task.TaskID,
		Object:      "image.generation",
		Model:       task.Properties.OriginModelName,
		Status:      asyncImagePublicStatus(task.Status),
		Progress:    task.Progress,
		CreatedAt:   task.SubmitTime,
		CompletedAt: task.FinishTime,
	}
	if response.Model == "" {
		response.Model = task.Properties.UpstreamModelName
	}
	if response.Progress == "" {
		response.Progress = "0%"
	}
	if task.Status == model.TaskStatusNotStart && len(task.Data) > 0 {
		var submitted dto.AsyncImageTaskResponse
		if err := common.Unmarshal(task.Data, &submitted); err == nil && submitted.Progress != "" {
			response.Progress = submitted.Progress
		}
	}
	if task.Status == model.TaskStatusFailure {
		response.Error = &dto.AsyncImageTaskError{Code: "generation_failed", Message: task.FailReason}
		return response, nil
	}
	if task.Status != model.TaskStatusSuccess {
		return response, nil
	}

	media, err := model.GetTemporaryMediaByTask(task.TaskID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	root, rootErr := asyncMediaRoot()
	for _, item := range media {
		if item.DeletedAt != 0 || item.ExpiresAt <= now || rootErr != nil || !temporaryMediaFileExists(root, item.FileName) {
			response.OutputExpired = true
			continue
		}
		response.Data = append(response.Data, temporaryMediaImageData(task, item))
		if response.ExpiresAt == 0 || item.ExpiresAt < response.ExpiresAt {
			response.ExpiresAt = item.ExpiresAt
		}
	}
	if len(response.Data) == 0 {
		response.OutputExpired = true
	}
	return response, nil
}

func asyncImagePublicStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusInProgress:
		return "in_progress"
	default:
		return "queued"
	}
}

func OpenTemporaryMedia(userID int, taskID, mediaID string) (*model.TemporaryMedia, *os.File, error) {
	media, exists, err := model.GetTemporaryMediaForUser(userID, taskID, mediaID)
	if err != nil {
		return nil, nil, err
	}
	if !exists || media == nil {
		return nil, nil, ErrTemporaryMediaNotFound
	}
	if media.DeletedAt != 0 || media.ExpiresAt <= time.Now().Unix() {
		_ = deleteTemporaryMedia(media)
		return media, nil, ErrTemporaryMediaExpired
	}
	root, err := asyncMediaRoot()
	if err != nil {
		return media, nil, err
	}
	if filepath.Base(media.FileName) != media.FileName || media.FileName == "" {
		return media, nil, ErrTemporaryMediaNotFound
	}
	file, err := os.Open(filepath.Join(root, media.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return media, nil, ErrTemporaryMediaNotFound
	}
	if err != nil {
		return media, nil, err
	}
	_ = model.MarkTemporaryMediaDownloaded(media.ID, time.Now().Unix())
	return media, file, nil
}

func deleteTemporaryMedia(media *model.TemporaryMedia) error {
	root, err := asyncMediaRoot()
	if err != nil {
		return err
	}
	if err := removeTemporaryMediaFile(root, media.FileName); err != nil {
		return err
	}
	return model.MarkTemporaryMediaDeleted(media.ID, time.Now().Unix())
}

func CleanupExpiredTemporaryMedia(ctx context.Context) (int, error) {
	items, err := model.GetExpiredTemporaryMedia(time.Now().Unix(), 200)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, item := range items {
		if err := deleteTemporaryMedia(item); err != nil {
			logger.LogError(ctx, fmt.Sprintf("delete temporary media %s: %v", item.MediaID, err))
			continue
		}
		deleted++
	}
	metadataHours := common.GetEnvOrDefault("ASYNC_MEDIA_METADATA_RETENTION_HOURS", defaultAsyncMediaMetadataHours)
	if metadataHours > 0 {
		cutoff := time.Now().Add(-time.Duration(metadataHours) * time.Hour).Unix()
		if _, err := model.DeleteTemporaryMediaMetadataBefore(cutoff, 1000); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func StartTemporaryMediaCleanup() {
	if _, err := asyncMediaRoot(); err != nil {
		common.SysError("initialize async media directory: " + err.Error())
		return
	}
	go func() {
		interval := common.GetEnvOrDefault("ASYNC_MEDIA_SWEEP_SECONDS", defaultAsyncMediaSweepSeconds)
		if interval <= 0 {
			interval = defaultAsyncMediaSweepSeconds
		}
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			if deleted, err := CleanupExpiredTemporaryMedia(context.Background()); err != nil {
				common.SysError("cleanup expired async media: " + err.Error())
			} else if deleted > 0 {
				common.SysLog(fmt.Sprintf("deleted %d expired async media files", deleted))
			}
			<-ticker.C
		}
	}()
}
