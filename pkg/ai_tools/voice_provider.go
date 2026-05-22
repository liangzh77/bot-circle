package ai_tools

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type VoiceProviderCapabilities struct {
	ProviderID       string               `json:"providerId"`
	DisplayName      string               `json:"displayName"`
	CloneModes       []string             `json:"cloneModes"`
	GenerationModes  []string             `json:"generationModes"`
	Parameters       []VoiceParameterSpec `json:"parameters"`
	Notes            []string             `json:"notes,omitempty"`
	RequiresCloneJob bool                 `json:"requiresCloneJob"`
}

type VoiceParameterSpec struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Default     string   `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
}

func audioDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	contentType := audioContentType(path)
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data)), nil
}

func audioContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			if semi := strings.Index(value, ";"); semi >= 0 {
				value = value[:semi]
			}
			return value
		}
	}
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a", ".mp4":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

func writeDownloadedFile(client *http.Client, url, outputPath string) (int64, error) {
	if strings.TrimSpace(url) == "" {
		return 0, fmt.Errorf("audio url is empty")
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &ProviderError{Provider: "download", Status: resp.StatusCode, Body: resp.Status}
	}
	return writeResponseBody(resp, outputPath)
}

func writeResponseBody(resp *http.Response, outputPath string) (int64, error) {
	if outputPath == "" {
		return 0, fmt.Errorf("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return 0, err
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, resp.Body)
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
