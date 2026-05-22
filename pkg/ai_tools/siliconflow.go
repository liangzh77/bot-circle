package ai_tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultSiliconFlowBaseURL            = "https://api.siliconflow.cn"
	DefaultSiliconFlowImageEditModel     = "Qwen/Qwen-Image-Edit-2509"
	DefaultSiliconFlowTranscriptionModel = "FunAudioLLM/SenseVoiceSmall"
	DefaultSiliconFlowTextModel          = "deepseek-ai/DeepSeek-V3.2"
)

type SiliconFlowClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type FirstFrameRequest struct {
	TemplateImagePath   string
	PersonImagePath     string
	BackgroundImagePath string
	Prompt              string
	Model               string
	OutputPath          string
}

type FirstFrameResult struct {
	URL        string
	OutputPath string
	Seed       int64
	TraceID    string
}

type imageGenerationRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Image  string `json:"image"`
	Image2 string `json:"image2,omitempty"`
	Image3 string `json:"image3,omitempty"`
}

type imageGenerationResponse struct {
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	Seed int64 `json:"seed"`
}

func (c SiliconFlowClient) GenerateFirstFrame(ctx context.Context, input FirstFrameRequest) (FirstFrameResult, error) {
	if input.Prompt == "" {
		input.Prompt = "用图2的人物代替图1的人物，用图3的背景代替图1的背景"
	}
	if input.Model == "" {
		input.Model = DefaultSiliconFlowImageEditModel
	}

	templateImage, err := DataURL(input.TemplateImagePath)
	if err != nil {
		return FirstFrameResult{}, err
	}
	personImage, err := DataURL(input.PersonImagePath)
	if err != nil {
		return FirstFrameResult{}, err
	}
	body := imageGenerationRequest{
		Model:  input.Model,
		Prompt: input.Prompt,
		Image:  templateImage,
		Image2: personImage,
	}
	if input.BackgroundImagePath != "" {
		backgroundImage, err := DataURL(input.BackgroundImagePath)
		if err != nil {
			return FirstFrameResult{}, err
		}
		body.Image3 = backgroundImage
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return FirstFrameResult{}, err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/images/generations", bytes.NewReader(payload))
	if err != nil {
		return FirstFrameResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return FirstFrameResult{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return FirstFrameResult{}, err
	}
	traceID := resp.Header.Get("x-siliconcloud-trace-id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FirstFrameResult{}, &ProviderError{Provider: "siliconflow", Status: resp.StatusCode, TraceID: traceID, Body: string(responseBody)}
	}

	var parsed imageGenerationResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return FirstFrameResult{}, err
	}
	if len(parsed.Images) == 0 || parsed.Images[0].URL == "" {
		return FirstFrameResult{}, fmt.Errorf("siliconflow image response did not include an image url")
	}

	result := FirstFrameResult{URL: parsed.Images[0].URL, Seed: parsed.Seed, TraceID: traceID}
	if input.OutputPath != "" {
		if err := downloadFile(ctx, c.HTTPClient, parsed.Images[0].URL, input.OutputPath); err != nil {
			return FirstFrameResult{}, err
		}
		result.OutputPath = input.OutputPath
	}
	return result, nil
}

type TranscriptionResult struct {
	Text    string
	TraceID string
}

type TextCompletionRequest struct {
	Model        string
	SystemPrompt string
	UserText     string
	Temperature  float64
	MaxTokens    int
}

type TextCompletionResult struct {
	Text    string
	TraceID string
}

type chatCompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []chatCompletionMessage `json:"messages"`
	Temperature float64                 `json:"temperature,omitempty"`
	MaxTokens   int                     `json:"max_tokens,omitempty"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatCompletionMessage `json:"message"`
	} `json:"choices"`
}

func (c SiliconFlowClient) CompleteText(ctx context.Context, input TextCompletionRequest) (TextCompletionResult, error) {
	model := input.Model
	if model == "" {
		model = DefaultSiliconFlowTextModel
	}
	temperature := input.Temperature
	if temperature == 0 {
		temperature = 0.2
	}
	body := chatCompletionRequest{
		Model: model,
		Messages: []chatCompletionMessage{
			{Role: "system", Content: input.SystemPrompt},
			{Role: "user", Content: input.UserText},
		},
		Temperature: temperature,
		MaxTokens:   input.MaxTokens,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return TextCompletionResult{}, err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return TextCompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return TextCompletionResult{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TextCompletionResult{}, err
	}
	traceID := resp.Header.Get("x-siliconcloud-trace-id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TextCompletionResult{}, &ProviderError{Provider: "siliconflow", Status: resp.StatusCode, TraceID: traceID, Body: string(responseBody)}
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return TextCompletionResult{}, err
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return TextCompletionResult{}, fmt.Errorf("siliconflow text response did not include content")
	}
	return TextCompletionResult{Text: strings.TrimSpace(parsed.Choices[0].Message.Content), TraceID: traceID}, nil
}

func (c SiliconFlowClient) TranscribeAudio(ctx context.Context, audioPath, model string) (TranscriptionResult, error) {
	if model == "" {
		model = DefaultSiliconFlowTranscriptionModel
	}

	file, err := os.Open(audioPath)
	if err != nil {
		return TranscriptionResult{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return TranscriptionResult{}, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return TranscriptionResult{}, err
	}
	if err := writer.WriteField("model", model); err != nil {
		return TranscriptionResult{}, err
	}
	if err := writer.Close(); err != nil {
		return TranscriptionResult{}, err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/audio/transcriptions", &body)
	if err != nil {
		return TranscriptionResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return TranscriptionResult{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TranscriptionResult{}, err
	}
	traceID := resp.Header.Get("x-siliconcloud-trace-id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TranscriptionResult{}, &ProviderError{Provider: "siliconflow", Status: resp.StatusCode, TraceID: traceID, Body: string(responseBody)}
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return TranscriptionResult{}, err
	}
	return TranscriptionResult{Text: parsed.Text, TraceID: traceID}, nil
}

func (c SiliconFlowClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("siliconflow api key is required")
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultSiliconFlowBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	return req, nil
}

func (c SiliconFlowClient) do(req *http.Request) (*http.Response, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

func DataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mimeType := mimeTypeForPath(path)
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func mimeTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func downloadFile(ctx context.Context, client *http.Client, url, outputPath string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return &ProviderError{Provider: "download", Status: resp.StatusCode, Body: string(body)}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}
