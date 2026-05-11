package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	runtimeReconcileApply            bool
	runtimeReconcileJSON             bool
	runtimeReconcileTerminateStopped bool
	runtimeReconcileMinAge           time.Duration
	runtimeReconcileLockTTL          time.Duration

	runtimeReconcileNow                = time.Now
	runtimeReconcileGetpgid            = syscall.Getpgid
	runtimeReconcileProcessGroupExists = processGroupExists
	runtimeReconcileSignalProcessGroup = signalProcessGroup
	runtimeReconcileSleep              = time.Sleep
)

type runtimeReconcileOptions struct {
	Apply            bool
	TerminateStopped bool
	MinAge           time.Duration
	Now              time.Time
}

type runtimeReconcileRow struct {
	ID            string `json:"id"`
	ZellijSession string `json:"zellij_session"`
	Repository    string `json:"repository"`
	GitBranch     string `json:"git_branch"`
	Status        string `json:"status"`
	DesiredState  string `json:"desired_state"`
	RuntimeStatus string `json:"runtime_status"`
	RuntimePID    int    `json:"runtime_pid"`
	RuntimePGID   int    `json:"runtime_pgid"`
	RuntimeAgeSec int64  `json:"runtime_age_sec"`
	ObservedState string `json:"observed_state"`
	Action        string `json:"action"`
	Applied       bool   `json:"applied"`
	Reason        string `json:"reason"`
	Error         string `json:"error,omitempty"`
}

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Inspect and reconcile recorded runtime process ownership",
}

var runtimeReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Reconcile recorded runtime PID/PGID ownership",
	Long: `Reconcile recorded runtime PID/PGID ownership.

By default this command is read-only. Use --apply to clear stale ownership
records. Use --terminate-stopped with --apply to terminate process groups only
for sessions that are already stopped/gone/dead in persisted state.`,
	RunE: runRuntimeReconcile,
}

func init() {
	rootCmd.AddCommand(runtimeCmd)
	runtimeCmd.AddCommand(runtimeReconcileCmd)
	runtimeReconcileCmd.Flags().BoolVar(&runtimeReconcileApply, "apply", false, "Apply safe DB cleanup actions")
	runtimeReconcileCmd.Flags().BoolVar(&runtimeReconcileJSON, "json", false, "Output machine-readable JSON")
	runtimeReconcileCmd.Flags().BoolVar(&runtimeReconcileTerminateStopped, "terminate-stopped", false, "With --apply, terminate recorded process groups for stopped/gone/dead sessions")
	runtimeReconcileCmd.Flags().DurationVar(&runtimeReconcileMinAge, "min-age", 2*time.Minute, "Ignore runtime ownership younger than this duration")
	runtimeReconcileCmd.Flags().DurationVar(&runtimeReconcileLockTTL, "lock-ttl", 10*time.Minute, "Runtime reconcile lock TTL")
}

func runRuntimeReconcile(cmd *cobra.Command, args []string) error {
	release, acquired, err := acquireRuntimeReconcileLock(runtimeReconcileLockTTL)
	if err != nil {
		return fmt.Errorf("acquiring runtime reconcile lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("runtime reconcile is already running")
	}
	defer release()

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	rows, err := reconcileRuntimeOwnership(db, runtimeReconcileOptions{
		Apply:            runtimeReconcileApply,
		TerminateStopped: runtimeReconcileTerminateStopped,
		MinAge:           runtimeReconcileMinAge,
		Now:              runtimeReconcileNow(),
	})
	if err != nil {
		return err
	}

	if runtimeReconcileJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No recorded runtime ownership found")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "SESSION\tPID\tPGID\tSTATE\tACTION\tAPPLIED\tREASON\n")
	for _, row := range rows {
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"%s\t%d\t%d\t%s\t%s\t%v\t%s\n",
			firstNonEmpty(row.ZellijSession, row.ID),
			row.RuntimePID,
			row.RuntimePGID,
			row.ObservedState,
			row.Action,
			row.Applied,
			row.Reason,
		)
	}
	return nil
}

func reconcileRuntimeOwnership(db *sql.DB, opts runtimeReconcileOptions) ([]runtimeReconcileRow, error) {
	if opts.Now.IsZero() {
		opts.Now = runtimeReconcileNow()
	}
	sessions, err := store.ListSessions(db)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	rows := make([]runtimeReconcileRow, 0)
	for _, session := range sessions {
		if session.RuntimePID <= 0 && session.RuntimePGID <= 0 {
			continue
		}
		row := reconcileRuntimeSession(db, session, opts)
		rows = append(rows, row)
	}
	return rows, nil
}

