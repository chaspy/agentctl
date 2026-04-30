package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

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
