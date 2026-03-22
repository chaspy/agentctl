package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// jsonlEntry represents a single line from the .jsonl file.
type jsonlEntry struct {
	Type              string          `json:"type"`
	CWD               string          `json:"cwd"`
	SessionID         string          `json:"sessionId"`
	GitBranch         string          `json:"gitBranch"`
	Message           json.RawMessage `json:"message"`
	Error             string          `json:"error"`
	IsApiErrorMessage bool            `json:"isApiErrorMessage"`
}

// messageContent represents the message field within an entry.
type messageContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock represents a content block within assistant messages.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// EnrichSession reads the .jsonl file and fills in CWD, GitBranch, and LastMessage.
func EnrichSession(s *SessionInfo) error {
	f, err := os.Open(s.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var lastUserOrAssistant *jsonlEntry

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Track CWD and git branch from any entry that has them
		if entry.CWD != "" {
			s.CWD = entry.CWD
		}
		if entry.GitBranch != "" {
			s.GitBranch = entry.GitBranch
		}
		if entry.SessionID != "" {
			s.SessionID = entry.SessionID
		}

		// Track the last user or assistant message
		if entry.Type == "user" || entry.Type == "assistant" {
			e := entry
			lastUserOrAssistant = &e
		}
	}

	if lastUserOrAssistant != nil {
		s.LastRole = lastUserOrAssistant.Type
		s.LastMessage = extractMessageText(lastUserOrAssistant)
		s.LastFullMessage = extractFullMessageText(lastUserOrAssistant)
		s.ErrorType = lastUserOrAssistant.Error
		s.IsAPIError = lastUserOrAssistant.IsApiErrorMessage
	}

	return scanner.Err()
}

// extractMessageText extracts readable text from a message entry.
func extractMessageText(entry *jsonlEntry) string {
	if entry.Message == nil {
		return ""
	}

	var msg messageContent
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}

	// Content can be a string or an array of content blocks
	// Try as string first
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		return truncate(textContent, 60)
	}

	// Try as array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		if len(texts) > 0 {
			return truncate(strings.Join(texts, " "), 60)
		}
	}

	return ""
}

// LastAssistantMessage reads the .jsonl file and returns the full text of the last assistant message.
func LastAssistantMessage(s *SessionInfo) string {
	f, err := os.Open(s.FilePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lastAssistant *jsonlEntry

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type == "assistant" {
			e := entry
			lastAssistant = &e
		}
	}

	if lastAssistant == nil {
		return ""
	}
	return extractFullMessageText(lastAssistant)
}

// extractFullMessageText extracts readable text from a message entry without truncation.
func extractFullMessageText(entry *jsonlEntry) string {
	if entry.Message == nil {
		return ""
	}

	var msg messageContent
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}

	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		return strings.TrimSpace(textContent)
	}

	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		if len(texts) > 0 {
			return strings.TrimSpace(strings.Join(texts, "\n"))
		}
	}

	return ""
}

// usageEntry is used to extract usage data from assistant response entries.
type usageEntry struct {
	Type    string   `json:"type"`
	Message *usageMsg `json:"message"`
}

type usageMsg struct {
	Role  string     `json:"role"`
	Usage *usageData `json:"usage"`
}

type usageData struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// TokenUsage holds aggregated token usage for a session.
type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
	CacheCreation int64
	CacheRead     int64
}

// ScanTokenUsage reads a JSONL file and sums up all token usage from assistant messages.
func ScanTokenUsage(filePath string) (TokenUsage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return TokenUsage{}, err
	}
	defer f.Close()

	var usage TokenUsage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "output_tokens") {
			continue
		}

		var entry usageEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		u := entry.Message.Usage
		usage.InputTokens += u.InputTokens
		usage.OutputTokens += u.OutputTokens
		usage.CacheCreation += u.CacheCreationInputTokens
		usage.CacheRead += u.CacheReadInputTokens
	}

	return usage, scanner.Err()
}

// ScanAllTokenUsage scans all sessions modified within maxAge and returns total token usage.
func ScanAllTokenUsage(maxAge time.Duration) (TokenUsage, int, error) {
	sessions, err := ScanSessions(maxAge)
	if err != nil {
		return TokenUsage{}, 0, err
	}

	var total TokenUsage
	count := 0
	for _, s := range sessions {
		u, err := ScanTokenUsage(s.FilePath)
		if err != nil {
			continue
		}
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheCreation += u.CacheCreation
		total.CacheRead += u.CacheRead
		count++

		// Also scan subagent files
		subDir := strings.TrimSuffix(s.FilePath, ".jsonl") + "/subagents"
		subFiles, _ := filepath.Glob(filepath.Join(subDir, "*.jsonl"))
		for _, sf := range subFiles {
			su, err := ScanTokenUsage(sf)
			if err != nil {
				continue
			}
			total.InputTokens += su.InputTokens
			total.OutputTokens += su.OutputTokens
			total.CacheCreation += su.CacheCreation
			total.CacheRead += su.CacheRead
		}
	}

	return total, count, nil
}

// ChatMessage represents a single user or assistant message for display.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RecentMessages reads the JSONL file and returns the last `limit` user/assistant messages.
func RecentMessages(filePath string, limit int) ([]ChatMessage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []ChatMessage

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		text := extractFullMessageText(&entry)
		if text == "" {
			continue
		}

		// Truncate to 2000 runes for mobile display
		runes := []rune(text)
		if len(runes) > 2000 {
			text = string(runes[:2000]) + "..."
		}

		messages = append(messages, ChatMessage{
			Role:    entry.Type,
			Content: text,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Return only the last `limit` messages
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	return messages, nil
}

// truncate shortens a string to maxLen runes, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	// Replace newlines with spaces for display
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
