package imageasync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const contextRequestKey = "async_image_request"

type upstreamTaskResponse struct {
	ID        string                   `json:"id"`
	Object    string                   `json:"object"`
	Model     string                   `json:"model"`
	Status    string                   `json:"status"`
	Progress  json.RawMessage          `json:"progress"`
	CreatedAt int64                    `json:"created_at"`
	Data      []dto.ImageData          `json:"data,omitempty"`
	Error     *dto.AsyncImageTaskError `json:"error,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	request, err := helper.GetAndValidOpenAIImageRequest(c, info.RelayMode)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	streamBridge := common.GetContextKeyBool(c, constant.ContextKeyAsyncImageStreamBridge)
	if (request.Async == nil || !*request.Async) && !streamBridge {
		return service.TaskErrorWrapperLocal(fmt.Errorf("async must be true"), "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	switch {
	case info.RelayMode == relayconstant.RelayModeImagesEdits, strings.HasSuffix(c.Request.URL.Path, "/edits"):
		info.Action = constant.TaskActionImageEdits
	default:
		info.Action = constant.TaskActionImageGenerations
	}
	c.Set(contextRequestKey, request)
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	request, ok := c.Get(contextRequestKey)
	if !ok {
		return nil
	}
	imageRequest, ok := request.(*dto.ImageRequest)
	if !ok || imageRequest.N == nil || *imageRequest.N <= 1 {
		return nil
	}
	return map[string]float64{"n": float64(*imageRequest.N)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	path := "generations"
	if info.Action == constant.TaskActionImageEdits {
		path = "edits"
	}
	return fmt.Sprintf("%s/v1/images/%s", a.baseURL, path), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	req.Header.Set("Accept", "application/json")
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = c.GetString(common.RequestIdKey)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		return buildMultipartBody(c, info)
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, fmt.Errorf("get request body: %w", err)
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	modelJSON, err := common.Marshal(info.UpstreamModelName)
	if err != nil {
		return nil, fmt.Errorf("encode upstream model: %w", err)
	}
	asyncJSON, err := common.Marshal(true)
	if err != nil {
		return nil, fmt.Errorf("encode async flag: %w", err)
	}
	fields["model"] = modelJSON
	fields["async"] = asyncJSON
	delete(fields, "stream")
	body, err = common.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	info.UpstreamRequestBodySize = int64(len(body))
	return bytes.NewReader(body), nil
}

func buildMultipartBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, fmt.Errorf("parse multipart request: %w", err)
	}
	defer form.RemoveAll()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", info.UpstreamModelName); err != nil {
		return nil, err
	}
	if err := writer.WriteField("async", "true"); err != nil {
		return nil, err
	}
	for field, values := range form.Value {
		if field == "model" || field == "async" || field == "stream" {
			continue
		}
		for _, value := range values {
			if err := writer.WriteField(field, value); err != nil {
				return nil, fmt.Errorf("write field %s: %w", field, err)
			}
		}
	}
	for field, files := range form.File {
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				return nil, fmt.Errorf("open multipart file: %w", err)
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
				"name": field, "filename": fileHeader.Filename,
			}))
			contentType := fileHeader.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			header.Set("Content-Type", contentType)
			part, createErr := writer.CreatePart(header)
			if createErr == nil {
				_, createErr = io.Copy(part, file)
			}
			_ = file.Close()
			if createErr != nil {
				return nil, fmt.Errorf("copy multipart file: %w", createErr)
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	info.UpstreamRequestBodySize = int64(body.Len())
	return &body, nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}

func (a *TaskAdaptor) DoResponse(_ *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var response upstreamTaskResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	upstreamTaskID := strings.TrimSpace(response.ID)
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream task id is empty"), "invalid_response", http.StatusBadGateway)
	}

	object := response.Object
	if object == "" {
		object = "image.generation"
	}
	modelName := response.Model
	if modelName == "" {
		modelName = info.OriginModelName
	}
	sanitizedResponse := dto.AsyncImageTaskResponse{
		ID:        info.PublicTaskID,
		Object:    object,
		Model:     modelName,
		Status:    response.Status,
		Progress:  normalizeProgress(response.Progress),
		CreatedAt: response.CreatedAt,
		Data:      response.Data,
		Error:     response.Error,
	}
	sanitized, err := common.Marshal(sanitizedResponse)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return upstreamTaskID, sanitized, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	path := "generations"
	if action, _ := body["action"].(string); action == constant.TaskActionImageEdits {
		path = "edits"
	}
	uri := fmt.Sprintf("%s/v1/images/%s/%s", strings.TrimRight(baseURL, "/"), path, url.PathEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly ||
		resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("temporary upstream task query status %d", resp.StatusCode)
	}
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var response upstreamTaskResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode async image task response: %w", err)
	}
	result := &relaycommon.TaskInfo{TaskID: response.ID, Progress: normalizeProgress(response.Progress), Images: response.Data}
	switch strings.ToLower(response.Status) {
	case "queued", "pending", "submitted":
		result.Status = string(model.TaskStatusQueued)
	case "in_progress", "processing", "running":
		result.Status = string(model.TaskStatusInProgress)
	case "completed", "succeeded", "success":
		result.Status = string(model.TaskStatusSuccess)
	case "failed", "cancelled", "canceled":
		result.Status = string(model.TaskStatusFailure)
		if response.Error != nil {
			result.Reason = response.Error.Message
		}
		if result.Reason == "" {
			result.Reason = "image generation failed"
		}
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string { return nil }

func (a *TaskAdaptor) GetChannelName() string { return "Async Image" }

func normalizeProgress(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var progress string
	if err := common.Unmarshal(raw, &progress); err == nil {
		progress = strings.TrimSpace(progress)
		if progress == "" || strings.HasSuffix(progress, "%") {
			return progress
		}
		return progress + "%"
	}
	var numeric float64
	if err := common.Unmarshal(raw, &numeric); err == nil {
		return fmt.Sprintf("%g%%", numeric)
	}
	return ""
}
