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
	DefaultVolcengineCloneTTSBaseURL    = "https://openspeech.bytedance.com"
	DefaultVolcengineCloneTTSResourceID = "seed-icl-2.0"
	DefaultVolcengineCloneTTSCluster    = "volcano_icl"
)

type VolcengineCloneTTSClient struct {
	BaseURL           string
	AppID             string
	AccessToken       string
	ResourceID        string
	Cluster           string
	HTTPClient        *http.Client
	DefaultSpeedRatio float64
}

type VolcengineCloneSpeechRequest struct {
	Text       string
	SpeakerID  string
	UID        string
	OutputPath string
	Encoding   string
	SpeedRatio float64
}

type VolcengineCloneSpeechResult struct {
	OutputPath string  `json:"outputPath"`
	SpeakerID  string  `json:"speakerId"`
	RequestID  string  `json:"requestId"`
	Encoding   string  `json:"encoding"`
	AudioBytes int64   `json:"audioBytes"`
	SpeedRatio float64 `json:"speedRatio"`
}

type volcengineCloneTTSResponse struct {
	Data     string `json:"data"`
	BaseResp struct {
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	} `json:"BaseResp"`
}

func (c VolcengineCloneTTSClient) Capabilities() VoiceProviderCapabilities {
	return VoiceProviderCapabilities{
		ProviderID:       "volcengine-voice-clone",
		DisplayName:      "火山引擎声音复刻",
		CloneModes:       []string{"preallocated-speaker-slot"},
		GenerationModes:  []string{"query"},
		RequiresCloneJob: true,
		Parameters: []VoiceParameterSpec{
			{Key: "speed_ratio", Label: "语速", Type: "number", Default: "1.0", Description: "火山复刻音色合成的 audio.speed_ratio。"},
			{Key: "encoding", Label: "音频格式", Type: "select", Default: "mp3", Options: []string{"mp3", "wav", "pcm"}},
		},
		Notes: []string{"需要先在火山侧准备 speaker_id 槽位并完成训练。"},
	}
}

func (c VolcengineCloneTTSClient) GenerateSpeech(ctx context.Context, input VolcengineCloneSpeechRequest) (VolcengineCloneSpeechResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return VolcengineCloneSpeechResult{}, fmt.Errorf("speech text is required")
	}
	if input.SpeakerID == "" {
		return VolcengineCloneSpeechResult{}, fmt.Errorf("speaker id is required")
	}
	if input.OutputPath == "" {
		return VolcengineCloneSpeechResult{}, fmt.Errorf("speech output path is required")
	}
	if c.AppID == "" || c.AccessToken == "" {
		return VolcengineCloneSpeechResult{}, fmt.Errorf("volcengine voice clone credentials are not configured")
	}
	reqID := "voiceclone_tts_" + randomRequestID()
	encoding := firstNonEmpty(input.Encoding, "mp3")
	speedRatio := input.SpeedRatio
	if speedRatio == 0 {
		speedRatio = c.DefaultSpeedRatio
	}
	if speedRatio == 0 {
		speedRatio = 1.0
	}
	body := map[string]any{
		"app": map[string]any{
			"appid":   c.AppID,
			"token":   c.AccessToken,
			"cluster": firstNonEmpty(c.Cluster, DefaultVolcengineCloneTTSCluster),
		},
		"user": map[string]any{
			"uid": firstNonEmpty(input.UID, "video-cloud-tool"),
		},
		"audio": map[string]any{
			"voice_type":  input.SpeakerID,
			"encoding":    encoding,
			"speed_ratio": speedRatio,
		},
		"request": map[string]any{
			"reqid":     reqID,
			"text":      input.Text,
			"operation": "query",
		},
	}
	var response volcengineCloneTTSResponse
	if err := c.doJSON(ctx, "/api/v1/tts", body, &response); err != nil {
		return VolcengineCloneSpeechResult{}, err
	}
	if response.BaseResp.StatusCode != 0 {
		return VolcengineCloneSpeechResult{}, fmt.Errorf("volcengine clone tts failed: %s", response.BaseResp.StatusMessage)
	}
	audioBytes, err := base64.StdEncoding.DecodeString(response.Data)
	if err != nil {
		return VolcengineCloneSpeechResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0755); err != nil {
		return VolcengineCloneSpeechResult{}, err
	}
	if err := os.WriteFile(input.OutputPath, audioBytes, 0644); err != nil {
		return VolcengineCloneSpeechResult{}, err
	}
	return VolcengineCloneSpeechResult{
		OutputPath: input.OutputPath,
		SpeakerID:  input.SpeakerID,
		RequestID:  reqID,
		Encoding:   encoding,
		AudioBytes: int64(len(audioBytes)),
		SpeedRatio: speedRatio,
	}, nil
}

func (c VolcengineCloneTTSClient) doJSON(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	baseURL := firstNonEmpty(c.BaseURL, DefaultVolcengineCloneTTSBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resourceID := firstNonEmpty(c.ResourceID, DefaultVolcengineCloneTTSResourceID)
	req.Header.Set("Authorization", "Bearer;"+c.AccessToken)
	req.Header.Set("Resource-Id", resourceID)
	req.Header.Set("X-Api-Resource-Id", resourceID)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Provider: "volcengine-voice-clone", Status: resp.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return err
	}
	return nil
}
