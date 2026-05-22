package ai_tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeychainUpsertExternalUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/runtime/channels/video_cloud/external-users/video-cloud-techprobe" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Fatalf("authorization header was not set")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "视频云技术验证" || body["isEnabled"] != true {
			t.Fatalf("unexpected body: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(RuntimeUser{
			ID:             "user_001",
			ChannelName:    "video_cloud",
			ExternalUserID: "video-cloud-techprobe",
			Name:           "视频云技术验证",
			IsEnabled:      true,
		})
	}))
	defer server.Close()

	client := KeychainClient{
		BaseURL:      server.URL,
		RuntimeToken: "runtime-token",
		HTTPClient:   server.Client(),
	}
	user, err := client.UpsertExternalUser(context.Background(), UpsertExternalUserRequest{
		ChannelName:    "video_cloud",
		ExternalUserID: "video-cloud-techprobe",
		Name:           "视频云技术验证",
		IsEnabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "user_001" {
		t.Fatalf("user.ID = %q", user.ID)
	}
}
