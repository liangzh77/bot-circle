package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"bot-circle/internal/config"
	"bot-circle/pkg/ai_tools"
)

func main() {
	configPath := flag.String("config", "app.config.json", "config file path")
	userText := flag.String("text", "把这段话改得更自然：这几个机器人今天吵得很厉害，但是关系更近了。", "text completion input")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	client := ai_tools.SiliconFlowClient{
		APIKey: cfg.SiliconFlow.APIKey,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := client.CompleteText(ctx, ai_tools.TextCompletionRequest{
		Model:        cfg.SiliconFlow.TextModel,
		SystemPrompt: "你是一个文案改写助手。",
		UserText:     *userText,
		Temperature:  0.2,
		MaxTokens:    1200,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(result.Text)
}
