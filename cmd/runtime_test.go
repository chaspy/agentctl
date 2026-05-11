package cmd

import (
	"database/sql"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestReconcileRuntimeOwnershipReadOnlyClassifiesRecordedProcesses(t *testing.T) {
	db := openRuntimeTestDB(t)
	defer db.Close()
	now := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	sessions := []store.Session{
		{
			ID:               "codex:test:running",
			Agent:            "codex",
			Repository:       "test",
			SessionID:        "running",
			ZellijSession:    "running-session",
			Status:           "active",
			DesiredState:     store.DesiredStateRunning,
			RuntimeStatus:    "running",
			RuntimePID:       111,
			RuntimePGID:      110,
			RuntimeStartedAt: startedAt,
		},
		{
			ID:               "codex:test:gone",
			Agent:            "codex",
			Repository:       "test",
			SessionID:        "gone",
			ZellijSession:    "gone-session",
			Status:           "dead",
			DesiredState:     store.DesiredStateStopped,
			RuntimeStatus:    "gone",
			RuntimePID:       222,
			RuntimePGID:      220,
			RuntimeStartedAt: startedAt,
		},
		{
			ID:               "codex:test:orphan",
			Agent:            "codex",
			Repository:       "test",
			SessionID:        "orphan",
			ZellijSession:    "orphan-session",
			Status:           "dead",
			DesiredState:     store.DesiredStateStopped,
			RuntimeStatus:    "gone",
			RuntimePID:       333,
			RuntimePGID:      330,
			RuntimeStartedAt: startedAt,
		},
	}
	for _, session := range sessions {
		if err := store.UpsertSession(db, &session); err != nil {
			t.Fatalf("UpsertSession(%s): %v", session.ID, err)
		}
	}

	restore := stubRuntimeInspection(
		t,
		map[int]int{111: 110},
		map[int]bool{110: true, 220: false, 330: true},
	)
	defer restore()

	rows, err := reconcileRuntimeOwnership(db, runtimeReconcileOptions{Now: now, MinAge: time.Minute})
	if err != nil {
		t.Fatalf("reconcileRuntimeOwnership: %v", err)
	}
	bySession := runtimeRowsBySession(rows)
	if bySession["running-session"].Action != "keep" || bySession["running-session"].ObservedState != "running" {
		t.Fatalf("running row = %+v", bySession["running-session"])
	}
	if bySession["gone-session"].Action != "would_clear_ownership" || bySession["gone-session"].ObservedState != "gone" {
		t.Fatalf("gone row = %+v", bySession["gone-session"])
	}
	if bySession["orphan-session"].Action != "would_terminate_group" || bySession["orphan-session"].ObservedState != "orphaned_group" {
		t.Fatalf("orphan row = %+v", bySession["orphan-session"])
	}
}

func TestReconcileRuntimeOwnershipApplyClearsGoneOwnership(t *testing.T) {
	db := openRuntimeTestDB(t)
	defer db.Close()
	now := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	session := &store.Session{
		ID:               "codex:test:gone",
		Agent:            "codex",
		Repository:       "test",
		SessionID:        "gone",
		ZellijSession:    "gone-session",
		Status:           "dead",
		DesiredState:     store.DesiredStateStopped,
		RuntimeStatus:    "gone",
		RuntimePID:       222,
		RuntimePGID:      220,
		RuntimeStartedAt: now.Add(-10 * time.Minute),
	}
	if err := store.UpsertSession(db, session); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	restore := stubRuntimeInspection(t, nil, map[int]bool{220: false})
	defer restore()

	rows, err := reconcileRuntimeOwnership(db, runtimeReconcileOptions{Apply: true, Now: now, MinAge: time.Minute})
	if err != nil {
		t.Fatalf("reconcileRuntimeOwnership: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "clear_ownership" || !rows[0].Applied {
		t.Fatalf("rows = %+v", rows)
	}
	got, err := store.GetSession(db, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.RuntimePID != 0 || got.RuntimePGID != 0 || !got.RuntimeStartedAt.IsZero() {
		t.Fatalf("runtime ownership not cleared: %+v", got)
	}
}

func TestReconcileRuntimeOwnershipApplyTerminatesStoppedGroupWhenRequested(t *testing.T) {
	db := openRuntimeTestDB(t)
	defer db.Close()
	now := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	session := &store.Session{
		ID:               "codex:test:orphan",
		Agent:            "codex",
		Repository:       "test",
		SessionID:        "orphan",
		ZellijSession:    "orphan-session",
		Status:           "dead",
		DesiredState:     store.DesiredStateStopped,
		RuntimeStatus:    "gone",
		RuntimePID:       333,
		RuntimePGID:      330,
		RuntimeStartedAt: now.Add(-10 * time.Minute),
	}
	if err := store.UpsertSession(db, session); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	restore := stubRuntimeInspection(t, nil, map[int]bool{330: true})
	defer restore()
	var signals []syscall.Signal
	origSignal := runtimeReconcileSignalProcessGroup
	origSleep := runtimeReconcileSleep
	runtimeReconcileSignalProcessGroup = func(pgid int, sig syscall.Signal) error {
		if pgid != 330 {
			t.Fatalf("pgid = %d, want 330", pgid)
		}
		signals = append(signals, sig)
		return nil
	}
	runtimeReconcileSleep = func(time.Duration) {}
	defer func() {
		runtimeReconcileSignalProcessGroup = origSignal
		runtimeReconcileSleep = origSleep
	}()

	rows, err := reconcileRuntimeOwnership(db, runtimeReconcileOptions{
		Apply:            true,
		TerminateStopped: true,
		Now:              now,
		MinAge:           time.Minute,
	})
	if err != nil {
		t.Fatalf("reconcileRuntimeOwnership: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "terminate_group" || !rows[0].Applied {
		t.Fatalf("rows = %+v", rows)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %+v", signals)
	}
}

func runtimeRowsBySession(rows []runtimeReconcileRow) map[string]runtimeReconcileRow {
	result := make(map[string]runtimeReconcileRow, len(rows))
	for _, row := range rows {
		result[row.ZellijSession] = row
	}
	return result
}

func openRuntimeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	return db
}

func stubRuntimeInspection(t *testing.T, pidPGIDs map[int]int, groups map[int]bool) func() {
	t.Helper()
	origGetpgid := runtimeReconcileGetpgid
	origGroupExists := runtimeReconcileProcessGroupExists
	runtimeReconcileGetpgid = func(pid int) (int, error) {
		if pgid, ok := pidPGIDs[pid]; ok {
			return pgid, nil
		}
		return 0, syscall.ESRCH
	}
	runtimeReconcileProcessGroupExists = func(pgid int) bool {
		return groups[pgid]
	}
	return func() {
		runtimeReconcileGetpgid = origGetpgid
		runtimeReconcileProcessGroupExists = origGroupExists
	}
}

func TestIsNoSuchProcess(t *testing.T) {
	if !isNoSuchProcess(syscall.ESRCH) {
		t.Fatal("ESRCH should be treated as no such process")
	}
	if isNoSuchProcess(errors.New("other")) {
		t.Fatal("unrelated errors should not be treated as no such process")
	}
}
