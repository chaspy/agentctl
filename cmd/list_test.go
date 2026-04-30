package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildListSessionsJSON(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	rows := buildListSessionsJSON([]store.Session{
		{
			ID:              "codex:chaspy/myassistant:s1",
			Agent:           "codex",
			Repository:      "chaspy/myassistant",
			SessionID:       "sess-1",
			ZellijSession:   "research-lead",
			GitBranch:       "feat/json",
			LastActive:      now.Add(-2 * time.Hour),
			LastSeenAliveAt: now,
			Status:          "idle",
			DesiredState:    store.DesiredStateRunning,
			RuntimeStatus:   "running",
			Role:            "lead",
			PRURL:           "https://github.com/chaspy/myassistant/pull/1",
			LastMessage:     "done",
		},
	})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Name != "research-lead" {
		t.Fatalf("name = %q, want research-lead", rows[0].Name)
	}
	if rows[0].Repo != "chaspy/myassistant" {
		t.Fatalf("repo = %q, want chaspy/myassistant", rows[0].Repo)
	}
	if rows[0].Role != "lead" {
		t.Fatalf("role = %q, want lead", rows[0].Role)
	}
	if rows[0].RuntimeStatus != "running" {
		t.Fatalf("runtime_status = %q, want running", rows[0].RuntimeStatus)
	}
	if rows[0].LastActive != now {
		t.Fatalf("last_active = %s, want %s", rows[0].LastActive, now)
	}
}

func TestFilterSessionsForListUsesObservedActivity(t *testing.T) {
	prevAll, prevHours, prevAgent := listAll, listHours, listAgent
	t.Cleanup(func() {
		listAll = prevAll
		listHours = prevHours
		listAgent = prevAgent
	})

	listAll = false
	listHours = 24
	listAgent = "all"

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	sessions := []store.Session{
		{
			ID:              "recent-alive",
			Agent:           "codex",
			LastActive:      now.Add(-72 * time.Hour),
			LastSeenAliveAt: now.Add(-5 * time.Minute),
		},
		{
			ID:            "recent-message",
			Agent:         "claude",
			LastMessageAt: now.Add(-10 * time.Minute),
		},
		{
			ID:         "stale",
			Agent:      "codex",
			LastActive: now.Add(-48 * time.Hour),
		},
	}

	filtered := filterSessionsForList(sessions, now)
	if len(filtered) != 2 {
		t.Fatalf("filtered sessions = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "recent-alive" {
		t.Fatalf("first session = %q, want recent-alive", filtered[0].ID)
	}
	if filtered[1].ID != "recent-message" {
		t.Fatalf("second session = %q, want recent-message", filtered[1].ID)
	}
}

func TestFilterSessionsForListAllIgnoresHoursButStillFiltersAgent(t *testing.T) {
	prevAll, prevHours, prevAgent := listAll, listHours, listAgent
	t.Cleanup(func() {
		listAll = prevAll
		listHours = prevHours
		listAgent = prevAgent
	})

	listAll = true
	listHours = 1
	listAgent = "codex"

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	sessions := []store.Session{
		{
			ID:         "old-codex",
			Agent:      "codex",
			LastActive: now.Add(-7 * 24 * time.Hour),
		},
		{
			ID:         "old-claude",
			Agent:      "claude",
			LastActive: now.Add(-7 * 24 * time.Hour),
		},
	}

	filtered := filterSessionsForList(sessions, now)
	if len(filtered) != 1 {
		t.Fatalf("filtered sessions = %d, want 1", len(filtered))
	}
	if filtered[0].ID != "old-codex" {
		t.Fatalf("filtered session = %q, want old-codex", filtered[0].ID)
	}
}
