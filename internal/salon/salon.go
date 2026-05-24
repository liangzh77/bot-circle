package salon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bot-circle/internal/config"
	"bot-circle/internal/robots"
	"bot-circle/pkg/ai_tools"
)

type TextCompleter interface {
	CompleteText(ctx context.Context, input ai_tools.TextCompletionRequest) (ai_tools.TextCompletionResult, error)
}

type Runner struct {
	Config     config.Config
	Client     TextCompleter
	Robots     []robots.Robot
	SetRules   string
	OutputDir  string
	MaxTurns   int
	StartedBy  string
	SkipMemory bool
	ResumeDir  string
}

type Turn struct {
	Index     int    `json:"index"`
	SpeakerID string `json:"speakerId"`
	Speaker   string `json:"speaker"`
	TargetID  string `json:"targetId"`
	Target    string `json:"target"`
	Text      string `json:"text"`
	Mood      string `json:"mood"`
}

type turnResponse struct {
	Speaker string `json:"speaker"`
	Target  string `json:"target"`
	Text    string `json:"text"`
	Mood    string `json:"mood"`
}

type Session struct {
	ID    string
	Dir   string
	News  string
	Turns []Turn
}

func NewRunner(cfg config.Config, client TextCompleter, robotList []robots.Robot) Runner {
	setRules, _ := os.ReadFile(filepath.Join(cfg.RobotsDir, "rules.md"))
	return Runner{
		Config:    cfg,
		Client:    client,
		Robots:    robotList,
		SetRules:  string(setRules),
		OutputDir: "sessions",
		MaxTurns:  100,
		StartedBy: "",
	}
}

func (r Runner) Run(ctx context.Context, news string) (Session, error) {
	if strings.TrimSpace(news) == "" && r.ResumeDir == "" {
		return Session{}, fmt.Errorf("news is required")
	}
	if r.MaxTurns <= 0 {
		r.MaxTurns = 100
	}
	if r.OutputDir == "" {
		r.OutputDir = "sessions"
	}
	if len(r.Robots) == 0 {
		return Session{}, fmt.Errorf("no robots loaded")
	}

	session, err := r.newOrLoadSession(news)
	if err != nil {
		return Session{}, err
	}
	if err := os.MkdirAll(filepath.Join(session.Dir, "prompts"), 0o755); err != nil {
		return Session{}, err
	}

	current, err := r.firstSpeaker()
	if err != nil {
		return Session{}, err
	}
	if len(session.Turns) > 0 {
		last := session.Turns[len(session.Turns)-1]
		next, ok := robots.Find(r.Robots, last.TargetID)
		if !ok {
			next, ok = robots.Find(r.Robots, last.Target)
		}
		if !ok {
			return Session{}, fmt.Errorf("cannot resume: unknown next speaker %q", last.Target)
		}
		current = next
	}
	startIndex := len(session.Turns) + 1
	endIndex := len(session.Turns) + r.MaxTurns
	for i := startIndex; i <= endIndex; i++ {
		prompt := r.turnPrompt(current, news, session.Turns)
		promptPath := filepath.Join(session.Dir, "prompts", fmt.Sprintf("turn-%03d-%s.md", i, current.ID))
		if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
			return Session{}, err
		}

		resp, err := r.completeTurn(ctx, current, prompt)
		if err != nil {
			return Session{}, fmt.Errorf("turn %d %s: %w", i, current.Name, err)
		}
		target, ok := robots.Find(r.Robots, resp.Target)
		if !ok || target.ID == current.ID {
			return Session{}, fmt.Errorf("turn %d %s chose invalid target %q", i, current.Name, resp.Target)
		}
		turn := Turn{
			Index:     i,
			SpeakerID: current.ID,
			Speaker:   current.Name,
			TargetID:  target.ID,
			Target:    target.Name,
			Text:      strings.TrimSpace(resp.Text),
			Mood:      strings.TrimSpace(resp.Mood),
		}
		session.Turns = append(session.Turns, turn)
		if err := r.writeChat(session); err != nil {
			return Session{}, err
		}
		current = target
	}

	if !r.SkipMemory {
		if err := r.consolidateMemories(ctx, session); err != nil {
			return Session{}, err
		}
	}
	if err := r.writeChat(session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (r Runner) newOrLoadSession(news string) (Session, error) {
	if r.ResumeDir == "" {
		id := time.Now().Format("20060102-150405")
		return Session{
			ID:   id,
			Dir:  filepath.Join(r.OutputDir, id),
			News: news,
		}, nil
	}
	session := Session{
		ID:   filepath.Base(filepath.Clean(r.ResumeDir)),
		Dir:  r.ResumeDir,
		News: news,
	}
	chatPath := filepath.Join(r.ResumeDir, "chat.md")
	if data, err := os.ReadFile(chatPath); err == nil && strings.TrimSpace(news) == "" {
		session.News = extractNewsFromChat(string(data))
	}
	turnsPath := filepath.Join(r.ResumeDir, "turns.json")
	data, err := os.ReadFile(turnsPath)
	if err != nil {
		return Session{}, fmt.Errorf("read resume turns: %w", err)
	}
	if err := json.Unmarshal(data, &session.Turns); err != nil {
		return Session{}, fmt.Errorf("parse resume turns: %w", err)
	}
	if strings.TrimSpace(session.News) == "" {
		return Session{}, fmt.Errorf("news is required when resume chat.md has no news")
	}
	return session, nil
}

func (r Runner) firstSpeaker() (robots.Robot, error) {
	if r.StartedBy != "" {
		if robot, ok := robots.Find(r.Robots, r.StartedBy); ok {
			return robot, nil
		}
		return robots.Robot{}, fmt.Errorf("unknown start speaker %q", r.StartedBy)
	}
	if robot, ok := robots.Find(r.Robots, "旗叔"); ok {
		return robot, nil
	}
	return r.Robots[0], nil
}

func (r Runner) completeTurn(ctx context.Context, robot robots.Robot, prompt string) (turnResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			prompt += "\n\n上一次输出不是合法 JSON。请只输出 JSON，不要解释。"
		}
		result, err := r.Client.CompleteText(ctx, ai_tools.TextCompletionRequest{
			Model:        r.Config.SiliconFlow.TextModel,
			SystemPrompt: "你是一个回合制群聊中的单个角色。你必须严格按 JSON 输出。",
			UserText:     prompt,
			Temperature:  0.75,
			MaxTokens:    800,
		})
		if err != nil {
			return turnResponse{}, err
		}
		resp, err := parseTurnResponse(result.Text)
		if err == nil {
			if strings.TrimSpace(resp.Speaker) != robot.Name {
				return turnResponse{}, fmt.Errorf("speaker mismatch: got %q want %q", resp.Speaker, robot.Name)
			}
			if strings.TrimSpace(resp.Text) == "" {
				return turnResponse{}, fmt.Errorf("empty text")
			}
			return resp, nil
		}
		lastErr = err
	}
	return turnResponse{}, lastErr
}

