package ai_tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultXiaomiMiMoBaseURL         = "https://api.xiaomimimo.com/v1"
	DefaultXiaomiMiMoVoiceCloneModel = "mimo-v2.5-tts-voiceclone"
)

type XiaomiMiMoClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type XiaomiMiMoSpeechRequest struct {
	Text            string
	Instruction     string
	Voice           string
	VoiceSamplePath string
	OutputPath      string
	Model           string
	Format          string
}

type XiaomiMiMoSpeechResult struct {
	OutputPath string         `json:"outputPath"`
	Model      string         `json:"model"`
	Format     string         `json:"format"`
	AudioBytes int64          `json:"audioBytes"`
	Usage      map[string]any `json:"usage,omitempty"`
}

type xiaomiMiMoChatRequest struct {
	Model    string              `json:"model"`
	Messages []xiaomiMiMoMessage `json:"messages"`
	Audio    struct {
		Format string `json:"format"`
		Voice  string `json:"voice"`
	} `json:"audio"`
}

type xiaomiMiMoMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type xiaomiMiMoChatResponse struct {
	Choices []struct {
		Message struct {
			Audio struct {
				Data string `json:"data"`
			} `json:"audio"`
		} `json:"message"`
	} `json:"choices"`
	Usage map[string]any `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (c XiaomiMiMoClient) Capabilities() VoiceProviderCapabilities {
	return VoiceProviderCapabilities{
		ProviderID:      "xiaomi-mimo",
		DisplayName:     "小米 MiMo",
		CloneModes:      []string{"inline-audio-sample"},
		GenerationModes: []string{"non-streaming"},
		Parameters: []VoiceParameterSpec{
			{Key: "instruction", Label: "自然语言风格", Type: "text", Description: "例如：用四川话、语速稍快、情绪开心。"},
			{Key: "format", Label: "音频格式", Type: "select", Default: "wav", Options: []string{"wav", "pcm16"}},
		},
		Notes: []string{"MiMo VoiceClone 当前每次合成请求直接携带音频样本，不先生成长期 voice id。"},
	}
}

func (c XiaomiMiMoClient) GenerateSpeech(ctx context.Context, input XiaomiMiMoSpeechRequest) (XiaomiMiMoSpeechResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return XiaomiMiMoSpeechResult{}, fmt.Errorf("speech text is required")
	}
	if input.OutputPath == "" {
		return XiaomiMiMoSpeechResult{}, fmt.Errorf("speech output path is required")
	}
	model := firstNonEmpty(input.Model, DefaultXiaomiMiMoVoiceCloneModel)
	format := firstNonEmpty(input.Format, "wav")
	voice := input.Voice
	if voice == "" && input.VoiceSamplePath != "" {
		dataURL, err := audioDataURL(input.VoiceSamplePath)
		if err != nil {
			return XiaomiMiMoSpeechResult{}, err
		}
		voice = dataURL
	}
	if voice == "" {
		return XiaomiMiMoSpeechResult{}, fmt.Errorf("voice or voice sample path is required")
	}

	var body xiaomiMiMoChatRequest
	body.Model = model
	if strings.TrimSpace(input.Instruction) != "" {
		body.Messages = append(body.Messages, xiaomiMiMoMessage{Role: "user", Content: input.Instruction})
	}
	body.Messages = append(body.Messages, xiaomiMiMoMessage{Role: "assistant", Content: input.Text})
	body.Audio.Format = format
	body.Audio.Voice = voice

	payload, err := json.Marshal(body)
	if err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	defer resp.Body.Close()

	var parsed xiaomiMiMoChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText := ""
		if parsed.Error != nil {
			bodyText = firstNonEmpty(parsed.Error.Message, parsed.Error.Code)
		}
		return XiaomiMiMoSpeechResult{}, &ProviderError{Provider: "xiaomi-mimo", Status: resp.StatusCode, Body: bodyText}
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Audio.Data == "" {
		return XiaomiMiMoSpeechResult{}, fmt.Errorf("xiaomi mimo response did not include audio data")
	}
	audio, err := base64.StdEncoding.DecodeString(parsed.Choices[0].Message.Audio.Data)
	if err != nil {
		return XiaomiMiMoSpeechResult{}, fmt.Errorf("decode xiaomi mimo audio: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0755); err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	if err := os.WriteFile(input.OutputPath, audio, 0644); err != nil {
		return XiaomiMiMoSpeechResult{}, err
	}
	return XiaomiMiMoSpeechResult{
		OutputPath: input.OutputPath,
		Model:      model,
		Format:     format,
		AudioBytes: int64(len(audio)),
		Usage:      parsed.Usage,
	}, nil
}

func (c XiaomiMiMoClient) newRequest(ctx context.Context, method, path string, body *bytes.Reader) (*http.Request, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("xiaomi mimo api key is required")
	}
	baseURL := firstNonEmpty(c.BaseURL, DefaultXiaomiMiMoBaseURL)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api-key", c.APIKey)
	return req, nil
}
