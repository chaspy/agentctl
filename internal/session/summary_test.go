package session

import "testing"

func TestGenerateAutoSummary(t *testing.T) {
	tests := []struct {
		name        string
		lastMessage string
		lastRole    string
		want        string
	}{
		{
			name:        "assistant message under 80 chars",
			lastMessage: "I've completed the refactoring of the auth module.",
			lastRole:    "assistant",
			want:        "I've completed the refactoring of the auth module.",
		},
		{
			name:        "assistant message over 80 chars gets truncated",
			lastMessage: "This is a very long message that exceeds eighty characters and should be truncated with an ellipsis at the end.",
			lastRole:    "assistant",
			want:        "This is a very long message that exceeds eighty characters and should be truncat...",
		},
		{
			name:        "user role returns empty",
			lastMessage: "Please fix the bug",
			lastRole:    "user",
			want:        "",
		},
		{
			name:        "empty message returns empty",
			lastMessage: "",
			lastRole:    "assistant",
			want:        "",
		},
		{
			name:        "empty role returns empty",
			lastMessage: "some message",
			lastRole:    "",
			want:        "",
		},
		{
			name:        "exactly 80 chars no truncation",
			lastMessage: "12345678901234567890123456789012345678901234567890123456789012345678901234567890",
			lastRole:    "assistant",
			want:        "12345678901234567890123456789012345678901234567890123456789012345678901234567890",
		},
		{
			name:        "81 chars gets truncated",
			lastMessage: "123456789012345678901234567890123456789012345678901234567890123456789012345678901",
			lastRole:    "assistant",
			want:        "12345678901234567890123456789012345678901234567890123456789012345678901234567890...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateAutoSummary(tt.lastMessage, tt.lastRole)
			if got != tt.want {
				t.Errorf("GenerateAutoSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}
