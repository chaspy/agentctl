package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateLogHandoff(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	t.Cleanup(func() {
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	origRouteReason := logRouteReason
	origHandoffSummary := logHandoffSummary
	origTokenBurn := logTokenBurn
	t.Cleanup(func() {
		logRouteReason = origRouteReason
		logHandoffSummary = origHandoffSummary
		logTokenBurn = origTokenBurn
	})

	logRouteReason = "implementation task prefers claude"
	logHandoffSummary = "added route reason persistence"
	logTokenBurn = 4096

	if err := runStateLogHandoff(stateLogHandoffCmd, []string{"claude:a/b:s1"}); err != nil {
		t.Fatalf("runStateLogHandoff: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	actions, err := store.GetActionsForSession(db, "claude:a/b:s1", 10)
	if err != nil {
		t.Fatalf("GetActionsForSession: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}

	action := actions[0]
	if action.ActionType != "handoff" {
		t.Fatalf("action_type = %q", action.ActionType)
	}
	if action.RouteReason != logRouteReason {
		t.Fatalf("route_reason = %q", action.RouteReason)
	}
	if action.HandoffSummary != logHandoffSummary {
		t.Fatalf("handoff_summary = %q", action.HandoffSummary)
	}
	if action.TokenBurn != logTokenBurn {
		t.Fatalf("token_burn = %d", action.TokenBurn)
	}
}
