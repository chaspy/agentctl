package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	jobAddName        string
	jobAddSchedule    string
	jobAddAction      string
	jobAddRepo        string
	jobAddSession     string
	jobAddBranch      string
	jobAddAgent       string
	jobAddInstruction string
	jobAddCwd         string
	jobAddTimeout     int
	jobLogsLimit      int
)

var (
	jobSpawnExecutor   = executeJobSpawn
	jobSendExecutor    = executeJobSend
	jobCommandExecutor = executeJobCommand
	nowFunc            = time.Now
)

const jobTestModeEnv = "AGENTCTL_JOB_TEST_MODE"

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage scheduled jobs",
}

var jobAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a scheduled job",
	RunE:  runJobAdd,
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs",
	RunE:  runJobList,
}

var jobDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a job",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobDelete,
}

var jobRunCmd = &cobra.Command{
	Use:   "run <name|id>",
	Short: "Run a job immediately",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobRun,
}

var jobLogsCmd = &cobra.Command{
	Use:   "logs <name|id>",
	Short: "Show recent job execution logs",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobLogs,
}

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobAddCmd, jobListCmd, jobDeleteCmd, jobRunCmd, jobLogsCmd)

	jobAddCmd.Flags().StringVar(&jobAddName, "name", "", "Job name")
	jobAddCmd.Flags().StringVar(&jobAddSchedule, "schedule", "", "Cron schedule")
	jobAddCmd.Flags().StringVar(&jobAddAction, "action", "", "Job action: spawn, send, or command")
	jobAddCmd.Flags().StringVar(&jobAddRepo, "repo", "", "Target repo for spawn jobs")
	jobAddCmd.Flags().StringVar(&jobAddSession, "session", "", "Target session for send jobs")
	jobAddCmd.Flags().StringVar(&jobAddBranch, "branch", "", "Branch for spawn jobs")
	jobAddCmd.Flags().StringVar(&jobAddAgent, "agent", "", "Agent for spawn jobs")
	jobAddCmd.Flags().StringVar(&jobAddInstruction, "instruction", "", "Instruction to execute")
	jobAddCmd.Flags().StringVar(&jobAddCwd, "cwd", "", "Working directory for command jobs")
	jobAddCmd.Flags().IntVar(&jobAddTimeout, "timeout", 600, "Timeout in seconds for command jobs")
	jobAddCmd.MarkFlagRequired("name")
	jobAddCmd.MarkFlagRequired("schedule")
	jobAddCmd.MarkFlagRequired("action")
	jobAddCmd.MarkFlagRequired("instruction")

	jobLogsCmd.Flags().IntVar(&jobLogsLimit, "limit", 10, "Number of recent runs to show")
}

