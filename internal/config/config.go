package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	SiliconFlow SiliconFlowConfig `json:"siliconFlow"`
	RobotsDir   string            `json:"robotsDir"`
}

type SiliconFlowConfig struct {
	APIKey    string `json:"apiKey"`
	TextModel string `json:"textModel"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.RobotsDir == "" {
		cfg.RobotsDir = "data/robots"
	}
	if cfg.SiliconFlow.TextModel == "" {
		return Config{}, fmt.Errorf("siliconFlow.textModel is required")
	}
	return cfg, nil
}
