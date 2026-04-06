package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestIsJobDue(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	job := &store.Job{
		Name:        "daily",
		Schedule:    "*/5 * * * *",
		Action:      "send",
		Session:     "manager",
		Instruction: "ping",
		Enabled:     true,
	}
	if err := store.CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	storedJob, err := store.GetJobByID(db, job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}

	due, err := isJobDue(db, *storedJob, storedJob.CreatedAt.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("isJobDue: %v", err)
	}
	if !due {
		t.Fatal("job should be due after 6 minutes")
	}

	startedAt := storedJob.CreatedAt.Add(6 * time.Minute)
	run := &store.JobRun{
		JobID:     job.ID,
		StartedAt: startedAt,
		Status:    "success",
		Output:    "ok",
	}
	if err := store.CreateJobRun(db, run); err != nil {
		t.Fatalf("CreateJobRun: %v", err)
	}

	due, err = isJobDue(db, *storedJob, startedAt.Add(30*time.Second))
	if err != nil {
		t.Fatalf("isJobDue: %v", err)
	}
	if due {
		t.Fatal("job should not be due immediately after the latest run")
	}
}

func TestRunJobSchedulerOnce(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	job := &store.Job{
		Name:        "scheduler-send",
		Schedule:    "* * * * *",
		Action:      "send",
		Session:     "manager",
		Instruction: "ping",
		Enabled:     true,
	}
	if err := store.CreateJob(db, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	storedJob, err := store.GetJobByID(db, job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}

	origSendExecutor := jobSendExecutor
	t.Cleanup(func() {
		jobSendExecutor = origSendExecutor
	})

	calls := 0
	jobSendExecutor = func(job *store.Job) (string, error) {
		calls++
		return "sent:" + job.Session, nil
	}

	var logs bytes.Buffer
	origLogger := jobSchedulerLogger
	t.Cleanup(func() {
		jobSchedulerLogger = origLogger
	})
	jobSchedulerLogger = func(format string, args ...any) {
		_, _ = logs.WriteString(fmt.Sprintf(format, args...))
	}

	checked, executed, err := runJobSchedulerOnce(db, storedJob.CreatedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("runJobSchedulerOnce: %v", err)
	}
	if checked != 1 {
		t.Fatalf("checked = %d, want 1", checked)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	runs, err := store.ListJobRuns(db, job.ID, 10)
	if err != nil {
		t.Fatalf("ListJobRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("runs = %+v", runs)
	}

	lock, err := store.GetJobLock(db, job.ID)
	if err == nil {
		t.Fatalf("job lock should be released, got %+v", lock)
	}
	if !strings.Contains(logs.String(), `executing job "scheduler-send" (action=send, target=manager)`) {
		t.Fatalf("scheduler log missing execution line: %q", logs.String())
	}
	if !strings.Contains(logs.String(), `job "scheduler-send" executed successfully`) {
		t.Fatalf("scheduler log missing success line: %q", logs.String())
	}
}
