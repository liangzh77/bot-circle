package ai_tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAliyunBailianBaseURL       = "https://dashscope.aliyuncs.com/api/v1"
	DefaultAliyunQwenCloneModel       = "qwen-voice-enrollment"
	DefaultAliyunQwenCloneTargetModel = "qwen3-tts-vc-realtime-2026-01-15"
	DefaultAliyunQwenSpeechModel      = "qwen3-tts-flash"
	DefaultAliyunQwenSystemVoice      = "Cherry"
	defaultAliyunVoicePreferredName   = "videocloud_voice"
	defaultAliyunVoiceCloneSampleText = "这是一段用于声音复刻的录音。"
	defaultAliyunVoiceCloneLanguage   = "zh"
	defaultAliyunQwenSpeechLanguage   = "Chinese"
	aliyunBailianCustomizationPath    = "/services/audio/tts/customization"
	aliyunBailianMultimodalGeneration = "/services/aigc/multimodal-generation/generation"
)

type AliyunBailianClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type AliyunBailianCloneRequest struct {
	VoiceSamplePath string
	PreferredName   string
	Transcript      string
	Language        string
	TargetModel     string
}

type AliyunBailianCloneResult struct {
	Voice       string         `json:"voice"`
	TargetModel string         `json:"targetModel"`
	RequestID   string         `json:"requestId"`
	Usage       map[string]any `json:"usage,omitempty"`
	RawOutput   map[string]any `json:"rawOutput,omitempty"`
}

type AliyunBailianSpeechRequest struct {
	Text                 string
	Voice                string
	OutputPath           string
	Model                string
	LanguageType         string
	Instructions         string
	OptimizeInstructions bool
}

type AliyunBailianSpeechResult struct {
	OutputPath string         `json:"outputPath"`
	Model      string         `json:"model"`
	Voice      string         `json:"voice"`
	AudioURL   string         `json:"audioUrl,omitempty"`
	AudioID    string         `json:"audioId,omitempty"`
	AudioBytes int64          `json:"audioBytes"`
	RequestID  string         `json:"requestId"`
	Usage      map[string]any `json:"usage,omitempty"`
}

type aliyunBailianCloneResponse struct {
	Output    map[string]any `json:"output"`
	Usage     map[string]any `json:"usage"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code"`
	Message   string         `json:"message"`
}

