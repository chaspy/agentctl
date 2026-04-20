package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

var (
	jobSchedulerPollInterval = 30 * time.Second
	jobSchedulerLogger       = func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format, args...)
	}
)

var jobSchedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Run the job scheduler loop",
	RunE:  runJobScheduler,
}

func init() {
	jobCmd.AddCommand(jobSchedulerCmd)
}

func runJobScheduler(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	logf := func(format string, args ...any) {
		fmt.Fprintf(cmd.ErrOrStderr(), format, args...)
	}

	logf("job scheduler started (poll interval: %s)\n", jobSchedulerPollInterval)

	ticker := time.NewTicker(jobSchedulerPollInterval)
	defer ticker.Stop()

	for {
		checked, executed, err := runJobSchedulerOnce(db, nowFunc())
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "scheduler tick failed: %v\n", err)
		} else {
			logf("scheduler tick: checked %d jobs, executed %d\n", checked, executed)
		}
		<-ticker.C
	}
}

func runJobSchedulerOnce(db *sql.DB, now time.Time) (int, int, error) {
	jobs, err := store.ListJobs(db)
	if err != nil {
		return 0, 0, fmt.Errorf("listing jobs: %w", err)
	}

	checked := 0
	executed := 0
	for _, job := range jobs {
		checked++
		if !job.Enabled {
			continue
		}
		due, err := isJobDue(db, job, now)
		if err != nil {
			return checked, executed, fmt.Errorf("checking job %q: %w", job.Name, err)
		}
		if !due {
			continue
		}
		if err := runScheduledJob(db, &job); err != nil {
			return checked, executed, err
		}
		executed++
	}

	return checked, executed, nil
}

func runScheduledJob(db *sql.DB, job *store.Job) error {
	target := job.Repo
	switch job.Action {
	case "send":
		target = job.Session
	case "command":
		target = job.Instruction
	}
	jobSchedulerLogger("executing job %q (action=%s, target=%s)\n", job.Name, job.Action, target)

	lockedBy, err := schedulerLockOwner()
	if err != nil {
		return err
	}
	if err := store.AcquireJobLock(db, job.ID, lockedBy); err != nil {
		if isUniqueConstraintError(err) {
			return nil
		}
		return fmt.Errorf("acquiring job lock for %q: %w", job.Name, err)
	}
	defer store.ReleaseJobLock(db, job.ID)

	if _, err := runStoredJob(db, job); err != nil {
		return err
	}
	jobSchedulerLogger("job %q executed successfully\n", job.Name)
	return nil
}

func isJobDue(db *sql.DB, job store.Job, now time.Time) (bool, error) {
	schedule, err := cron.ParseStandard(job.Schedule)
	if err != nil {
		return false, fmt.Errorf("parsing cron schedule %q: %w", job.Schedule, err)
	}

	latestRun, err := store.GetLatestJobRun(db, job.ID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	base := job.CreatedAt
	if latestRun != nil {
		base = normalizeCronBase(latestRun.StartedAt)
	}

	nextRun := schedule.Next(base)
	return !nextRun.After(now), nil
}

func normalizeCronBase(ts time.Time) time.Time {
	base := ts.Truncate(time.Minute)
	if ts.After(base) {
		return base.Add(time.Minute)
	}
	return base
}

func schedulerLockOwner() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("resolving hostname: %w", err)
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid()), nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr interface{ Error() string }
	if errors.As(err, &sqliteErr) {
		return strings.Contains(strings.ToLower(sqliteErr.Error()), "unique")
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
