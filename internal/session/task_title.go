package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GenerateTaskTitle generates a short Japanese title for a session by reading
// the first few user messages from the JSONL file and calling claude -p.
// Returns empty string if generation fails or times out.
func GenerateTaskTitle(filePath string) string {
	messages := firstUserMessages(filePath, 3)
	if len(messages) == 0 {
		return ""
	}

	input := strings.Join(messages, "\n\n")
	prompt := fmt.Sprintf(
		"以下はAIエージェントセッションの最初のメッセージです。このセッションが何をするタスクなのか、20文字以内の日本語タイトルで答えてください。タイトルのみ返してください。\n\n%s",
		input,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}

	title := strings.TrimSpace(out.String())
	runes := []rune(title)
	if len(runes) > 20 {
		title = string(runes[:20])
	}
	return title
}

// firstUserMessages reads a JSONL file and returns the first `limit` user messages
// that contain text content (not tool_result responses).
func firstUserMessages(filePath string, limit int) []string {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var messages []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		if len(messages) >= limit {
			break
		}
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type != "user" {
			continue
		}

		text := extractUserMessageText(&entry)
		if text == "" {
			continue
		}

		messages = append(messages, text)
	}

	return messages
}

// extractUserMessageText extracts text from a user message entry.
// Returns "" for tool_result messages (agent responses to tool calls).
func extractUserMessageText(entry *jsonlEntry) string {
	if entry.Message == nil {
		return ""
	}

	var msg messageContent
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}

	// Try as plain string first
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		return strings.TrimSpace(textContent)
	}

	// Try as array of content blocks
	var rawBlocks []json.RawMessage
	if err := json.Unmarshal(msg.Content, &rawBlocks); err != nil {
		return ""
	}

	var texts []string
	for _, rb := range rawBlocks {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(rb, &block); err != nil {
			continue
		}
		if block.Type == "tool_result" {
			return "" // skip tool_result messages entirely
		}
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}

	return strings.TrimSpace(strings.Join(texts, " "))
}
