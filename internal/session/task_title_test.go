package session

import "testing"

func TestTruncateFallback(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "short message unchanged",
			msg:  "Fix auth bug",
			want: "Fix auth bug",
		},
		{
			name: "exactly 40 runes no truncation",
			msg:  "1234567890123456789012345678901234567890",
			want: "1234567890123456789012345678901234567890",
		},
		{
			name: "41 runes gets truncated",
			msg:  "12345678901234567890123456789012345678901",
			want: "1234567890123456789012345678901234567890...",
		},
		{
			name: "empty string",
			msg:  "",
			want: "",
		},
		{
			name: "whitespace only",
			msg:  "   ",
			want: "",
		},
		{
			name: "Japanese text under limit",
			msg:  "認証バグを修正する",
			want: "認証バグを修正する",
		},
		{
			name: "long Japanese text truncated",
			msg:  "これは非常に長い日本語のメッセージで、四十文字を超えるため切り詰められるべきです。",
			want: "これは非常に長い日本語のメッセージで、四十文字を超えるため切り詰められるべきです...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateFallback(tt.msg)
			if got != tt.want {
				t.Errorf("truncateFallback() = %q, want %q", got, tt.want)
			}
		})
	}
}
