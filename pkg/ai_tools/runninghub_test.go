package ai_tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunningHubInfinitetalkFlow(t *testing.T) {
	tmp := t.TempDir()
	imagePath := filepath.Join(tmp, "portrait.jpg")
	audioPath := filepath.Join(tmp, "speech.mp3")
	outputPath := filepath.Join(tmp, "out.mp4")
	if err := os.WriteFile(imagePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi/v2/media/upload/binary":
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"download_url":"https://files.example/uploaded"}}`))
		case "/openapi/v2/run/ai-app/" + DefaultInfinitetalkAIAppID:
			var req RunningHubRunRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if len(req.NodeInfoList) != 2 {
				t.Fatalf("expected node info list, got %+v", req.NodeInfoList)
			}
			_, _ = w.Write([]byte(`{"taskId":"task-1","status":"RUNNING"}`))
		case "/openapi/v2/query":
			queryCount++
			if queryCount == 1 {
				_, _ = w.Write([]byte(`{"taskId":"task-1","status":"RUNNING"}`))
				return
			}
			_, _ = w.Write([]byte(`{"taskId":"task-1","status":"SUCCESS","results":[{"url":"` + serverURL(r) + `/result.mp4","outputType":"mp4"}]}`))
		case "/result.mp4":
			_, _ = w.Write([]byte("video"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := RunningHubClient{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()}
	result, err := client.GenerateInfinitetalkVideo(context.Background(), InfinitetalkRequest{
		ImagePath:  imagePath,
		AudioPath:  audioPath,
		OutputPath: outputPath,
		NodeInfoList: []RunningHubNodeInfo{
			{NodeID: "image-node", FieldName: "image", FieldValue: "{{image_url}}"},
			{NodeID: "audio-node", FieldName: "audio", FieldValue: "{{audio_url}}"},
		},
		PollInterval:   10 * time.Millisecond,
		Timeout:        2 * time.Second,
		DownloadResult: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.Status != RunningHubStatusSuccess {
		t.Fatalf("unexpected status %s", result.Task.Status)
	}
	if data, err := os.ReadFile(outputPath); err != nil || string(data) != "video" {
		t.Fatalf("unexpected output %q err=%v", data, err)
	}
}

func TestDefaultInfinitetalkNodeInfoUses1280MaxSize(t *testing.T) {
	nodes := DefaultInfinitetalkNodeInfo(InfinitetalkRequest{})
	for _, node := range nodes {
		if node.NodeID == "312" && node.FieldName == "value" {
			if node.FieldValue != "1280" {
				t.Fatalf("expected default max size 1280, got %#v", node.FieldValue)
			}
			return
		}
	}
	t.Fatal("missing max size node 312")
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