type aliyunBailianSpeechResponse struct {
	StatusCode int    `json:"status_code"`
	RequestID  string `json:"request_id"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Output     struct {
		Audio struct {
			Data      string `json:"data"`
			URL       string `json:"url"`
			ID        string `json:"id"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"audio"`
	} `json:"output"`
	Usage map[string]any `json:"usage"`
}

func (c AliyunBailianClient) Capabilities() VoiceProviderCapabilities {
	return VoiceProviderCapabilities{
		ProviderID:      "aliyun-bailian",
		DisplayName:     "阿里云百炼",
		CloneModes:      []string{"qwen-base64-audio", "cosyvoice-public-url"},
		GenerationModes: []string{"qwen-http-url", "cosyvoice-websocket"},
		Parameters: []VoiceParameterSpec{
			{Key: "language_type", Label: "语言", Type: "select", Default: "Chinese", Options: []string{"Auto", "Chinese", "English", "Japanese", "Korean", "French", "Russian"}},
			{Key: "instructions", Label: "指令", Type: "text", Description: "例如：用四川话表达，语速稍快，情绪更热情。"},
			{Key: "optimize_instructions", Label: "优化指令", Type: "boolean", Default: "false"},
		},
		Notes: []string{
			"Qwen 声音复刻可直接传 base64 音频。",
			"CosyVoice 声音复刻要求公网音频 URL，语音合成走 WebSocket，后续接入。",
		},
	}
}

func (c AliyunBailianClient) CloneQwenVoice(ctx context.Context, input AliyunBailianCloneRequest) (AliyunBailianCloneResult, error) {
	if input.VoiceSamplePath == "" {
		return AliyunBailianCloneResult{}, fmt.Errorf("voice sample path is required")
	}
	dataURL, err := audioDataURL(input.VoiceSamplePath)
	if err != nil {
		return AliyunBailianCloneResult{}, err
	}
	targetModel := firstNonEmpty(input.TargetModel, DefaultAliyunQwenCloneTargetModel)
	preferredName := sanitizeAliyunPreferredName(firstNonEmpty(input.PreferredName, defaultAliyunVoicePreferredName))
	requestInput := map[string]any{
		"action":         "create",
		"target_model":   targetModel,
		"preferred_name": preferredName,
		"audio": map[string]any{
			"data": dataURL,
		},
		"language": firstNonEmpty(input.Language, defaultAliyunVoiceCloneLanguage),
	}
	if transcript := strings.TrimSpace(input.Transcript); transcript != "" {
		requestInput["text"] = transcript
	}
	body := map[string]any{
		"model": DefaultAliyunQwenCloneModel,
		"input": requestInput,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return AliyunBailianCloneResult{}, err
	}
	req, err := c.newJSONRequest(ctx, aliyunBailianCustomizationPath, payload)
	if err != nil {
		return AliyunBailianCloneResult{}, err
	}
	var parsed aliyunBailianCloneResponse
	if err := c.doJSON(req, &parsed); err != nil {
		return AliyunBailianCloneResult{}, err
	}
	if parsed.Code != "" || parsed.Message != "" {
		return AliyunBailianCloneResult{}, &ProviderError{Provider: "aliyun-bailian", Status: 400, TraceID: parsed.RequestID, Body: firstNonEmpty(parsed.Message, parsed.Code)}
	}
	voice, _ := parsed.Output["voice"].(string)
	if voice == "" {
		voice, _ = parsed.Output["voice_id"].(string)
	}
	if voice == "" {
		return AliyunBailianCloneResult{}, fmt.Errorf("aliyun bailian clone response missing voice")
	}
	if returnedTarget, _ := parsed.Output["target_model"].(string); returnedTarget != "" {
		targetModel = returnedTarget
	}
	return AliyunBailianCloneResult{
		Voice:       voice,
		TargetModel: targetModel,
		RequestID:   parsed.RequestID,
		Usage:       parsed.Usage,
		RawOutput:   parsed.Output,
	}, nil
}

func (c AliyunBailianClient) GenerateQwenSpeech(ctx context.Context, input AliyunBailianSpeechRequest) (AliyunBailianSpeechResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return AliyunBailianSpeechResult{}, fmt.Errorf("speech text is required")
	}
	if input.OutputPath == "" {
		return AliyunBailianSpeechResult{}, fmt.Errorf("speech output path is required")
	}
	voice := firstNonEmpty(input.Voice, DefaultAliyunQwenSystemVoice)
	model := firstNonEmpty(input.Model, DefaultAliyunQwenSpeechModel)
	requestInput := map[string]any{
		"text":          input.Text,
		"voice":         voice,
		"language_type": firstNonEmpty(input.LanguageType, defaultAliyunQwenSpeechLanguage),
	}
	if input.Instructions != "" {
		requestInput["instructions"] = input.Instructions
	}
	if input.OptimizeInstructions {
		requestInput["optimize_instructions"] = true
	}
	body := map[string]any{
		"model": model,
		"input": requestInput,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return AliyunBailianSpeechResult{}, err
	}
	req, err := c.newJSONRequest(ctx, aliyunBailianMultimodalGeneration, payload)
	if err != nil {
		return AliyunBailianSpeechResult{}, err
	}
	var parsed aliyunBailianSpeechResponse
	if err := c.doJSON(req, &parsed); err != nil {
		return AliyunBailianSpeechResult{}, err
	}
	if parsed.Code != "" || parsed.Message != "" || parsed.StatusCode >= 400 {
		return AliyunBailianSpeechResult{}, &ProviderError{Provider: "aliyun-bailian", Status: parsed.StatusCode, TraceID: parsed.RequestID, Body: firstNonEmpty(parsed.Message, parsed.Code)}
	}
	audioBytes := int64(0)
	if parsed.Output.Audio.Data != "" {
		audio, err := base64.StdEncoding.DecodeString(parsed.Output.Audio.Data)
		if err != nil {
			return AliyunBailianSpeechResult{}, fmt.Errorf("decode aliyun bailian audio: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0755); err != nil {
			return AliyunBailianSpeechResult{}, err
		}
		if err := os.WriteFile(input.OutputPath, audio, 0644); err != nil {
			return AliyunBailianSpeechResult{}, err
		}
		audioBytes = int64(len(audio))
	} else if parsed.Output.Audio.URL != "" {
		written, err := writeDownloadedFile(c.HTTPClient, parsed.Output.Audio.URL, input.OutputPath)
		if err != nil {
			return AliyunBailianSpeechResult{}, err
		}
		audioBytes = written
	} else {
		return AliyunBailianSpeechResult{}, fmt.Errorf("aliyun bailian response did not include audio data or url")
	}
	return AliyunBailianSpeechResult{
		OutputPath: input.OutputPath,
		Model:      model,
		Voice:      voice,
		AudioURL:   parsed.Output.Audio.URL,
		AudioID:    parsed.Output.Audio.ID,
		AudioBytes: audioBytes,
		RequestID:  parsed.RequestID,
		Usage:      parsed.Usage,
	}, nil
}

func (c AliyunBailianClient) newJSONRequest(ctx context.Context, path string, payload []byte) (*http.Request, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("aliyun bailian api key is required")
	}
	baseURL := firstNonEmpty(c.BaseURL, DefaultAliyunBailianBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c AliyunBailianClient) doJSON(req *http.Request, target any) error {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Provider: "aliyun-bailian", Status: resp.StatusCode, Body: string(body)}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	return nil
}

func sanitizeAliyunPreferredName(value string) string {
	value = strings.TrimSpace(value)
	var out []rune
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return defaultAliyunVoicePreferredName
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return string(out)
}
