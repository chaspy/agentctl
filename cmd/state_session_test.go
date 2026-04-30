package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestStateSessionSummaryUpdatesActiveSessionBySessionKey(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:summary-1",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "provider-session-1",
		ZellijSession: "summary-worker-codex",
		Status:        "idle",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastActive:    time.Date(2026, 4, 30, 5, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	var out bytes.Buffer
	stateSessionSummaryCmd.SetOut(&out)
	sessionSummaryText = "新しい要約"

	if err := runStateSessionSummary(stateSessionSummaryCmd, []string{"provider-session-1"}); err != nil {
		t.Fatalf("runStateSessionSummary: %v", err)
	}

	session, err := store.GetSession(db, "codex:chaspy/myassistant:summary-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.TaskSummary != "新しい要約" {
		t.Fatalf("task_summary = %q, want 新しい要約", session.TaskSummary)
	}
}

func TestStateSessionClearSummariesClearsActiveSessions(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	for _, session := range []*store.Session{
		{
			ID:            "codex:chaspy/myassistant:clear-1",
			Agent:         "codex",
			Repository:    "chaspy/myassistant",
			SessionID:     "clear-1",
			ZellijSession: "clear-worker-1",
			Status:        "idle",
			DesiredState:  store.DesiredStateRunning,
			RuntimeStatus: "running",
			LastActive:    time.Date(2026, 4, 30, 5, 0, 0, 0, time.UTC),
			TaskSummary:   "keep no more",
		},
		{
			ID:            "codex:chaspy/myassistant:clear-2",
			Agent:         "codex",
			Repository:    "chaspy/myassistant",
			SessionID:     "clear-2",
			ZellijSession: "clear-worker-2",
			Status:        "dead",
			DesiredState:  store.DesiredStateStopped,
			RuntimeStatus: "gone",
			LastActive:    time.Date(2026, 4, 30, 5, 0, 0, 0, time.UTC),
			TaskSummary:   "should remain",
		},
	} {
		if err := store.UpsertSession(db, session); err != nil {
			t.Fatalf("UpsertSession(%s): %v", session.ID, err)
		}
	}

	var out bytes.Buffer
	stateSessionClearSummariesCmd.SetOut(&out)

	if err := runStateSessionClearSummaries(stateSessionClearSummariesCmd, nil); err != nil {
		t.Fatalf("runStateSessionClearSummaries: %v", err)
	}

	active, err := store.GetSession(db, "codex:chaspy/myassistant:clear-1")
	if err != nil {
		t.Fatalf("GetSession active: %v", err)
	}
	if active.TaskSummary != "" {
		t.Fatalf("active task_summary = %q, want empty", active.TaskSummary)
	}
	dead, err := store.GetSession(db, "codex:chaspy/myassistant:clear-2")
	if err != nil {
		t.Fatalf("GetSession dead: %v", err)
	}
	if dead.TaskSummary != "should remain" {
		t.Fatalf("dead task_summary = %q, want unchanged", dead.TaskSummary)
	}
}

func TestStateSessionMarkDeadLogsAction(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:dead-1",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "dead-1",
		ZellijSession: "dead-worker-codex",
		Status:        "idle",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastActive:    time.Date(2026, 4, 30, 5, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	var out bytes.Buffer
	stateSessionMarkDeadCmd.SetOut(&out)
	sessionMarkDeadReason = "ghost cleanup"
	sessionMarkDeadLogAction = true
	sessionMarkDeadRouteReason = "manager-closeout"
	sessionMarkDeadResult = "closeout_mark_dead"
	t.Cleanup(func() {
		sessionMarkDeadReason = ""
		sessionMarkDeadLogAction = false
		sessionMarkDeadRouteReason = ""
		sessionMarkDeadResult = ""
	})

	if err := runStateSessionMarkDead(stateSessionMarkDeadCmd, []string{"dead-worker-codex"}); err != nil {
		t.Fatalf("runStateSessionMarkDead: %v", err)
	}

	session, err := store.GetSession(db, "codex:chaspy/myassistant:dead-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.Status != "dead" || session.DesiredState != store.DesiredStateStopped || session.RuntimeStatus != "gone" {
		t.Fatalf("session = %+v", session)
	}
	actions, err := store.GetActionsForSession(db, session.ID, 5)
	if err != nil {
		t.Fatalf("GetActionsForSession: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(actions))
	}
	if actions[0].RouteReason != "manager-closeout" || actions[0].Result != "closeout_mark_dead" {
		t.Fatalf("action = %+v", actions[0])
	}
}
