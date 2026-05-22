package ai_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRunningHubBaseURL      = "https://www.runninghub.cn"
	DefaultInfinitetalkAIAppID    = "2006270540499128322"
	DefaultRunningHubPollInterval = 3 * time.Second
	DefaultRunningHubPollTimeout  = 60 * time.Minute
	RunningHubStatusQueued        = "QUEUED"
	RunningHubStatusRunning       = "RUNNING"
	RunningHubStatusSuccess       = "SUCCESS"
	RunningHubStatusFailed        = "FAILED"
)

type RunningHubClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type RunningHubNodeInfo struct {
	NodeID      string `json:"nodeId"`
	FieldName   string `json:"fieldName"`
	FieldValue  any    `json:"fieldValue"`
	Description string `json:"description,omitempty"`
}

type RunningHubRunRequest struct {
	AIAppID          string               `json:"-"`
	NodeInfoList     []RunningHubNodeInfo `json:"nodeInfoList,omitempty"`
	InstanceType     string               `json:"instanceType,omitempty"`
	UsePersonalQueue bool                 `json:"usePersonalQueue,omitempty"`
	RetainSeconds    int                  `json:"retainSeconds,omitempty"`
	WebhookURL       string               `json:"webhookUrl,omitempty"`
}

type RunningHubTask struct {
	TaskID       string             `json:"taskId"`
	Status       string             `json:"status"`
	ErrorCode    string             `json:"errorCode"`
	ErrorMessage string             `json:"errorMessage"`
	Results      []RunningHubResult `json:"results"`
	ClientID     string             `json:"clientId"`
	PromptTips   string             `json:"promptTips"`
}

type RunningHubResult struct {
	URL        string `json:"url"`
	NodeID     string `json:"nodeId"`
	OutputType string `json:"outputType"`
	Text       string `json:"text"`
}

type RunningHubQueueStatus struct {
	APIKeyType        string `json:"apiKeyType"`
	ConcurrentLimit   int    `json:"concurrentLimit"`
	RunningCount      int    `json:"runningCount"`
	QueuedCount       int    `json:"queuedCount"`
	TotalCurrentTasks int    `json:"totalCurrentTasks"`
}

type InfinitetalkRequest struct {
	ExistingTaskID  string
	ImagePath       string
	AudioPath       string
	OutputPath      string
	NodeInfoList    []RunningHubNodeInfo
	Prompt          string
	MaxSize         int
	Jitter          string
	Zoom            string
	InstanceType    string
	PollInterval    time.Duration
	Timeout         time.Duration
	DownloadResult  bool
	OnTaskSubmitted func(RunningHubTask)
	OnTaskPolled    func(RunningHubTask)
}

type InfinitetalkResult struct {
	Task       RunningHubTask
	OutputPath string
}

func (c RunningHubClient) GenerateInfinitetalkVideo(ctx context.Context, input InfinitetalkRequest) (InfinitetalkResult, error) {
	taskID := strings.TrimSpace(input.ExistingTaskID)
	if taskID == "" {
		task, err := c.SubmitInfinitetalkVideo(ctx, input)
		if err != nil {
			return InfinitetalkResult{}, err
		}
		taskID = task.TaskID
		if input.OnTaskSubmitted != nil {
			input.OnTaskSubmitted(task)
		}
	}

	pollInterval := input.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultRunningHubPollInterval
	}
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = DefaultRunningHubPollTimeout
	}
	finalTask, err := c.WaitTask(ctx, taskID, pollInterval, timeout, input.OnTaskPolled)
	if err != nil {
		return InfinitetalkResult{}, err
	}
	result := InfinitetalkResult{Task: finalTask}
	if input.DownloadResult && input.OutputPath != "" {
		videoURL := firstResultURL(finalTask.Results, "mp4")
		if videoURL == "" {
			videoURL = firstResultURL(finalTask.Results, "")
		}
		if videoURL == "" {
			return InfinitetalkResult{}, fmt.Errorf("runninghub task %s succeeded without downloadable result", finalTask.TaskID)
		}
		if err := downloadFile(ctx, c.HTTPClient, videoURL, input.OutputPath); err != nil {
			return InfinitetalkResult{}, err
		}
		result.OutputPath = input.OutputPath
	}
	return result, nil
}

