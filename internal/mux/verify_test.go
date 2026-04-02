package mux

import (
	"fmt"
	"testing"
	"time"
)

func TestParseZellijSessionsDetailed(t *testing.T) {
	// Simulate raw output with ANSI codes
	raw := "\x1b[32;1magent-manager\x1b[m [Created \x1b[35;1m17days\x1b[m ago] (\x1b[31;1mEXITED\x1b[m - attach to resurrect)\n" +
		"\x1b[32;1mmyassistant\x1b[m [Created \x1b[35;1m22h\x1b[m ago] \n" +
		"\x1b[32;1magentctl-fix\x1b[m [Created \x1b[35;1m1h\x1b[m ago] (\x1b[31;1mEXITED\x1b[m - attach to resurrect)\n"

	sessions := parseZellijSessionsDetailed(raw)
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// agent-manager: EXITED
	if sessions[0].Name != "agent-manager" || !sessions[0].Exited {
		t.Errorf("session 0: name=%q exited=%v, want agent-manager/true", sessions[0].Name, sessions[0].Exited)
	}
	// myassistant: active
	if sessions[1].Name != "myassistant" || sessions[1].Exited {
		t.Errorf("session 1: name=%q exited=%v, want myassistant/false", sessions[1].Name, sessions[1].Exited)
	}
	// agentctl-fix: EXITED
	if sessions[2].Name != "agentctl-fix" || !sessions[2].Exited {
		t.Errorf("session 2: name=%q exited=%v, want agentctl-fix/true", sessions[2].Name, sessions[2].Exited)
	}
}

func TestStripAnsi(t *testing.T) {
	input := "\x1b[32;1mhello\x1b[m world"
	got := stripAnsi(input)
	if got != "hello world" {
		t.Errorf("stripAnsi(%q) = %q, want %q", input, got, "hello world")
	}
}

func TestHasPendingInput(t *testing.T) {
	tests := []struct {
		name       string
		screenDump string
		sentText   string
		want       bool
	}{
		{
			name:       "empty screen",
			screenDump: "",
			sentText:   "hello",
			want:       false,
		},
		{
			name:       "empty sentText",
			screenDump: "some content",
			sentText:   "",
			want:       false,
		},
		{
			name:       "text pending on prompt line",
			screenDump: "Previous output\n\n> fix the bug in auth.go",
			sentText:   "fix the bug in auth.go",
			want:       true,
		},
		{
			name:       "text submitted with indicator below",
			screenDump: "Previous output\n\n> fix the bug in auth.go\n\nThinking...",
			sentText:   "fix the bug in auth.go",
			want:       false,
		},
		{
			name:       "text submitted with empty prompt below",
			screenDump: "Previous output\n\nHuman: fix the bug in auth.go\n\n>",
			sentText:   "fix the bug in auth.go",
			want:       false,
		},
		{
			name:       "text pending without prompt prefix",
			screenDump: "Some output\n\nfix the bug in auth.go",
			sentText:   "fix the bug in auth.go",
			want:       true,
		},
		{
			name:       "long text uses suffix match",
			screenDump: "output\n\n> this is a very long instruction message that exceeds fifty characters for testing suffix matching",
			sentText:   "this is a very long instruction message that exceeds fifty characters for testing suffix matching",
			want:       true,
		},
		{
			name:       "long text submitted",
			screenDump: "output\n\n> this is a very long instruction message that exceeds fifty characters for testing suffix matching\n\nProcessing...",
			sentText:   "this is a very long instruction message that exceeds fifty characters for testing suffix matching",
			want:       false,
		},
		{
			name:       "screen with only blank lines",
			screenDump: "\n\n\n\n",
			sentText:   "hello",
			want:       false,
		},
		{
			name:       "text with leading/trailing whitespace",
			screenDump: "output\n\n>  fix bug  ",
			sentText:   "  fix bug  ",
			want:       true,
		},
		{
			name:       "wrapped text pending across lines",
			screenDump: "output\n\n> fix the bug in\nauth.go",
			sentText:   "fix the bug in auth.go",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasPendingInput(tt.screenDump, tt.sentText)
			if got != tt.want {
				t.Errorf("HasPendingInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mockAdapter implements Adapter for testing VerifySend.
type mockAdapter struct {
	dumpResults  []string
	dumpIdx      int
	dumpErr      error
	sendEnterErr error
	enterCalls   int
}

func (m *mockAdapter) Name() string                              { return "mock" }
func (m *mockAdapter) SendKeys(session string, text string) error { return nil }
func (m *mockAdapter) ListSessions() ([]string, error)           { return nil, nil }
func (m *mockAdapter) ResolveSession(q string) (string, error)   { return q, nil }
func (m *mockAdapter) available() bool                           { return true }

func (m *mockAdapter) SendEnter(session string) error {
	m.enterCalls++
	return m.sendEnterErr
}

func (m *mockAdapter) DumpScreen(session string) (string, error) {
	if m.dumpErr != nil {
		return "", m.dumpErr
	}
	if m.dumpIdx >= len(m.dumpResults) {
		return m.dumpResults[len(m.dumpResults)-1], nil
	}
	result := m.dumpResults[m.dumpIdx]
	m.dumpIdx++
	return result, nil
}

func TestVerifySend(t *testing.T) {
	origDelay := VerifyDelay
	VerifyDelay = 1 * time.Millisecond
	t.Cleanup(func() { VerifyDelay = origDelay })

	t.Run("text cleared on first check", func(t *testing.T) {
		m := &mockAdapter{
			dumpResults: []string{"output\n\nThinking..."},
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.enterCalls != 0 {
			t.Errorf("expected 0 enter retries, got %d", m.enterCalls)
		}
	})

	t.Run("text cleared after one retry", func(t *testing.T) {
		m := &mockAdapter{
			dumpResults: []string{
				"output\n\n> fix the bug",    // still pending
				"output\n\nThinking...",       // cleared after retry
			},
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.enterCalls != 1 {
			t.Errorf("expected 1 enter retry, got %d", m.enterCalls)
		}
	})

	t.Run("text cleared after two retries", func(t *testing.T) {
		m := &mockAdapter{
			dumpResults: []string{
				"output\n\n> fix the bug",    // still pending
				"output\n\n> fix the bug",    // still pending
				"output\n\nThinking...",       // cleared
			},
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.enterCalls != 2 {
			t.Errorf("expected 2 enter retries, got %d", m.enterCalls)
		}
	})

	t.Run("text still pending after max retries", func(t *testing.T) {
		m := &mockAdapter{
			dumpResults: []string{
				"output\n\n> fix the bug",
				"output\n\n> fix the bug",
				"output\n\n> fix the bug",
				"output\n\n> fix the bug", // final check also fails
			},
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if m.enterCalls != 3 {
			t.Errorf("expected 3 enter retries, got %d", m.enterCalls)
		}
	})

	t.Run("dump-screen error", func(t *testing.T) {
		m := &mockAdapter{
			dumpErr: fmt.Errorf("connection refused"),
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("send-enter error on retry", func(t *testing.T) {
		m := &mockAdapter{
			dumpResults:  []string{"output\n\n> fix the bug"},
			sendEnterErr: fmt.Errorf("session not found"),
		}
		err := VerifySend(m, "test-session", "fix the bug")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if m.enterCalls != 1 {
			t.Errorf("expected 1 enter call, got %d", m.enterCalls)
		}
	})
}
