package salon

import (
	"strings"
	"testing"
)

func TestParseTurnResponse(t *testing.T) {
	resp, err := parseTurnResponse("```json\n{\"speaker\":\"许真\",\"target\":\"灵灵\",\"text\":\"先看证据。\",\"mood\":\"克制\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Speaker != "许真" || resp.Target != "灵灵" || resp.Text != "先看证据。" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestTranscriptEmpty(t *testing.T) {
	got := transcript(nil, false)
	if !strings.Contains(got, "暂无") {
		t.Fatalf("expected empty transcript guidance, got %q", got)
	}
}
