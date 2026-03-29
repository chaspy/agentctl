package session

import "testing"

func TestDetectBlockedReason(t *testing.T) {
	tests := []struct {
		name        string
		lastMessage string
		want        string
	}{
		{
			name:        "awaiting_approval: 承認",
			lastMessage: "次のタスクを提案します。承認しますか？",
			want:        BlockedReasonAwaitingApproval,
		},
		{
			name:        "awaiting_approval: してください",
			lastMessage: "Chrome を再起動してください",
			want:        BlockedReasonAwaitingApproval,
		},
		{
			name:        "awaiting_approval: please confirm",
			lastMessage: "Please confirm before I push to main.",
			want:        BlockedReasonAwaitingApproval,
		},
		{
			name:        "awaiting_approval: shall i proceed",
			lastMessage: "Shall I proceed with the refactoring?",
			want:        BlockedReasonAwaitingApproval,
		},
		{
			name:        "awaiting_input: どうしますか",
			lastMessage: "どうしますか？",
			want:        BlockedReasonAwaitingInput,
		},
		{
			name:        "awaiting_input: 教えてください",
			lastMessage: "リポジトリ名を教えてください",
			want:        BlockedReasonAwaitingInput,
		},
		{
			name:        "awaiting_input: let me know",
			lastMessage: "Let me know if you want any changes.",
			want:        BlockedReasonAwaitingInput,
		},
		{
			name:        "awaiting_input: full-width question mark",
			lastMessage: "この方針で進めてよいですか？",
			want:        BlockedReasonAwaitingInput,
		},
		{
			name:        "rate_limit: you've hit your limit",
			lastMessage: "You've hit your limit · resets 6pm (Asia/Tokyo)",
			want:        BlockedReasonRateLimit,
		},
		{
			name:        "rate_limit: rate limit",
			lastMessage: "Rate limit exceeded, please wait",
			want:        BlockedReasonRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectBlockedReason(tt.lastMessage)
			if got != tt.want {
				t.Errorf("DetectBlockedReason(%q) = %q, want %q", tt.lastMessage, got, tt.want)
			}
		})
	}
}

func TestDetectStatus(t *testing.T) {
	tests := []struct {
		name        string
		lastMessage string
		lastRole    string
		alive       bool
		errorType   string
		isAPIError  bool
		want        string
	}{
		// Metadata-based error detection
		{
			name:        "metadata: rate_limit error",
			lastMessage: "You've hit your limit · resets 6pm (Asia/Tokyo)",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "rate_limit",
			isAPIError:  true,
			want:        StatusError,
		},
		{
			name:        "metadata: invalid_request error",
			lastMessage: "Prompt is too long",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "invalid_request",
			isAPIError:  true,
			want:        StatusError,
		},
		{
			name:        "metadata: authentication_failed error",
			lastMessage: "Not logged in · Please run /login",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "authentication_failed",
			isAPIError:  true,
			want:        StatusError,
		},
		{
			name:        "metadata: errorType without isAPIError flag is not error",
			lastMessage: "some message",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "rate_limit",
			isAPIError:  false,
			want:        StatusIdle,
		},
		{
			name:        "metadata: isAPIError without errorType is not error",
			lastMessage: "some message",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "",
			isAPIError:  true,
			want:        StatusIdle,
		},
		// Text mentioning errors should NOT trigger error status without metadata
		{
			name:        "no false positive: message mentions API Error but no metadata",
			lastMessage: "API Error: 400 {\"type\":\"error\"}",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "",
			isAPIError:  false,
			want:        StatusIdle,
		},
		{
			name:        "rate limit text without metadata is blocked (not error)",
			lastMessage: "Rate limit exceeded, please wait",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "",
			isAPIError:  false,
			want:        StatusBlocked,
		},
		// Blocked patterns (unchanged)
		{
			name:        "blocked: してください",
			lastMessage: "Chrome を再起動してください",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: してもらう必要",
			lastMessage: "Chrome を再起動してもらう必要があります",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: 承認",
			lastMessage: "次のタスクを提案します。承認しますか？",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: どうしますか",
			lastMessage: "どうしますか？",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: よろしいですか",
			lastMessage: "kill してよろしいですか？",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: 教えてください",
			lastMessage: "リポジトリ名を教えてください",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked pattern but user role - active",
			lastMessage: "Chrome を再起動してください",
			lastRole:    "user",
			alive:       true,
			want:        StatusActive,
		},
		{
			name:        "process dead",
			lastMessage: "作業完了しました",
			lastRole:    "assistant",
			alive:       false,
			want:        StatusDead,
		},
		{
			name:        "active: user sent message, waiting for response",
			lastMessage: "テストを実行して",
			lastRole:    "user",
			alive:       true,
			want:        StatusActive,
		},
		{
			name:        "idle: assistant finished, no blocking pattern",
			lastMessage: "全てのCIチェックがパスしました！",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusIdle,
		},
		{
			name:        "empty message, alive",
			lastMessage: "",
			lastRole:    "",
			alive:       true,
			want:        StatusIdle,
		},
		{
			name:        "empty message, dead",
			lastMessage: "",
			lastRole:    "",
			alive:       false,
			want:        StatusDead,
		},
		// Blocked patterns (full-width question mark, English patterns)
		{
			name:        "blocked: full-width question mark at end",
			lastMessage: "この方針で進めてよいですか？",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: full-width question mark with trailing whitespace",
			lastMessage: "実行しますか？ \n",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: どう思いますか",
			lastMessage: "このアプローチについてどう思いますか",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: 次は何をしましょう",
			lastMessage: "完了しました。次は何をしましょうか",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: いかがでしょうか",
			lastMessage: "この実装でいかがでしょうか",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: would you like to proceed",
			lastMessage: "I've prepared the changes. Would you like to proceed with the deployment?",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: shall i proceed",
			lastMessage: "Shall I proceed with the refactoring?",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: should i proceed",
			lastMessage: "Should I proceed with this approach?",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: do you want me to",
			lastMessage: "Do you want me to run the tests?",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: would you like me to",
			lastMessage: "Would you like me to create a PR?",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: please confirm",
			lastMessage: "Please confirm before I push to main.",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: please review",
			lastMessage: "Please review the changes above.",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "blocked: let me know",
			lastMessage: "Let me know if you want any changes.",
			lastRole:    "assistant",
			alive:       true,
			want:        StatusBlocked,
		},
		{
			name:        "not blocked: question mark from user role",
			lastMessage: "これでいいですか？",
			lastRole:    "user",
			alive:       true,
			want:        StatusActive,
		},
		// Error metadata takes priority over blocked patterns
		{
			name:        "error metadata overrides blocked pattern",
			lastMessage: "承認してください",
			lastRole:    "assistant",
			alive:       true,
			errorType:   "rate_limit",
			isAPIError:  true,
			want:        StatusError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectStatus(tt.lastMessage, tt.lastRole, tt.alive, tt.errorType, tt.isAPIError)
			if got != tt.want {
				t.Errorf("DetectStatus(%q, %q, %v, %q, %v) = %q, want %q",
					tt.lastMessage, tt.lastRole, tt.alive, tt.errorType, tt.isAPIError, got, tt.want)
			}
		})
	}
}
