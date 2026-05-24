package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	SiliconFlow  SiliconFlowConfig `json:"siliconFlow"`
	RobotsDir    string            `json:"robotsDir"`
	RobotSetsDir string            `json:"robotSetsDir"`
	RobotSet     string            `json:"robotSet"`
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
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.RobotsDir == "" {
		if cfg.RobotSetsDir == "" {
			cfg.RobotSetsDir = "data/robot_sets"
		}
		if cfg.RobotSet == "" {
			cfg.RobotSet = "news_salon"
		}
		cfg.RobotsDir = cfg.RobotSetsDir + "/" + cfg.RobotSet
	}
	if cfg.SiliconFlow.TextModel == "" {
		return Config{}, fmt.Errorf("siliconFlow.textModel is required")
	}
	return cfg, nil
}
