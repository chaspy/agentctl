package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
)

// mockSessionResolver implements sessionResolver for testing.
type mockSessionResolver struct {
	sessions []string
}

func (m *mockSessionResolver) ResolveSession(query string) (string, error) {
	for _, s := range m.sessions {
		if s == query {
			return s, nil
		}
	}
	return "", fmt.Errorf("mux session %q not found", query)
}

func makeSession(repo string, age time.Duration) provider.SessionInfo {
	return provider.SessionInfo{
		Agent:      provider.AgentClaude,
		Repository: repo,
		FilePath:   "/tmp/" + repo + ".jsonl",
		ModTime:    time.Now().Add(-age),
	}
}

func TestMatchByRepository(t *testing.T) {
	sessions := []provider.SessionInfo{
		makeSession("studious/jp-Studious-JP-worktree-fix-issue-1738-line-chat-missing", 1*time.Minute),
		makeSession("chaspy/agentctl", 2*time.Minute),
		makeSession("org/other-repo", 3*time.Minute),
	}

	tests := []struct {
		query string
		want  int
	}{
		{"agentctl", 1},
		{"chaspy", 1},
		{"1738", 1},
		{"studious", 1},
		{"STUDIOUS", 1},   // case-insensitive
		{"org", 1},        // only "org/other-repo"
		{"repo", 1},       // only "other-repo"
		{"nonexistent", 0},
		{"studious-1738", 0}, // the problematic case — not a contiguous substring
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := matchByRepository(sessions, tt.query)
			if len(got) != tt.want {
				t.Errorf("matchByRepository(%q) = %d results, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestResolveSessionsForSend_repositoryMatch(t *testing.T) {
	sessions := []provider.SessionInfo{
		makeSession("chaspy/agentctl", 1*time.Minute),
		makeSession("org/other-repo", 2*time.Minute),
	}
	resolver := &mockSessionResolver{sessions: []string{"chaspy-agentctl", "org-other"}}

	got, err := resolveSessionsForSend(sessions, "agentctl", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Repository != "chaspy/agentctl" {
		t.Errorf("expected chaspy/agentctl, got %q", got[0].Repository)
	}
}

func TestResolveSessionsForSend_muxFallback(t *testing.T) {
	// Simulate the "studious-1738" case: no repo matches, but mux has the session.
	sessions := []provider.SessionInfo{
		makeSession("studious/jp-Studious-JP-worktree-fix-issue-1738-line-chat-missing", 1*time.Minute),
		makeSession("chaspy/agentctl", 5*time.Minute),
	}
	resolver := &mockSessionResolver{sessions: []string{"studious-1738", "chaspy-agentctl"}}

	got, err := resolveSessionsForSend(sessions, "studious-1738", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All sessions returned for best-effort monitoring
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions for best-effort monitoring, got %d", len(got))
	}
	// Most recently modified first
	if got[0].ModTime.Before(got[1].ModTime) {
		t.Errorf("sessions should be sorted by ModTime desc")
	}
}

func TestResolveSessionsForSend_muxFallbackNoJSONL(t *testing.T) {
	// Mux session exists but no JSONL sessions scanned.
	resolver := &mockSessionResolver{sessions: []string{"studious-1738"}}

	got, err := resolveSessionsForSend(nil, "studious-1738", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(got))
	}
}

func TestResolveSessionsForSend_noMatch(t *testing.T) {
	sessions := []provider.SessionInfo{
		makeSession("chaspy/agentctl", 1*time.Minute),
	}
	resolver := &mockSessionResolver{sessions: []string{"chaspy-agentctl"}}

	_, err := resolveSessionsForSend(sessions, "nonexistent-xyz", resolver)
	if err == nil {
		t.Fatal("expected error for unmatched query, got nil")
	}
}

func TestResolveSessionsForSend_repositoryMatchTakesPriority(t *testing.T) {
	// When repo match succeeds, mux fallback should NOT be used.
	sessions := []provider.SessionInfo{
		makeSession("chaspy/agentctl", 1*time.Minute),
		makeSession("org/other-repo", 2*time.Minute),
	}
	// The resolver has "agentctl" as a mux session too, but repo match should win.
	resolver := &mockSessionResolver{sessions: []string{"agentctl"}}

	got, err := resolveSessionsForSend(sessions, "agentctl", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only return the 1 repo-matched session, not all 2.
	if len(got) != 1 {
		t.Errorf("expected 1 result (repo match), got %d", len(got))
	}
	if got[0].Repository != "chaspy/agentctl" {
		t.Errorf("expected chaspy/agentctl, got %q", got[0].Repository)
	}
}
