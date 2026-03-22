package session

import "strings"

// Status constants for session state.
const (
	StatusBlocked = "blocked"
	StatusError   = "error"
	StatusIdle    = "idle"
	StatusActive  = "active"
	StatusDead    = "dead"
)

// blockedPatterns are Japanese/English phrases that indicate the session is
// waiting for human action.
var blockedPatterns = []string{
	// Japanese patterns
	"してください",
	"してもらう必要",
	"待ち",
	"承認",
	"どうしますか",
	"どれから",
	"よろしいですか",
	"教えてください",
	"確認が必要",
	"判断が必要",
	"再起動してもらう",
	"どう思いますか",
	"次は何をしましょう",
	"いかがでしょうか",
	"ご意見",
	"ご確認",
	// English patterns
	"waiting for",
	"need your",
	"blocked on",
	"would you like to proceed",
	"shall i proceed",
	"should i proceed",
	"do you want me to",
	"would you like me to",
	"please confirm",
	"please review",
	"let me know",
}

// DetectStatus determines the session status from its last message, last role,
// whether the process is alive, and structured error metadata from JSONL.
//
// Priority:
//  1. JSONL error metadata (errorType + isAPIError) → "error"
//  2. Assistant message with blocked patterns → "blocked"
//  3. Process dead → "dead"
//  4. Process alive + last role is user → "active" (generating response)
//  5. Otherwise → "idle"
func DetectStatus(lastMessage, lastRole string, alive bool, errorType string, isAPIError bool) string {
	// Check structured error metadata from JSONL entry.
	// This is reliable because it uses the API's own error classification
	// rather than text pattern matching.
	if isAPIError && errorType != "" {
		return StatusError
	}

	if lastRole == "assistant" && lastMessage != "" {
		if containsAny(lastMessage, blockedPatterns) {
			return StatusBlocked
		}
		// Messages ending with "？" (full-width question mark) indicate a question to the user
		if endsWithQuestion(lastMessage) {
			return StatusBlocked
		}
	}

	if !alive {
		return StatusDead
	}

	if lastRole == "user" {
		return StatusActive
	}

	return StatusIdle
}

// endsWithQuestion checks if the message ends with a full-width question mark (？).
// This catches assistant messages that ask the user a question in Japanese.
func endsWithQuestion(s string) bool {
	trimmed := strings.TrimRight(s, " \t\n\r")
	return strings.HasSuffix(trimmed, "？")
}

func containsAny(s string, patterns []string) bool {
	lower := strings.ToLower(s)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
