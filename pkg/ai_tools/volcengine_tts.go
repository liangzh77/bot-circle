package ai_tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
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
	DefaultVolcengineTTSBaseURL    = "https://openspeech.bytedance.com"
	DefaultVolcengineTTSResourceID = "seed-tts-2.0"
	DefaultVolcengineTTSSpeaker    = "zh_female_vv_uranus_bigtts"
)

type VolcengineTTSClient struct {
	BaseURL    string
	APIKey     string
	AppID      string
	AccessKey  string
	ResourceID string
	HTTPClient *http.Client
}

type SpeechSynthesisRequest struct {
	Text       string
	Speaker    string
	OutputPath string
	UID        string
	Format     string
	SampleRate int
}

type SpeechSynthesisResult struct {
	OutputPath string
	Format     string
	SampleRate int
	LogID      string
	AudioBytes int64
	Usage      map[string]any
}

type volcengineTTSRequest struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	ReqParams struct {
		Text        string `json:"text"`
		Speaker     string `json:"speaker"`
		AudioParams struct {
			Format     string `json:"format"`
			SampleRate int    `json:"sample_rate"`
		} `json:"audio_params"`
	} `json:"req_params"`
}

type volcengineTTSChunk struct {
	Code     int            `json:"code"`
	Message  string         `json:"message"`
	Data     string         `json:"data"`
	Usage    map[string]any `json:"usage"`
	Sentence any            `json:"sentence"`
}

func (c VolcengineTTSClient) GenerateSpeech(ctx context.Context, input SpeechSynthesisRequest) (SpeechSynthesisResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return SpeechSynthesisResult{}, fmt.Errorf("speech text is required")
	}
	if input.OutputPath == "" {
		return SpeechSynthesisResult{}, fmt.Errorf("speech output path is required")
	}
	if input.Speaker == "" {
		input.Speaker = DefaultVolcengineTTSSpeaker
	}
	if input.UID == "" {
		input.UID = "video-cloud-techprobe"
	}
	if input.Format == "" {
		input.Format = "mp3"
	}
	if input.SampleRate <= 0 {
		input.SampleRate = 24000
	}

	var body volcengineTTSRequest
	body.User.UID = input.UID
	body.ReqParams.Text = input.Text
	body.ReqParams.Speaker = input.Speaker
	body.ReqParams.AudioParams.Format = input.Format
	body.ReqParams.AudioParams.SampleRate = input.SampleRate

	payload, err := json.Marshal(body)
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v3/tts/unidirectional", bytes.NewReader(payload))
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Request-Id", randomRequestID())

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	defer resp.Body.Close()

	logID := resp.Header.Get("X-Tt-Logid")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return SpeechSynthesisResult{}, readErr
		}
		return SpeechSynthesisResult{}, &ProviderError{Provider: "volcengine", Status: resp.StatusCode, TraceID: logID, Body: string(responseBody)}
	}

	if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0755); err != nil {
		return SpeechSynthesisResult{}, err
	}
	out, err := os.Create(input.OutputPath)
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	defer out.Close()

	var audioBytes int64
	var usage map[string]any
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if strings.HasPrefix(line, "event:") {
			continue
		}

		var chunk volcengineTTSChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return SpeechSynthesisResult{}, fmt.Errorf("parse volcengine tts chunk: %w", err)
		}
		if chunk.Code != 0 && chunk.Code != 20000000 {
			return SpeechSynthesisResult{}, &ProviderError{Provider: "volcengine", Status: chunk.Code, TraceID: logID, Body: chunk.Message}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Data == "" {
			continue
		}
		audio, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			return SpeechSynthesisResult{}, fmt.Errorf("decode volcengine tts audio chunk: %w", err)
		}
		written, err := out.Write(audio)
		if err != nil {
			return SpeechSynthesisResult{}, err
		}
		audioBytes += int64(written)
	}
	if err := scanner.Err(); err != nil {
		return SpeechSynthesisResult{}, err
	}
	if audioBytes == 0 {
		return SpeechSynthesisResult{}, fmt.Errorf("volcengine tts response did not include audio data")
	}

	return SpeechSynthesisResult{
		OutputPath: input.OutputPath,
		Format:     input.Format,
		SampleRate: input.SampleRate,
		LogID:      logID,
		AudioBytes: audioBytes,
		Usage:      usage,
	}, nil
}

func (c VolcengineTTSClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	c = c.withParsedCredential()
	if c.APIKey == "" && (c.AppID == "" || c.AccessKey == "") {
		return nil, fmt.Errorf("volcengine api key or app id/access key is required")
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultVolcengineTTSBaseURL
	}
	resourceID := c.ResourceID
	if resourceID == "" {
		resourceID = DefaultVolcengineTTSResourceID
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("X-Api-Key", c.APIKey)
	} else {
		req.Header.Set("X-Api-App-Id", c.AppID)
		req.Header.Set("X-Api-Access-Key", c.AccessKey)
	}
	req.Header.Set("X-Api-Resource-Id", resourceID)
	return req, nil
}

func (c VolcengineTTSClient) withParsedCredential() VolcengineTTSClient {
	raw := strings.TrimSpace(c.APIKey)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return c
	}
	var parsed struct {
		APIKey    string `json:"apiKey"`
		AppID     string `json:"appId"`
		AccessKey string `json:"accessKey"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return c
	}
	c.APIKey = parsed.APIKey
	c.AppID = parsed.AppID
	c.AccessKey = parsed.AccessKey
	return c
}

func randomRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "video-cloud-techprobe"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
