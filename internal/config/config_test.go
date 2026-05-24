package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAcceptsUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.config.json")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{
  "siliconFlow": {
    "apiKey": "test",
    "textModel": "deepseek-ai/DeepSeek-V3.2"
  },
  "robotSetsDir": "data/robot_sets",
  "robotSet": "deyun"
}`)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RobotsDir != "data/robot_sets/deyun" {
		t.Fatalf("unexpected robots dir: %s", cfg.RobotsDir)
	}
}