func runJobAdd(cmd *cobra.Command, args []string) error {
	job := &store.Job{
		Name:        strings.TrimSpace(jobAddName),
		Schedule:    strings.TrimSpace(jobAddSchedule),
		Action:      strings.TrimSpace(jobAddAction),
		Repo:        strings.TrimSpace(jobAddRepo),
		Session:     strings.TrimSpace(jobAddSession),
		Branch:      strings.TrimSpace(jobAddBranch),
		Agent:       strings.TrimSpace(jobAddAgent),
		Instruction: strings.TrimSpace(jobAddInstruction),
		Cwd:         strings.TrimSpace(jobAddCwd),
		Timeout:     jobAddTimeout,
		Enabled:     true,
	}
	if err := validateJob(job); err != nil {
		return err
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if err := store.CreateJob(db, job); err != nil {
		return fmt.Errorf("creating job: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added job %q (id=%d)\n", job.Name, job.ID)
	return nil
}

func runJobList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	jobs, err := store.ListJobs(db)
	if err != nil {
		return fmt.Errorf("listing jobs: %w", err)
	}
	if len(jobs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No jobs found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSCHEDULE\tACTION\tTARGET\tENABLED")
	for _, job := range jobs {
		target := job.Repo
		switch job.Action {
		case "send":
			target = job.Session
		case "command":
			target = job.Instruction
		}
		enabled := "no"
		if job.Enabled {
			enabled = "yes"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", job.ID, job.Name, job.Schedule, job.Action, target, enabled)
	}
	return w.Flush()
}

func runJobDelete(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	job, err := findJob(db, args[0])
	if err != nil {
		return err
	}
	if err := store.DeleteJob(db, job.ID); err != nil {
		return fmt.Errorf("deleting job: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted job %q (id=%d)\n", job.Name, job.ID)
	return nil
}

func runJobRun(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	job, err := findJob(db, args[0])
	if err != nil {
		return err
	}

	output, err := runStoredJob(db, job)
	if output != "" {
		fmt.Fprint(cmd.OutOrStdout(), output)
		if !strings.HasSuffix(output, "\n") {
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Ran job %q successfully\n", job.Name)
	return nil
}

func runJobLogs(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	job, err := findJob(db, args[0])
	if err != nil {
		return err
	}

	runs, err := store.ListJobRuns(db, job.ID, jobLogsLimit)
	if err != nil {
		return fmt.Errorf("listing job logs: %w", err)
	}
	if len(runs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No logs for job %q.\n", job.Name)
		return nil
	}

	for _, run := range runs {
		finishedAt := "-"
		if run.FinishedAt != nil {
			finishedAt = run.FinishedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[run:%d] status=%s started_at=%s finished_at=%s\n",
			run.ID, run.Status, run.StartedAt.Format(time.RFC3339), finishedAt)
		if run.Output != "" {
			fmt.Fprintln(cmd.OutOrStdout(), run.Output)
		}
	}
	return nil
}

func runStoredJob(db *sql.DB, job *store.Job) (string, error) {
	startedAt := nowFunc().UTC()
	run := &store.JobRun{
		JobID:     job.ID,
		StartedAt: startedAt,
		Status:    "running",
	}
	if err := store.CreateJobRun(db, run); err != nil {
		return "", fmt.Errorf("creating job run: %w", err)
	}

	output, runErr := executeStoredJob(job)
	finishedAt := nowFunc().UTC()
	run.FinishedAt = &finishedAt
	run.Output = output
	if runErr != nil {
		run.Status = "failed"
	} else {
		run.Status = "success"
	}
	if err := store.UpdateJobRun(db, run); err != nil {
		return output, fmt.Errorf("updating job run: %w", err)
	}
	if runErr != nil {
		return output, fmt.Errorf("running job %q: %w", job.Name, runErr)
	}
	return output, nil
}

func executeStoredJob(job *store.Job) (string, error) {
	switch job.Action {
	case "spawn":
		return jobSpawnExecutor(job)
	case "send":
		return jobSendExecutor(job)
	case "command":
		return jobCommandExecutor(job)
	default:
		return "", fmt.Errorf("unsupported job action %q", job.Action)
	}
}

func executeJobSpawn(job *store.Job) (string, error) {
	if os.Getenv(jobTestModeEnv) != "" {
		return fmt.Sprintf("[job-test-mode] spawn repo=%s branch=%s agent=%s instruction=%s",
			job.Repo, job.Branch, job.Agent, job.Instruction), nil
	}

	origBranch := spawnBranch
	origName := spawnName
	origMessage := spawnMessage
	origSummary := spawnSummary
	origLoop := spawnLoop
	origAgent := spawnAgent
	origTaskType := spawnTaskType
	defer func() {
		spawnBranch = origBranch
		spawnName = origName
		spawnMessage = origMessage
		spawnSummary = origSummary
		spawnLoop = origLoop
		spawnAgent = origAgent
		spawnTaskType = origTaskType
	}()

	spawnBranch = job.Branch
	spawnName = ""
	spawnMessage = job.Instruction
	spawnSummary = ""
	spawnLoop = false
	spawnAgent = job.Agent
	spawnTaskType = taskTypeGeneral

	return captureStdout(func() error {
		return runSpawn(spawnCmd, []string{job.Repo})
	})
}

func executeJobSend(job *store.Job) (string, error) {
	if os.Getenv(jobTestModeEnv) != "" {
		return fmt.Sprintf("[job-test-mode] send session=%s instruction=%s",
			job.Session, job.Instruction), nil
	}

	origNoWait := sendNoWait
	defer func() {
		sendNoWait = origNoWait
	}()
	sendNoWait = true

	return captureStdout(func() error {
		return runSend(sendCmd, []string{job.Session, job.Instruction})
	})
}

func executeJobCommand(job *store.Job) (string, error) {
	if os.Getenv(jobTestModeEnv) != "" {
		return fmt.Sprintf("[job-test-mode] command=%s cwd=%s timeout=%d",
			job.Instruction, job.Cwd, job.Timeout), nil
	}

	timeout := time.Duration(job.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", job.Instruction)
	if job.Cwd != "" {
		cmd.Dir = job.Cwd
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("command failed: %w", err)
	}
	return buf.String(), nil
}

func captureStdout(fn func() error) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer r.Close()

	origStdout := os.Stdout
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = origStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		return "", readErr
	}
	return string(out), runErr
}

func findJob(db *sql.DB, ref string) (*store.Job, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		job, getErr := store.GetJobByID(db, id)
		if getErr == nil {
			return job, nil
		}
		if getErr != sql.ErrNoRows {
			return nil, fmt.Errorf("getting job by id: %w", getErr)
		}
	}

	job, err := store.GetJobByName(db, ref)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job %q not found", ref)
		}
		return nil, fmt.Errorf("getting job by name: %w", err)
	}
	return job, nil
}

func validateJob(job *store.Job) error {
	if job.Name == "" {
		return fmt.Errorf("--name is required")
	}
	if job.Schedule == "" {
		return fmt.Errorf("--schedule is required")
	}
	if job.Instruction == "" {
		return fmt.Errorf("--instruction is required")
	}

	switch job.Action {
	case "spawn":
		if job.Repo == "" {
			return fmt.Errorf("--repo is required for action=spawn")
		}
	case "send":
		if job.Session == "" {
			return fmt.Errorf("--session is required for action=send")
		}
	case "command":
		// instruction is already required globally
	default:
		return fmt.Errorf("--action must be spawn, send, or command")
	}

	return nil
}
