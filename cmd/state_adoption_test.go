package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateAdoptionListJSON(t *testing.T) {
	dbPath := withStateAdoptTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	adoption := &store.SessionAdoption{
		Agent:                 "codex",
		Mux:                   "zellij",
		ZellijSession:         "atama-local-app-protected",
		Repository:            "studiuos-jp/Studious_JP",
		CWD:                   "/tmp/repo",
		GitBranch:             "fix/atama-local-app-issues",
		Strategy:              store.AdoptionStrategyProtected,
		TargetPermissionLevel: store.PermissionSuggest,
		Status:                store.AdoptionStatusQueued,
		Note:                  "queue for later",
	}
	if err := store.CreateSessionAdoption(db, adoption); err != nil {
		t.Fatalf("CreateSessionAdoption: %v", err)
	}

	origJSON := stateAdoptionJSON
	stateAdoptionJSON = true
	defer func() { stateAdoptionJSON = origJSON }()

	var out bytes.Buffer
	stateAdoptionListCmd.SetOut(&out)
	if err := runStateAdoptionList(stateAdoptionListCmd, nil); err != nil {
		t.Fatalf("runStateAdoptionList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"ZellijSession":"atama-local-app-protected"`) {
		t.Fatalf("expected adoption JSON to contain zellij session, got %q", got)
	}
	if !strings.Contains(got, `"Status":"queued"`) {
		t.Fatalf("expected adoption JSON to contain queued status, got %q", got)
	}
}

func TestRunStateAdoptionCancelQueued(t *testing.T) {
	dbPath := withStateAdoptTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	adoption := &store.SessionAdoption{
		Agent:                 "codex",
		Mux:                   "zellij",
		ZellijSession:         "atama-local-app-protected",
		Repository:            "studiuos-jp/Studious_JP",
		CWD:                   "/tmp/repo",
		GitBranch:             "fix/atama-local-app-issues",
		Strategy:              store.AdoptionStrategyProtected,
		TargetPermissionLevel: store.PermissionSuggest,
		Status:                store.AdoptionStatusQueued,
	}
	if err := store.CreateSessionAdoption(db, adoption); err != nil {
		t.Fatalf("CreateSessionAdoption: %v", err)
	}

	var out bytes.Buffer
	stateAdoptionCancelCmd.SetOut(&out)
	if err := runStateAdoptionCancel(stateAdoptionCancelCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateAdoptionCancel: %v", err)
	}
	if !strings.Contains(out.String(), "Cancelled session adoption #1") {
		t.Fatalf("unexpected output: %q", out.String())
	}

	updated, err := store.GetSessionAdoption(db, 1)
	if err != nil {
		t.Fatalf("GetSessionAdoption: %v", err)
	}
	if updated.Status != store.AdoptionStatusCancelled {
		t.Fatalf("adoption status = %q, want %q", updated.Status, store.AdoptionStatusCancelled)
	}
}
