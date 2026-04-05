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

	ticker := time.NewTicker(jobSchedulerPollInterval)
	defer ticker.Stop()

	for {
		if _, err := runJobSchedulerOnce(db, nowFunc()); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "scheduler tick failed: %v\n", err)
		}
		<-ticker.C
	}
}

func runJobSchedulerOnce(db *sql.DB, now time.Time) (int, error) {
	jobs, err := store.ListJobs(db)
	if err != nil {
		return 0, fmt.Errorf("listing jobs: %w", err)
	}

	executed := 0
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		due, err := isJobDue(db, job, now)
		if err != nil {
			return executed, fmt.Errorf("checking job %q: %w", job.Name, err)
		}
		if !due {
			continue
		}
		if err := runScheduledJob(db, &job); err != nil {
			return executed, err
		}
		executed++
	}

	return executed, nil
}

func runScheduledJob(db *sql.DB, job *store.Job) error {
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
		base = latestRun.StartedAt
	}

	nextRun := schedule.Next(base)
	return !nextRun.After(now), nil
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