func reconcileRuntimeSession(db *sql.DB, session store.Session, opts runtimeReconcileOptions) runtimeReconcileRow {
	row := runtimeReconcileRow{
		ID:            session.ID,
		ZellijSession: session.ZellijSession,
		Repository:    session.Repository,
		GitBranch:     session.GitBranch,
		Status:        session.Status,
		DesiredState:  session.DesiredState,
		RuntimeStatus: session.RuntimeStatus,
		RuntimePID:    session.RuntimePID,
		RuntimePGID:   session.RuntimePGID,
		Action:        "keep",
		Reason:        "recorded runtime ownership is consistent",
	}
	if !session.RuntimeStartedAt.IsZero() {
		row.RuntimeAgeSec = int64(opts.Now.Sub(session.RuntimeStartedAt).Seconds())
	}

	if session.RuntimePGID <= 1 {
		row.ObservedState = "invalid_pgid"
		row.Action = "clear_ownership"
		row.Reason = "recorded process group is invalid"
		clearRuntimeOwnershipIfApplied(db, session, opts.Apply, &row)
		return row
	}
	if current := syscall.Getpgrp(); current == session.RuntimePGID {
		row.ObservedState = "current_pgid"
		row.Action = "skip"
		row.Reason = "refusing to touch current process group"
		return row
	}
	if opts.MinAge > 0 && !session.RuntimeStartedAt.IsZero() && opts.Now.Sub(session.RuntimeStartedAt) < opts.MinAge {
		row.ObservedState = "young"
		row.Action = "skip"
		row.Reason = fmt.Sprintf("runtime ownership is younger than %s", opts.MinAge)
		return row
	}

	pidPGID, pidErr := runtimeReconcileGetpgid(session.RuntimePID)
	groupAlive := runtimeReconcileProcessGroupExists(session.RuntimePGID)
	terminal := isTerminalRuntimeSession(session)

	switch {
	case pidErr == nil && pidPGID == session.RuntimePGID:
		row.ObservedState = "running"
		if terminal {
			maybeTerminateStoppedRuntimeGroup(db, session, opts, &row)
		}
	case pidErr == nil && pidPGID != session.RuntimePGID:
		row.ObservedState = "pid_reused"
		row.Action = "clear_ownership"
		row.Reason = fmt.Sprintf("recorded pid now belongs to pgid %d", pidPGID)
		clearRuntimeOwnershipIfApplied(db, session, opts.Apply, &row)
	case isNoSuchProcess(pidErr) && groupAlive:
		row.ObservedState = "orphaned_group"
		if terminal {
			maybeTerminateStoppedRuntimeGroup(db, session, opts, &row)
		} else {
			row.Action = "inspect"
			row.Reason = "recorded pid is gone but process group still exists for a running session"
		}
	case isNoSuchProcess(pidErr) && !groupAlive:
		row.ObservedState = "gone"
		row.Action = "clear_ownership"
		row.Reason = "recorded pid and process group are gone"
		clearRuntimeOwnershipIfApplied(db, session, opts.Apply, &row)
	case pidErr != nil:
		row.ObservedState = "unknown"
		row.Action = "inspect"
		row.Reason = "could not inspect recorded pid"
		row.Error = pidErr.Error()
	default:
		row.ObservedState = "unknown"
		row.Action = "inspect"
		row.Reason = "runtime ownership requires manual inspection"
	}
	return row
}

func maybeTerminateStoppedRuntimeGroup(db *sql.DB, session store.Session, opts runtimeReconcileOptions, row *runtimeReconcileRow) {
	if !opts.TerminateStopped {
		row.Action = "would_terminate_group"
		row.Reason = "session is stopped/gone/dead; pass --apply --terminate-stopped to terminate recorded group"
		return
	}
	row.Action = "terminate_group"
	row.Reason = "session is stopped/gone/dead; terminating recorded process group"
	if !opts.Apply {
		row.Action = "would_terminate_group"
		return
	}
	if err := runtimeReconcileSignalProcessGroup(session.RuntimePGID, syscall.SIGTERM); err != nil {
		row.Error = err.Error()
		return
	}
	runtimeReconcileSleep(2 * time.Second)
	if err := runtimeReconcileSignalProcessGroup(session.RuntimePGID, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
		row.Error = err.Error()
		return
	}
	clearRuntimeOwnershipIfApplied(db, session, true, row)
}

func clearRuntimeOwnershipIfApplied(db *sql.DB, session store.Session, apply bool, row *runtimeReconcileRow) {
	if !apply {
		row.Action = "would_" + row.Action
		return
	}
	rows, err := store.ClearSessionRuntimeProcessByZellijSession(db, session.ZellijSession)
	if err != nil {
		row.Error = err.Error()
		return
	}
	row.Applied = rows > 0
}

func processGroupExists(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}

func isTerminalRuntimeSession(session store.Session) bool {
	status := strings.ToLower(strings.TrimSpace(session.Status))
	runtimeStatus := strings.ToLower(strings.TrimSpace(session.RuntimeStatus))
	lifecycle := strings.ToLower(strings.TrimSpace(session.LifecycleState))
	return !session.WantsRunning() ||
		status == "dead" ||
		runtimeStatus == "gone" ||
		lifecycle == "stopped"
}

func acquireRuntimeReconcileLock(ttl time.Duration) (func(), bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, err
	}
	dir := filepath.Join(home, ".agentctl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}
	path := filepath.Join(dir, "runtime-reconcile.lock")
	now := runtimeReconcileNow()
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(file, "pid=%d\nstarted_at=%s\n", os.Getpid(), now.Format(time.RFC3339))
			_ = file.Close()
			return func() { _ = os.Remove(path) }, true, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, false, err
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return nil, false, statErr
		}
		if ttl > 0 && now.Sub(info.ModTime()) > ttl {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, false, err
			}
			continue
		}
		return nil, false, nil
	}
}
