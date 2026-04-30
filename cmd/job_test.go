package cmd

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func withJobTestDB(t *testing.T) string {
	t.Helper()
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
	return dbPath
}

func TestRunJobAddListDelete(t *testing.T) {
	dbPath := withJobTestDB(t)
	var out bytes.Buffer
	jobListCmd.SetOut(&out)
	jobAddCmd.SetOut(&out)
	jobDeleteCmd.SetOut(&out)

	jobAddName = "daily-report"
	jobAddSchedule = "0 9 * * *"
	jobAddAction = "spawn"
	jobAddRepo = "chaspy/agentctl"
	jobAddSession = ""
	jobAddBranch = "main"
	jobAddAgent = "codex"
	jobAddInstruction = "日次レポートを作る"

	if err := runJobAdd(jobAddCmd, nil); err != nil {
		t.Fatalf("runJobAdd: %v", err)
	}

	out.Reset()
	if err := runJobList(jobListCmd, nil); err != nil {
		t.Fatalf("runJobList: %v", err)
	}
	if !strings.Contains(out.String(), "daily-report") {
		t.Fatalf("runJobList output = %q", out.String())
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	job, err := store.GetJobByName(db, "daily-report")
	if err != nil {
		t.Fatalf("GetJobByName: %v", err)
	}

	out.Reset()
	if err := runJobDelete(jobDeleteCmd, []string{job.Name}); err != nil {
		t.Fatalf("runJobDelete: %v", err)
	}
	if _, err := store.GetJobByID(db, job.ID); err == nil {
		t.Fatal("job should be deleted")
	}
}

func TestRunJobAddStateSyncWithoutInstruction(t *testing.T) {
	dbPath := withJobTestDB(t)
	var out bytes.Buffer
	jobAddCmd.SetOut(&out)
	jobListCmd.SetOut(&out)

	jobAddName = "agentctl-state-sync"
	jobAddSchedule = "*/5 * * * *"
	jobAddAction = "state_sync"
	jobAddRepo = ""
	jobAddSession = ""
	jobAddBranch = ""
	jobAddAgent = "codex"
	jobAddInstruction = ""
	jobAddCwd = ""
	jobAddTimeout = 600

	if err := runJobAdd(jobAddCmd, nil); err != nil {
		t.Fatalf("runJobAdd: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	job, err := store.GetJobByName(db, "agentctl-state-sync")
	if err != nil {
		t.Fatalf("GetJobByName: %v", err)
	}
	if job.Action != "state-sync" || job.Agent != "codex" {
		t.Fatalf("job = %+v", job)
	}

	out.Reset()
	if err := runJobList(jobListCmd, nil); err != nil {
		t.Fatalf("runJobList: %v", err)
	}
	if !strings.Contains(out.String(), "state-sync") || !strings.Contains(out.String(), "codex") {
		t.Fatalf("runJobList output = %q", out.String())
	}
}

func TestRunJobRunAndLogs(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	job := &store.Job{
		Name:        "heartbeat",
		Schedule:    "*/5 * * * *",
		Action:      "send",
		Session:     "manager",
		Instruction: "状況を確認する",
		Enabled:     true,
	}
	if err := store.CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	origSendExecutor := jobSendExecutor
	origNowFunc := nowFunc
	t.Cleanup(func() {
		jobSendExecutor = origSendExecutor
		nowFunc = origNowFunc
	})

	jobSendExecutor = func(job *store.Job) (string, error) {
		return "ok: " + job.Session, nil
	}
	nowTick := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		nowTick = nowTick.Add(1 * time.Second)
		return nowTick
	}

	var out bytes.Buffer
	jobRunCmd.SetOut(&out)
	jobLogsCmd.SetOut(&out)

	if err := runJobRun(jobRunCmd, []string{job.Name}); err != nil {
		t.Fatalf("runJobRun: %v", err)
	}
	if !strings.Contains(out.String(), "Ran job") {
		t.Fatalf("runJobRun output = %q", out.String())
	}

	runs, err := store.ListJobRuns(db, job.ID, 10)
	if err != nil {
		t.Fatalf("ListJobRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("runs = %+v", runs)
	}

	out.Reset()
	jobLogsLimit = 10
	if err := runJobLogs(jobLogsCmd, []string{job.Name}); err != nil {
		t.Fatalf("runJobLogs: %v", err)
	}
	if !strings.Contains(out.String(), "ok: manager") {
		t.Fatalf("runJobLogs output = %q", out.String())
	}
}

func TestRunJobRunStateSync(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	job := &store.Job{
		Name:     "agentctl-state-sync",
		Schedule: "*/5 * * * *",
		Action:   "state-sync",
		Agent:    "all",
		Enabled:  true,
	}
	if err := store.CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	origStateSyncExecutor := jobStateSyncExecutor
	origNowFunc := nowFunc
	t.Cleanup(func() {
		jobStateSyncExecutor = origStateSyncExecutor
		nowFunc = origNowFunc
	})

	jobStateSyncExecutor = func(db *sql.DB, job *store.Job) (string, error) {
		if job.Agent != "all" {
			t.Fatalf("job.Agent = %q, want all", job.Agent)
		}
		return "Synced 2 sessions to database\n", nil
	}
	nowTick := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		nowTick = nowTick.Add(1 * time.Second)
		return nowTick
	}

	var out bytes.Buffer
	jobRunCmd.SetOut(&out)

	if err := runJobRun(jobRunCmd, []string{job.Name}); err != nil {
		t.Fatalf("runJobRun: %v", err)
	}
	if !strings.Contains(out.String(), "Synced 2 sessions") {
		t.Fatalf("runJobRun output = %q", out.String())
	}

	runs, err := store.ListJobRuns(db, job.ID, 10)
	if err != nil {
		t.Fatalf("ListJobRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestRunJobRunRejectsAlreadyLockedJob(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	job := &store.Job{
		Name:        "locked-job",
		Schedule:    "*/5 * * * *",
		Action:      "send",
		Session:     "manager",
		Instruction: "ping",
		Enabled:     true,
	}
	if err := store.CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := store.AcquireJobLock(db, job.ID, "scheduler:123"); err != nil {
		t.Fatalf("AcquireJobLock: %v", err)
	}

	var out bytes.Buffer
	jobRunCmd.SetOut(&out)

	err = runJobRun(jobRunCmd, []string{job.Name})
	if err == nil {
		t.Fatal("runJobRun: expected already running error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("runJobRun error = %v", err)
	}

	runs, err := store.ListJobRuns(db, job.ID, 10)
	if err != nil {
		t.Fatalf("ListJobRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want 0", len(runs))
	}
}

func TestValidateJob(t *testing.T) {
	err := validateJob(&store.Job{
		Name:        "invalid",
		Schedule:    "* * * * *",
		Action:      "send",
		Instruction: "ping",
	})
	if err == nil {
		t.Fatal("validateJob should fail without session")
	}

	err = validateJob(&store.Job{
		Name:     "state-sync",
		Schedule: "* * * * *",
		Action:   "state_sync",
	})
	if err != nil {
		t.Fatalf("validateJob state-sync: %v", err)
	}
}
