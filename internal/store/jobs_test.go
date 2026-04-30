package store

import (
	"testing"
	"time"
)

func TestJobCRUDAndRuns(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	job := &Job{
		Name:        "daily-report",
		Schedule:    "0 9 * * *",
		Action:      "spawn",
		Repo:        "chaspy/agentctl",
		Branch:      "main",
		Agent:       "codex",
		Instruction: "日次レポートを作成する",
		Enabled:     true,
	}
	if err := CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	gotByID, err := GetJobByID(db, job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if gotByID.Name != job.Name || !gotByID.Enabled {
		t.Fatalf("GetJobByID mismatch: %+v", gotByID)
	}

	gotByName, err := GetJobByName(db, job.Name)
	if err != nil {
		t.Fatalf("GetJobByName: %v", err)
	}
	if gotByName.ID != job.ID {
		t.Fatalf("GetJobByName ID = %d, want %d", gotByName.ID, job.ID)
	}

	jobs, err := ListJobs(db)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobs len = %d, want 1", len(jobs))
	}

	startedAt := time.Now().UTC().Truncate(time.Second)
	run := &JobRun{
		JobID:     job.ID,
		StartedAt: startedAt,
		Status:    "running",
		Output:    "started",
	}
	if err := CreateJobRun(db, run); err != nil {
		t.Fatalf("CreateJobRun: %v", err)
	}

	finishedAt := startedAt.Add(5 * time.Second)
	run.FinishedAt = &finishedAt
	run.Status = "success"
	run.Output = "completed"
	if err := UpdateJobRun(db, run); err != nil {
		t.Fatalf("UpdateJobRun: %v", err)
	}

	latestRun, err := GetLatestJobRun(db, job.ID)
	if err != nil {
		t.Fatalf("GetLatestJobRun: %v", err)
	}
	if latestRun.Status != "success" || latestRun.FinishedAt == nil {
		t.Fatalf("GetLatestJobRun mismatch: %+v", latestRun)
	}

	runs, err := ListJobRuns(db, job.ID, 10)
	if err != nil {
		t.Fatalf("ListJobRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Output != "completed" {
		t.Fatalf("ListJobRuns mismatch: %+v", runs)
	}

	if err := DeleteJob(db, job.ID); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if _, err := GetJobByID(db, job.ID); err == nil {
		t.Fatal("GetJobByID after delete: expected error")
	}
}

func TestStateSyncJobCanBeStored(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	job := &Job{
		Name:     "agentctl-state-sync",
		Schedule: "*/5 * * * *",
		Action:   "state-sync",
		Agent:    "all",
		Enabled:  true,
	}
	if err := CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	got, err := GetJobByName(db, job.Name)
	if err != nil {
		t.Fatalf("GetJobByName: %v", err)
	}
	if got.Action != "state-sync" || got.Agent != "all" {
		t.Fatalf("state-sync job mismatch: %+v", got)
	}
}

func TestJobLocks(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	job := &Job{
		Name:        "keepalive",
		Schedule:    "*/5 * * * *",
		Action:      "send",
		Session:     "manager",
		Instruction: "状況を確認する",
		Enabled:     true,
	}
	if err := CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := AcquireJobLock(db, job.ID, "host:123"); err != nil {
		t.Fatalf("AcquireJobLock: %v", err)
	}
	if err := AcquireJobLock(db, job.ID, "host:456"); err == nil {
		t.Fatal("AcquireJobLock second time: expected conflict")
	}

	lock, err := GetJobLock(db, job.ID)
	if err != nil {
		t.Fatalf("GetJobLock: %v", err)
	}
	if lock.LockedBy != "host:123" {
		t.Fatalf("GetJobLock.LockedBy = %q, want %q", lock.LockedBy, "host:123")
	}

	if err := ReleaseJobLock(db, job.ID); err != nil {
		t.Fatalf("ReleaseJobLock: %v", err)
	}
	if _, err := GetJobLock(db, job.ID); err == nil {
		t.Fatal("GetJobLock after release: expected error")
	}
}