func (c RunningHubClient) SubmitInfinitetalkVideo(ctx context.Context, input InfinitetalkRequest) (RunningHubTask, error) {
	if input.ImagePath == "" {
		return RunningHubTask{}, fmt.Errorf("image path is required")
	}
	if input.AudioPath == "" {
		return RunningHubTask{}, fmt.Errorf("audio path is required")
	}

	imageURL, err := c.UploadFile(ctx, input.ImagePath)
	if err != nil {
		return RunningHubTask{}, fmt.Errorf("upload image: %w", err)
	}
	audioURL, err := c.UploadFile(ctx, input.AudioPath)
	if err != nil {
		return RunningHubTask{}, fmt.Errorf("upload audio: %w", err)
	}

	nodeInfoList := input.NodeInfoList
	if len(nodeInfoList) == 0 {
		nodeInfoList = DefaultInfinitetalkNodeInfo(input)
	}
	nodeInfo := fillNodeInfo(nodeInfoList, imageURL, audioURL)
	return c.RunAIApp(ctx, RunningHubRunRequest{
		AIAppID:          DefaultInfinitetalkAIAppID,
		NodeInfoList:     nodeInfo,
		InstanceType:     input.InstanceType,
		UsePersonalQueue: false,
	})
}

func DefaultInfinitetalkNodeInfo(input InfinitetalkRequest) []RunningHubNodeInfo {
	prompt := input.Prompt
	if prompt == "" {
		prompt = "人物在接受采访，身体保持静止，只有嘴部在讲话"
	}
	maxSize := input.MaxSize
	if maxSize <= 0 {
		maxSize = 1280
	}
	jitter := input.Jitter
	if jitter == "" {
		jitter = "0"
	}
	zoom := input.Zoom
	if zoom == "" {
		zoom = "1.0000000000000002"
	}

	return []RunningHubNodeInfo{
		{NodeID: "284", FieldName: "image", FieldValue: "{{image_url}}", Description: "图片上传"},
		{NodeID: "312", FieldName: "value", FieldValue: fmt.Sprintf("%d", maxSize), Description: "最长边尺寸"},
		{NodeID: "125", FieldName: "audio", FieldValue: "{{audio_url}}", Description: "音频上传"},
		{NodeID: "314", FieldName: "prompt", FieldValue: prompt, Description: "提示词"},
		{NodeID: "369", FieldName: "value", FieldValue: jitter, Description: "抖动强度"},
		{NodeID: "370", FieldName: "value", FieldValue: zoom, Description: "缩放大小"},
	}
}

func (c RunningHubClient) UploadFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/openapi/v2/media/upload/binary", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	respBody, err := c.do(req)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 0 {
		return "", fmt.Errorf("runninghub upload failed: %s", parsed.Message)
	}
	if parsed.Data.DownloadURL == "" {
		return "", fmt.Errorf("runninghub upload response missing download_url")
	}
	return parsed.Data.DownloadURL, nil
}

