package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildStateShowReportSummarizesHealth(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:blocked",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "blocked",
		Status:        "blocked",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "blocked-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:error",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "error",
		Status:        "error",
		Alive:         false,
		RuntimeStatus: "gone",
		ZellijSession: "error-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:ghost",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "ghost",
		Status:        "idle",
		Alive:         true,
		RuntimeStatus: "gone",
		ZellijSession: "ghost-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:dup1",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "dup1",
		Status:        "idle",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "dup-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "codex:a/b:dup2",
		Agent:         "codex",
		Repository:    "a/b",
		SessionID:     "dup2",
		Status:        "blocked",
		BlockedReason: "rate_limit",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "dup-session",
		LastActive:    now,
	})

	report, err := buildStateShowReport(db)
	if err != nil {
		t.Fatalf("buildStateShowReport: %v", err)
	}

	if report.Summary.ActiveSessions != 5 {
		t.Fatalf("active_sessions = %d, want 5", report.Summary.ActiveSessions)
	}
	if report.Summary.BlockedSessions != 2 {
		t.Fatalf("blocked_sessions = %d, want 2", report.Summary.BlockedSessions)
	}
	if report.Summary.ErrorSessions != 1 {
		t.Fatalf("error_sessions = %d, want 1", report.Summary.ErrorSessions)
	}
	if report.Summary.GhostSessions != 1 {
		t.Fatalf("ghost_sessions = %d, want 1", report.Summary.GhostSessions)
	}
	if report.Summary.DuplicateGroups != 1 {
		t.Fatalf("duplicate_groups = %d, want 1", report.Summary.DuplicateGroups)
	}
	if report.Summary.DuplicateSessions != 2 {
		t.Fatalf("duplicate_sessions = %d, want 2", report.Summary.DuplicateSessions)
	}

	var blocked, errorSess, ghost, dup1, dup2 *stateShowSession
	for i := range report.Sessions {
		s := &report.Sessions[i]
		switch s.ID {
		case "claude:a/b:blocked":
			blocked = s
		case "claude:a/b:error":
			errorSess = s
		case "claude:a/b:ghost":
			ghost = s
		case "claude:a/b:dup1":
			dup1 = s
		case "codex:a/b:dup2":
			dup2 = s
		}
	}

	if blocked == nil || errorSess == nil || ghost == nil || dup1 == nil || dup2 == nil {
		t.Fatalf("missing one or more sessions in report: %+v", report.Sessions)
	}
	if blocked.Duplicate || blocked.Ghost {
		t.Fatalf("blocked session flags unexpected: %+v", blocked)
	}
	if len(errorSess.Health) != 1 || errorSess.Health[0] != "error" {
		t.Fatalf("error session health unexpected: %+v", errorSess.Health)
	}
	if errorSess.Ghost {
		t.Fatalf("error session should not be ghost: %+v", errorSess)
	}
	if errorSess.Duplicate {
		t.Fatalf("error session should not be duplicate: %+v", errorSess)
	}
	if !ghost.Ghost {
		t.Fatalf("ghost session should be flagged ghost: %+v", ghost)
	}
	if !dup1.Duplicate || !dup2.Duplicate {
		t.Fatalf("duplicate sessions should be flagged duplicate: dup1=%+v dup2=%+v", dup1, dup2)
	}
	if dup1.DuplicateGroup != "dup-session" || dup2.DuplicateGroup != "dup-session" {
		t.Fatalf("duplicate group mismatch: dup1=%q dup2=%q", dup1.DuplicateGroup, dup2.DuplicateGroup)
	}
}