func (r Runner) turnPrompt(robot robots.Robot, news string, turns []Turn) string {
	return fmt.Sprintf(`你正在扮演机器人：%s。

你只能扮演 %s。
你不知道其他角色的完整人物设定，只能根据聊天记录理解他们。
不要写旁白，不要总结，不要替别人说话。
你必须回应当前对话，并指定下一句对谁说。

【你的角色设定】
%s

【你的长期记忆】
%s

【本次新闻】
%s

【本套角色的对话规则】
%s

【本次聊天记录】
%s

【可选择的下一位说话对象】
%s

【输出格式】
只输出 JSON，不要输出 Markdown，不要解释。

【写作硬规则】
1. 不要说空泛正确的话，例如“人命关天”“值得关注”“我们要记住”“不能忘记”。
2. 每句话只做一件事：接话、拆一点、补一个细节、问一句，或轻轻推进。
3. 不要重复上一句的意思。你必须补充新角度，或者轻轻反驳，或者把话题推给某个人。
4. 你的话要像真人临场说出来，不要像新闻评论、悼念稿、公众号或总结发言。
5. 如果话题沉重，可以克制，但不能变成套话。
6. 允许短句、停顿和不完整的口语。不要每句都写成金句。
7. 不要每句都有效。允许递话、接歪、半包袱和口头反应。
8. 不能编造新闻里没有的具体事实、数字、地点、动作。荒唐设定只能发生在评论和延伸里，不能改新闻事实。
9. 只说一句，不超过 60 个汉字。

{
  "speaker": "%s",
  "target": "下一位说话对象",
  "text": "一句自然的话，不超过80个汉字",
  "mood": ""
}
`, robot.Name, robot.Name, robot.Role, robot.Memory, news, r.SetRules, transcript(turns, len(r.Robots) > 2), strings.Join(robots.NamesExcept(r.Robots, robot.ID), "、"), robot.Name)
}

