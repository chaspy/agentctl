package cmd

import (
	"bytes"
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
}
