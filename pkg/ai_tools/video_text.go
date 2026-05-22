package ai_tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type VideoTranscriptRequest struct {
	VideoPath       string
	TempAudioPath   string
	FFmpegPath      string
	TranscribeModel string
	KeepTempAudio   bool
}

type VideoTranscriptResult struct {
	Text      string
	AudioPath string
	TraceID   string
}

func ExtractVideoTranscript(ctx context.Context, siliconflow SiliconFlowClient, input VideoTranscriptRequest) (VideoTranscriptResult, error) {
	if input.VideoPath == "" {
		return VideoTranscriptResult{}, fmt.Errorf("video path is required")
	}
	audioPath := input.TempAudioPath
	if audioPath == "" {
		audioPath = filepath.Join(os.TempDir(), "video_cloud_transcript_"+filepath.Base(input.VideoPath)+".mp3")
	}
	if err := ExtractAudio(ctx, input.FFmpegPath, input.VideoPath, audioPath); err != nil {
		return VideoTranscriptResult{}, err
	}
	if !input.KeepTempAudio {
		defer os.Remove(audioPath)
	}
	transcript, err := siliconflow.TranscribeAudio(ctx, audioPath, input.TranscribeModel)
	if err != nil {
		return VideoTranscriptResult{}, err
	}
	return VideoTranscriptResult{Text: transcript.Text, AudioPath: audioPath, TraceID: transcript.TraceID}, nil
}

func ExtractAudio(ctx context.Context, ffmpegPath, videoPath, audioPath string) error {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-i", videoPath,
		"-vn",
		"-acodec", "libmp3lame",
		"-ar", "16000",
		"-ac", "1",
		audioPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract audio with ffmpeg: %w: %s", err, string(output))
	}
	return nil
}
