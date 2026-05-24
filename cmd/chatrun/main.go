package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"bot-circle/internal/config"
	"bot-circle/internal/robots"
	"bot-circle/internal/salon"
	"bot-circle/pkg/ai_tools"
)

func main() {
	configPath := flag.String("config", "app.config.json", "config file path")
	news := flag.String("news", "", "news topic to discuss")
	turns := flag.Int("turns", 100, "number of chat turns")
	start := flag.String("start", "", "start speaker name or id")
	output := flag.String("out", "sessions", "session output directory")
	skipMemory := flag.Bool("skip-memory", false, "skip episode and memory consolidation")
	robotSet := flag.String("set", "", "robot set name under robotSetsDir")
	resume := flag.String("resume", "", "existing session directory to continue")
	flag.Parse()

	if strings.TrimSpace(*news) == "" && strings.TrimSpace(*resume) == "" {
		fmt.Fprintln(os.Stderr, "-news is required")
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *robotSet != "" {
		cfg.RobotSet = *robotSet
		cfg.RobotsDir = cfg.RobotSetsDir + "/" + cfg.RobotSet
	}
	if cfg.SiliconFlow.APIKey == "" {
		fmt.Fprintln(os.Stderr, "siliconFlow.apiKey is required in app.config.json")
		os.Exit(1)
	}
	robotList, err := robots.LoadAll(cfg.RobotsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	client := ai_tools.SiliconFlowClient{APIKey: cfg.SiliconFlow.APIKey}
	runner := salon.NewRunner(cfg, client, robotList)
	runner.MaxTurns = *turns
	runner.StartedBy = *start
	runner.OutputDir = *output
	runner.SkipMemory = *skipMemory
	runner.ResumeDir = *resume

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*turns)*90*time.Second)
	defer cancel()

	session, err := runner.Run(ctx, *news)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("聊天完成：%s\n", session.Dir)
	fmt.Printf("聊天记录：%s/chat.md\n", session.Dir)
	if len(session.Turns) >= 2 {
		fmt.Printf("第二句提示词：%s/prompts/turn-002-%s.md\n", session.Dir, session.Turns[1].SpeakerID)
	}
}
