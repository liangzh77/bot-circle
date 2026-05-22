package ai_tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateFirstFrameBuildsThreeImageRequest(t *testing.T) {
	tmp := t.TempDir()
	imagePath := filepath.Join(tmp, "image.jpg")
	if err := os.WriteFile(imagePath, []byte("fake-image"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got imageGenerationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"images":[{"url":"http://example.test/out.png"}],"seed":7}`))
	}))
	defer server.Close()

	client := SiliconFlowClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.GenerateFirstFrame(context.Background(), FirstFrameRequest{
		TemplateImagePath:   imagePath,
		PersonImagePath:     imagePath,
		BackgroundImagePath: imagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != "http://example.test/out.png" || result.Seed != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got.Model != DefaultSiliconFlowImageEditModel {
		t.Fatalf("unexpected model %q", got.Model)
	}
	for _, value := range []string{got.Image, got.Image2, got.Image3} {
		if !strings.HasPrefix(value, "data:image/jpeg;base64,") {
			t.Fatalf("expected data url, got %q", value)
		}
	}
}

func TestGenerateFirstFrameAllowsTwoImageRequest(t *testing.T) {
	tmp := t.TempDir()
	imagePath := filepath.Join(tmp, "image.jpg")
	if err := os.WriteFile(imagePath, []byte("fake-image"), 0o644); err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"images":[{"url":"http://example.test/out.png"}],"seed":7}`))
	}))
	defer server.Close()

	client := SiliconFlowClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	if _, err := client.GenerateFirstFrame(context.Background(), FirstFrameRequest{
		TemplateImagePath: imagePath,
		PersonImagePath:   imagePath,
		Prompt:            "two images",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["image3"]; ok {
		t.Fatalf("image3 should be omitted for two-image request: %+v", raw)
	}
	if raw["image"] == "" || raw["image2"] == "" {
		t.Fatalf("expected image and image2 data urls: %+v", raw)
	}
}

func TestTranscribeAudioUsesMultipartForm(t *testing.T) {
	tmp := t.TempDir()
	audioPath := filepath.Join(tmp, "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != DefaultSiliconFlowTranscriptionModel {
			t.Fatalf("unexpected model %q", r.FormValue("model"))
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()

	client := SiliconFlowClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.TranscribeAudio(context.Background(), audioPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" {
		t.Fatalf("unexpected text %q", result.Text)
	}
}