func (r Runner) consolidateMemories(ctx context.Context, session Session) error {
	transcriptText := transcript(session.Turns, len(r.Robots) > 2)
	for _, robot := range r.Robots {
		episodePrompt := fmt.Sprintf(`你正在为机器人 %s 沉淀本次聊天记忆。

请只站在 %s 的视角写。你不能写入它没有听到、没有依据知道的上帝视角真相。

【角色设定】
%s

【旧长期记忆】
%s

【本次新闻】
%s

【完整聊天记录】
%s

请输出 Markdown，格式：
# 记忆片段 %s

## 发生了什么

## 我注意到

## 我对其他人的感觉

## 我自己的变化

## 重要性

重要性用 1/5 到 5/5。`, robot.Name, robot.Name, robot.Role, robot.Memory, session.News, transcriptText, session.ID)

		result, err := r.Client.CompleteText(ctx, ai_tools.TextCompletionRequest{
			Model:        r.Config.SiliconFlow.TextModel,
			SystemPrompt: "你是角色记忆整理器。你只写该角色视角的主观记忆。",
			UserText:     episodePrompt,
			Temperature:  0.35,
			MaxTokens:    1200,
		})
		if err != nil {
			return fmt.Errorf("episode memory for %s: %w", robot.Name, err)
		}
		episodeDir := filepath.Join(robot.Dir, "episodes")
		if err := os.MkdirAll(episodeDir, 0o755); err != nil {
			return err
		}
		episodePath := filepath.Join(episodeDir, session.ID+".md")
		if err := os.WriteFile(episodePath, []byte(result.Text+"\n"), 0o644); err != nil {
			return err
		}

		memoryPrompt := fmt.Sprintf(`请更新机器人 %s 的长期记忆。

要求：
- 只保留 %s 自己能知道和会记住的内容。
- 不要无限增长，控制在 1000 到 2000 汉字以内。
- 保留稳定人格、近期重要关系变化、尚未解决的矛盾。
- 删除琐碎事实。
- 如果新旧记忆冲突，写成“我曾经以为……但最近开始怀疑……”。

【旧长期记忆】
%s

【新记忆片段】
%s

请输出新的 memory.md 完整内容，Markdown 格式。`, robot.Name, robot.Name, robot.Memory, result.Text)

		updated, err := r.Client.CompleteText(ctx, ai_tools.TextCompletionRequest{
			Model:        r.Config.SiliconFlow.TextModel,
			SystemPrompt: "你是长期记忆压缩器。输出可直接覆盖 memory.md 的 Markdown。",
			UserText:     memoryPrompt,
			Temperature:  0.25,
			MaxTokens:    1800,
		})
		if err != nil {
			return fmt.Errorf("long-term memory for %s: %w", robot.Name, err)
		}
		if err := os.WriteFile(robot.MemoryPath, []byte(updated.Text+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func parseTurnResponse(text string) (turnResponse, error) {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end < start {
		return turnResponse{}, fmt.Errorf("no JSON object in response: %s", text)
	}
	cleaned = cleaned[start : end+1]

	var resp turnResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return turnResponse{}, err
	}
	return resp, nil
}

func transcript(turns []Turn, showTarget bool) string {
	if len(turns) == 0 {
		return "暂无。请你作为第一位说话者，围绕新闻开启对话。"
	}
	var b strings.Builder
	for _, turn := range turns {
		if showTarget {
			fmt.Fprintf(&b, "%d. %s -> %s：%s", turn.Index, turn.Speaker, turn.Target, turn.Text)
		} else {
			fmt.Fprintf(&b, "%d. %s：%s", turn.Index, turn.Speaker, turn.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func extractNewsFromChat(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "## 新闻" {
			continue
		}
		for _, candidate := range lines[i+1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if strings.HasPrefix(candidate, "## ") {
				return ""
			}
			return candidate
		}
	}
	return ""
}

func (r Runner) writeChat(session Session) error {
	if err := os.MkdirAll(session.Dir, 0o755); err != nil {
		return err
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# 聊天记录 %s\n\n", session.ID)
	fmt.Fprintf(&md, "## 新闻\n\n%s\n\n", session.News)
	fmt.Fprintf(&md, "## 对话\n\n")
	md.WriteString(transcript(session.Turns, len(r.Robots) > 2))
	if err := os.WriteFile(filepath.Join(session.Dir, "chat.md"), []byte(md.String()), 0o644); err != nil {
		return err
	}
	jsonData, err := json.MarshalIndent(session.Turns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(session.Dir, "turns.json"), jsonData, 0o644)
}
