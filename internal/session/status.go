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

// BlockedReason constants for why a session is blocked.
const (
	BlockedReasonAwaitingApproval = "awaiting_approval"
	BlockedReasonAwaitingInput    = "awaiting_input"
	BlockedReasonRateLimit        = "rate_limit"
)

// awaitingApprovalPatterns indicate the agent needs explicit human approval.
var awaitingApprovalPatterns = []string{
	// Japanese patterns
	"してください",
	"してもらう必要",
	"承認",
	"よろしいですか",
	"確認が必要",
	"判断が必要",
	"再起動してもらう",
	"いかがでしょうか",
	"ご確認",
	// English patterns
	"would you like to proceed",
	"shall i proceed",
	"should i proceed",
	"do you want me to",
	"would you like me to",
	"please confirm",
	"please review",
}

// awaitingInputPatterns indicate the agent needs user input or a choice.
var awaitingInputPatterns = []string{
	// Japanese patterns
	"待ち",
	"どうしますか",
	"どれから",
	"教えてください",
	"どう思いますか",
	"次は何をしましょう",
	"ご意見",
	// English patterns
	"waiting for",
	"need your",
	"blocked on",
	"let me know",
}

// rateLimitPatterns indicate a rate limit has been hit.
var rateLimitPatterns = []string{
	"you've hit your limit",
	"rate limit",
	"レート制限",
}

// blockedPatterns combines all patterns that indicate a blocked session.
var blockedPatterns []string

func init() {
	blockedPatterns = append(blockedPatterns, awaitingApprovalPatterns...)
	blockedPatterns = append(blockedPatterns, awaitingInputPatterns...)
	blockedPatterns = append(blockedPatterns, rateLimitPatterns...)
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

// DetectBlockedReason classifies why a blocked session is blocked.
// Returns one of BlockedReasonRateLimit, BlockedReasonAwaitingApproval,
// BlockedReasonAwaitingInput, or "" if the reason cannot be determined.
// Should only be called when DetectStatus returns StatusBlocked.
func DetectBlockedReason(lastMessage string) string {
	if containsAny(lastMessage, rateLimitPatterns) {
		return BlockedReasonRateLimit
	}
	if containsAny(lastMessage, awaitingApprovalPatterns) {
		return BlockedReasonAwaitingApproval
	}
	if containsAny(lastMessage, awaitingInputPatterns) {
		return BlockedReasonAwaitingInput
	}
	// Full-width question mark → awaiting_input
	if endsWithQuestion(lastMessage) {
		return BlockedReasonAwaitingInput
	}
	return ""
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
