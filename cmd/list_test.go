package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildListSessionsJSON(t *testing.T) {
	now := time.Now()
	rows := buildListSessionsJSON([]store.Session{
		{
			ID:            "claude:chaspy/agentctl:s1",
			Agent:         "claude",
			Repository:    "chaspy/agentctl",
			GitBranch:     "feat/json",
			LastActive:    now,
			Alive:         true,
			Status:        "blocked",
			BlockedReason: "approval",
			PRURL:         "https://github.com/chaspy/agentctl/pull/99",
			LastMessage:   "waiting for review",
		},
	})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Role != "worker" {
		t.Fatalf("role = %q, want worker", rows[0].Role)
	}
	if rows[0].BlockedReason != "approval" {
		t.Fatalf("blocked_reason = %q, want approval", rows[0].BlockedReason)
	}
	if rows[0].Branch != "feat/json" {
		t.Fatalf("branch = %q, want feat/json", rows[0].Branch)
	}
}
