package ai_tools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const DefaultAliyunQwenRealtimeWSURL = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"

type AliyunQwenRealtimeSpeechRequest struct {
	Text         string
	Voice        string
	OutputPath   string
	Model        string
	LanguageType string
	SampleRate   int
}

type AliyunQwenRealtimeSpeechResult struct {
	OutputPath       string `json:"outputPath"`
	Model            string `json:"model"`
	Voice            string `json:"voice"`
	AudioBytes       int64  `json:"audioBytes"`
	FirstAudioMillis int64  `json:"firstAudioMillis"`
}

func (c AliyunBailianClient) GenerateQwenRealtimeSpeech(ctx context.Context, input AliyunQwenRealtimeSpeechRequest) (AliyunQwenRealtimeSpeechResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return AliyunQwenRealtimeSpeechResult{}, fmt.Errorf("speech text is required")
	}
	if input.OutputPath == "" {
		return AliyunQwenRealtimeSpeechResult{}, fmt.Errorf("speech output path is required")
	}
	if input.Voice == "" {
		return AliyunQwenRealtimeSpeechResult{}, fmt.Errorf("voice is required")
	}
	if c.APIKey == "" {
		return AliyunQwenRealtimeSpeechResult{}, fmt.Errorf("aliyun bailian api key is required")
	}
	model := firstNonEmpty(input.Model, DefaultAliyunQwenCloneTargetModel)
	sampleRate := input.SampleRate
	if sampleRate <= 0 {
		sampleRate = 24000
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.APIKey)
	url := DefaultAliyunQwenRealtimeWSURL + "?model=" + model
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return AliyunQwenRealtimeSpeechResult{}, err
	}
	defer conn.Close()

	startedAt := time.Now()
	var audio []byte
	var firstAudioMillis int64
	done := make(chan error, 1)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			var event map[string]any
			if err := json.Unmarshal(message, &event); err != nil {
				done <- err
				return
			}
			switch event["type"] {
			case "error":
				body, _ := json.Marshal(event["error"])
				done <- &ProviderError{Provider: "aliyun-bailian", Body: string(body)}
				return
			case "response.audio.delta":
				delta, _ := event["delta"].(string)
				if delta == "" {
					continue
				}
				chunk, err := base64.StdEncoding.DecodeString(delta)
				if err != nil {
					done <- err
					return
				}
				if firstAudioMillis == 0 {
					firstAudioMillis = time.Since(startedAt).Milliseconds()
				}
				audio = append(audio, chunk...)
			case "session.finished":
				done <- nil
				return
			}
		}
	}()

	if err := writeRealtimeEvent(conn, "session.update", map[string]any{
		"session": map[string]any{
			"mode":            "server_commit",
			"voice":           input.Voice,
			"language_type":   firstNonEmpty(input.LanguageType, "Chinese"),
			"response_format": "pcm",
			"sample_rate":     sampleRate,
		},
	}); err != nil {
		return AliyunQwenRealtimeSpeechResult{}, err
	}
	if err := writeRealtimeEvent(conn, "input_text_buffer.append", map[string]any{"text": input.Text}); err != nil {
		return AliyunQwenRealtimeSpeechResult{}, err
	}
	if err := writeRealtimeEvent(conn, "session.finish", nil); err != nil {
		return AliyunQwenRealtimeSpeechResult{}, err
	}

	select {
	case err := <-done:
		if err != nil {
			return AliyunQwenRealtimeSpeechResult{}, err
		}
	case <-ctx.Done():
		return AliyunQwenRealtimeSpeechResult{}, ctx.Err()
	}
	if len(audio) == 0 {
		return AliyunQwenRealtimeSpeechResult{}, fmt.Errorf("aliyun realtime response did not include audio")
	}
	if err := writePCM16WAV(input.OutputPath, audio, sampleRate); err != nil {
		return AliyunQwenRealtimeSpeechResult{}, err
	}
	return AliyunQwenRealtimeSpeechResult{
		OutputPath:       input.OutputPath,
		Model:            model,
		Voice:            input.Voice,
		AudioBytes:       int64(len(audio)),
		FirstAudioMillis: firstAudioMillis,
	}, nil
}

func writeRealtimeEvent(conn *websocket.Conn, eventType string, extra map[string]any) error {
	event := map[string]any{
		"type":     eventType,
		"event_id": fmt.Sprintf("event_%d", time.Now().UnixNano()),
	}
	for key, value := range extra {
		event[key] = value
	}
	return conn.WriteJSON(event)
}

func writePCM16WAV(path string, pcm []byte, sampleRate int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	dataLen := uint32(len(pcm))
	byteRate := uint32(sampleRate * 2)
	blockAlign := uint16(2)
	if _, err := out.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(out, binary.LittleEndian, uint32(36)+dataLen); err != nil {
		return err
	}
	if _, err := out.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, value := range []any{uint32(16), uint16(1), uint16(1), uint32(sampleRate), byteRate, blockAlign, uint16(16)} {
		if err := binary.Write(out, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	if _, err := out.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(out, binary.LittleEndian, dataLen); err != nil {
		return err
	}
	_, err = out.Write(pcm)
	return err
}