func (c RunningHubClient) RunAIApp(ctx context.Context, input RunningHubRunRequest) (RunningHubTask, error) {
	if input.AIAppID == "" {
		input.AIAppID = DefaultInfinitetalkAIAppID
	}
	if input.InstanceType == "" {
		input.InstanceType = "default"
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return RunningHubTask{}, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/openapi/v2/run/ai-app/"+input.AIAppID, bytes.NewReader(payload))
	if err != nil {
		return RunningHubTask{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	respBody, err := c.do(req)
	if err != nil {
		return RunningHubTask{}, err
	}
	var task RunningHubTask
	if err := parseRunningHubTask(respBody, &task); err != nil {
		return RunningHubTask{}, err
	}
	if task.TaskID == "" {
		return RunningHubTask{}, fmt.Errorf("runninghub run response missing taskId: %s", string(respBody))
	}
	return task, nil
}

func (c RunningHubClient) QueryTask(ctx context.Context, taskID string) (RunningHubTask, error) {
	payload, err := json.Marshal(map[string]string{"taskId": taskID})
	if err != nil {
		return RunningHubTask{}, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/openapi/v2/query", bytes.NewReader(payload))
	if err != nil {
		return RunningHubTask{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	respBody, err := c.do(req)
	if err != nil {
		return RunningHubTask{}, err
	}
	var task RunningHubTask
	if err := parseRunningHubTask(respBody, &task); err != nil {
		return RunningHubTask{}, err
	}
	task.Status = normalizeRunningHubStatus(task.Status)
	return task, nil
}

func (c RunningHubClient) QueryQueueStatus(ctx context.Context) (RunningHubQueueStatus, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/openapi/v2/queue/status", nil)
	if err != nil {
		return RunningHubQueueStatus{}, err
	}
	respBody, err := c.do(req)
	if err != nil {
		return RunningHubQueueStatus{}, err
	}
	var parsed struct {
		Code          int    `json:"code"`
		Msg           string `json:"msg"`
		ErrorMessages any    `json:"errorMessages"`
		Data          struct {
			APIKeyType        string `json:"apiKeyType"`
			ConcurrentLimit   any    `json:"concurrentLimit"`
			RunningCount      any    `json:"runningCount"`
			QueuedCount       any    `json:"queuedCount"`
			TotalCurrentTasks any    `json:"totalCurrentTasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return RunningHubQueueStatus{}, err
	}
	if parsed.Code != 0 {
		return RunningHubQueueStatus{}, fmt.Errorf("runninghub queue status failed: %s", firstNonEmpty(parsed.Msg, string(respBody)))
	}
	return RunningHubQueueStatus{
		APIKeyType:        parsed.Data.APIKeyType,
		ConcurrentLimit:   intFromJSONValue(parsed.Data.ConcurrentLimit),
		RunningCount:      intFromJSONValue(parsed.Data.RunningCount),
		QueuedCount:       intFromJSONValue(parsed.Data.QueuedCount),
		TotalCurrentTasks: intFromJSONValue(parsed.Data.TotalCurrentTasks),
	}, nil
}

func (c RunningHubClient) WaitTask(ctx context.Context, taskID string, interval, timeout time.Duration, onPolled ...func(RunningHubTask)) (RunningHubTask, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		task, err := c.QueryTask(ctx, taskID)
		if err != nil {
			return RunningHubTask{}, err
		}
		for _, callback := range onPolled {
			if callback != nil {
				callback(task)
			}
		}
		switch normalizeRunningHubStatus(task.Status) {
		case RunningHubStatusSuccess:
			return task, nil
		case RunningHubStatusFailed:
			return RunningHubTask{}, fmt.Errorf("runninghub task %s failed: %s %s", taskID, task.ErrorCode, task.ErrorMessage)
		}

		select {
		case <-ctx.Done():
			return RunningHubTask{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func parseRunningHubTask(body []byte, task *RunningHubTask) error {
	if err := json.Unmarshal(body, task); err != nil {
		return err
	}
	if task.TaskID != "" || task.Status != "" || len(task.Results) > 0 {
		task.Status = normalizeRunningHubStatus(task.Status)
		return nil
	}
	var wrapped struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    RunningHubTask `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return err
	}
	if wrapped.Data.TaskID != "" || wrapped.Data.Status != "" || len(wrapped.Data.Results) > 0 {
		*task = wrapped.Data
		task.Status = normalizeRunningHubStatus(task.Status)
		return nil
	}
	return nil
}

func normalizeRunningHubStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "SUCCEEDED", "COMPLETED", "COMPLETE", "FINISHED", "FINISH", "DONE":
		return RunningHubStatusSuccess
	case "FAILED", "FAIL", "ERROR", "CANCELED", "CANCELLED":
		return RunningHubStatusFailed
	case "QUEUED", "PENDING", "WAITING":
		return RunningHubStatusQueued
	case "RUNNING", "PROCESSING", "EXECUTING":
		return RunningHubStatusRunning
	default:
		return strings.ToUpper(strings.TrimSpace(status))
	}
}

func (c RunningHubClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("runninghub api key is required")
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultRunningHubBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	return req, nil
}

func (c RunningHubClient) do(req *http.Request) ([]byte, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ProviderError{Provider: "runninghub", Status: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

func fillNodeInfo(items []RunningHubNodeInfo, imageURL, audioURL string) []RunningHubNodeInfo {
	out := make([]RunningHubNodeInfo, len(items))
	copy(out, items)
	for i := range out {
		if value, ok := out[i].FieldValue.(string); ok {
			value = strings.ReplaceAll(value, "{{image_url}}", imageURL)
			value = strings.ReplaceAll(value, "{{audio_url}}", audioURL)
			out[i].FieldValue = value
		}
	}
	return out
}

func firstResultURL(results []RunningHubResult, outputType string) string {
	for _, result := range results {
		if result.URL == "" {
			continue
		}
		if outputType == "" || strings.EqualFold(result.OutputType, outputType) {
			return result.URL
		}
	}
	return ""
}

func intFromJSONValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}
