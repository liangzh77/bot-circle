package ai_tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXiaomiMiMoGenerateSpeechWithVoiceSample(t *testing.T) {
	tmp := t.TempDir()
	samplePath := filepath.Join(tmp, "sample.wav")
	if err := os.WriteFile(samplePath, []byte("sample-audio"), 0644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(tmp, "out.wav")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != "test-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		var body xiaomiMiMoChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != DefaultXiaomiMiMoVoiceCloneModel {
			t.Fatalf("unexpected model: %s", body.Model)
		}
		if len(body.Messages) != 2 || body.Messages[0].Role != "user" || body.Messages[1].Role != "assistant" {
			t.Fatalf("unexpected messages: %#v", body.Messages)
		}
		if !strings.HasPrefix(body.Audio.Voice, "data:audio/wav;base64,") {
			t.Fatalf("voice sample was not encoded as wav data url: %s", body.Audio.Voice)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"audio": map[string]any{
							"data": base64.StdEncoding.EncodeToString([]byte("generated-audio")),
						},
					},
				},
			},
			"usage": map[string]any{"characters": 6},
		})
	}))
	defer server.Close()

	client := XiaomiMiMoClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.GenerateSpeech(context.Background(), XiaomiMiMoSpeechRequest{
		Text:            "你好",
		Instruction:     "用四川话",
		VoiceSamplePath: samplePath,
		OutputPath:      outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AudioBytes != int64(len("generated-audio")) {
		t.Fatalf("unexpected audio bytes: %d", result.AudioBytes)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated-audio" {
		t.Fatalf("unexpected output: %q", string(data))
	}
}
