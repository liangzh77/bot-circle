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

func TestAliyunBailianCloneQwenVoice(t *testing.T) {
	tmp := t.TempDir()
	samplePath := filepath.Join(tmp, "sample.mp3")
	if err := os.WriteFile(samplePath, []byte("sample-audio"), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != aliyunBailianCustomizationPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != DefaultAliyunQwenCloneModel {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		input := body["input"].(map[string]any)
		if input["action"] != "create" {
			t.Fatalf("unexpected action: %#v", input["action"])
		}
		if input["preferred_name"] != "badname_123" {
			t.Fatalf("preferred name was not sanitized: %#v", input["preferred_name"])
		}
		if input["text"] != "测试文本。" {
			t.Fatalf("unexpected transcript: %#v", input["text"])
		}
		audio := input["audio"].(map[string]any)
		if !strings.HasPrefix(audio["data"].(string), "data:audio/mpeg;base64,") {
			t.Fatalf("audio was not encoded as mpeg data url: %s", audio["data"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{
				"voice":        "cloned_voice",
				"target_model": DefaultAliyunQwenCloneTargetModel,
			},
			"request_id": "req-1",
			"usage":      map[string]any{"count": 1},
		})
	}))
	defer server.Close()

	client := AliyunBailianClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.CloneQwenVoice(context.Background(), AliyunBailianCloneRequest{
		VoiceSamplePath: samplePath,
		PreferredName:   "bad-name!_123",
		Transcript:      "测试文本。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Voice != "cloned_voice" || result.RequestID != "req-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAliyunBailianCloneQwenVoiceOmitsEmptyTranscript(t *testing.T) {
	tmp := t.TempDir()
	samplePath := filepath.Join(tmp, "sample.mp3")
	if err := os.WriteFile(samplePath, []byte("sample-audio"), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		input := body["input"].(map[string]any)
		if _, exists := input["text"]; exists {
			t.Fatalf("empty transcript should be omitted, got %#v", input["text"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{
				"voice":        "cloned_voice_auto",
				"target_model": DefaultAliyunQwenCloneTargetModel,
			},
			"request_id": "req-auto",
		})
	}))
	defer server.Close()

	client := AliyunBailianClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.CloneQwenVoice(context.Background(), AliyunBailianCloneRequest{
		VoiceSamplePath: samplePath,
		PreferredName:   "auto",
		Transcript:      "   ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Voice != "cloned_voice_auto" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAliyunBailianGenerateQwenSpeechDownloadsAudioURL(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "out.wav")

	server := httptest.NewServer(nil)
	mux := http.NewServeMux()
	mux.HandleFunc(aliyunBailianMultimodalGeneration, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		input := body["input"].(map[string]any)
		if input["voice"] != "cloned_voice" || input["instructions"] != "用四川话" {
			t.Fatalf("unexpected input: %#v", input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"request_id":  "req-2",
			"output": map[string]any{
				"audio": map[string]any{
					"url": server.URL + "/audio.wav",
					"id":  "audio-1",
				},
			},
			"usage": map[string]any{"characters": 2},
		})
	})
	mux.HandleFunc("/audio.wav", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("generated-audio"))
	})
	server.Config.Handler = mux
	defer server.Close()

	client := AliyunBailianClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.GenerateQwenSpeech(context.Background(), AliyunBailianSpeechRequest{
		Text:                 "你好",
		Voice:                "cloned_voice",
		Instructions:         "用四川话",
		OptimizeInstructions: true,
		OutputPath:           outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AudioID != "audio-1" || result.RequestID != "req-2" {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated-audio" {
		t.Fatalf("unexpected output: %q", string(data))
	}
}

func TestAliyunBailianGenerateQwenSpeechWritesBase64Audio(t *testing.T) {
	tmp := t.TempDir()
	outputPath := filepath.Join(tmp, "out.wav")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"request_id":  "req-3",
			"output": map[string]any{
				"audio": map[string]any{
					"data": base64.StdEncoding.EncodeToString([]byte("generated-audio")),
				},
			},
		})
	}))
	defer server.Close()

	client := AliyunBailianClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.GenerateQwenSpeech(context.Background(), AliyunBailianSpeechRequest{
		Text:       "你好",
		Voice:      "cloned_voice",
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AudioBytes != int64(len("generated-audio")) {
		t.Fatalf("unexpected audio bytes: %d", result.AudioBytes)
	}
}
