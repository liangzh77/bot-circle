package ai_tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVolcengineCloneTTSGenerateSpeech(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "out.mp3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tts" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer;access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("Resource-Id"); got != DefaultVolcengineCloneTTSResourceID {
			t.Fatalf("unexpected resource id: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		audio := body["audio"].(map[string]any)
		if audio["voice_type"] != "speaker-1" || audio["encoding"] != "mp3" {
			t.Fatalf("unexpected audio params: %#v", audio)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": base64.StdEncoding.EncodeToString([]byte("generated-audio")),
			"BaseResp": map[string]any{
				"StatusCode":    0,
				"StatusMessage": "OK",
			},
		})
	}))
	defer server.Close()

	result, err := (VolcengineCloneTTSClient{
		BaseURL:     server.URL,
		AppID:       "app-id",
		AccessToken: "access-token",
		HTTPClient:  server.Client(),
	}).GenerateSpeech(context.Background(), VolcengineCloneSpeechRequest{
		Text:       "你好",
		SpeakerID:  "speaker-1",
		OutputPath: outputPath,
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
