package robots

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Robot struct {
	ID         string
	Name       string
	Dir        string
	RolePath   string
	MemoryPath string
	Role       string
	Memory     string
}

func LoadAll(root string) ([]Robot, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var out []Robot
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		rolePath := filepath.Join(dir, "role.md")
		memoryPath := filepath.Join(dir, "memory.md")
		role, err := os.ReadFile(rolePath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rolePath, err)
		}
		memory, err := os.ReadFile(memoryPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", memoryPath, err)
		}
		out = append(out, Robot{
			ID:         entry.Name(),
			Name:       titleFromMarkdown(string(role), entry.Name()),
			Dir:        dir,
			RolePath:   rolePath,
			MemoryPath: memoryPath,
			Role:       string(role),
			Memory:     string(memory),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) == 0 {
		return nil, fmt.Errorf("no robot folders found in %s", root)
	}
	return out, nil
}

func IndexByID(list []Robot) map[string]Robot {
	out := make(map[string]Robot, len(list))
	for _, robot := range list {
		out[robot.ID] = robot
	}
	return out
}

func Find(list []Robot, value string) (Robot, bool) {
	value = strings.TrimSpace(value)
	for _, robot := range list {
		if strings.EqualFold(robot.ID, value) || robot.Name == value {
			return robot, true
		}
	}
	return Robot{}, false
}

func NamesExcept(list []Robot, speakerID string) []string {
	var names []string
	for _, robot := range list {
		if robot.ID != speakerID {
			names = append(names, robot.Name)
		}
	}
	return names
}

func titleFromMarkdown(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}
