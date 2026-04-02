package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks assigned to sessions",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runTaskList,
}

var taskAddCmd = &cobra.Command{
	Use:   "add <session-id> <description>",
	Short: "Add a task to a session",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskAdd,
}

var taskCompleteCmd = &cobra.Command{
	Use:   "complete <task-id> [result]",
	Short: "Mark a task as completed",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runTaskComplete,
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Cancel a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCancel,
}

var taskDepCmd = &cobra.Command{
	Use:   "dep <task-id> <blocked-by-id>",
	Short: "Add a dependency (task-id is blocked by blocked-by-id)",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskDep,
}

var taskUndepCmd = &cobra.Command{
	Use:   "undep <task-id> <blocked-by-id>",
	Short: "Remove a dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskUndep,
}

var taskReadyCmd = &cobra.Command{
	Use:   "ready",
	Short: "List tasks that are ready to start (no incomplete blockers)",
	RunE:  runTaskReady,
}

var (
	taskSession string
	taskOwner   string
)

func init() {
	stateCmd.AddCommand(stateTaskCmd)
	stateTaskCmd.AddCommand(taskListCmd)
	stateTaskCmd.AddCommand(taskAddCmd)
	stateTaskCmd.AddCommand(taskCompleteCmd)
	stateTaskCmd.AddCommand(taskCancelCmd)
	stateTaskCmd.AddCommand(taskDepCmd)
	stateTaskCmd.AddCommand(taskUndepCmd)
	stateTaskCmd.AddCommand(taskReadyCmd)

	taskListCmd.Flags().StringVar(&taskSession, "session", "", "Filter by session ID")
	taskAddCmd.Flags().StringVar(&taskOwner, "owner", "", "Task owner (e.g. session name or agent)")
}

func runTaskList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	var tasks []store.Task
	if taskSession != "" {
		tasks, err = store.GetTasksForSession(db, taskSession)
	} else {
		tasks, err = store.GetActiveTasks(db)
	}
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSESSION\tSTATUS\tOWNER\tBLOCKED_BY\tBLOCKS\tDESCRIPTION\tRESULT")
	for _, t := range tasks {
		result := t.Result
		if result == "" {
			result = "-"
		}
		owner := t.Owner
		if owner == "" {
			owner = "-"
		}
		blockedBy, _ := store.GetBlockedBy(db, t.ID)
		blocks, _ := store.GetBlocks(db, t.ID)
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.SessionID, t.Status, owner,
			formatIDs(blockedBy), formatIDs(blocks),
			t.Description, result)
	}
	return w.Flush()
}

func runTaskAdd(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	t := &store.Task{
		SessionID:   args[0],
		Description: args[1],
		Status:      "pending",
		Owner:       taskOwner,
	}
	if err := store.CreateTask(db, t); err != nil {
		return fmt.Errorf("creating task: %w", err)
	}
	fmt.Printf("Created task #%d for session %s\n", t.ID, t.SessionID)
	return nil
}

func runTaskComplete(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID %q: %w", args[0], err)
	}

	result := ""
	if len(args) > 1 {
		result = args[1]
	}
	if err := store.UpdateTaskStatus(db, id, "completed", result); err != nil {
		return fmt.Errorf("completing task: %w", err)
	}
	fmt.Printf("Task #%d marked as completed\n", id)
	return nil
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID %q: %w", args[0], err)
	}

	if err := store.UpdateTaskStatus(db, id, "cancelled", ""); err != nil {
		return fmt.Errorf("cancelling task: %w", err)
	}
	fmt.Printf("Task #%d cancelled\n", id)
	return nil
}

func runTaskDep(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID %q: %w", args[0], err)
	}
	depID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid dependency ID %q: %w", args[1], err)
	}

	if err := store.AddTaskDependency(db, taskID, depID); err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	fmt.Printf("Task #%d is now blocked by task #%d\n", taskID, depID)
	return nil
}

func runTaskUndep(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID %q: %w", args[0], err)
	}
	depID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid dependency ID %q: %w", args[1], err)
	}

	if err := store.RemoveTaskDependency(db, taskID, depID); err != nil {
		return fmt.Errorf("removing dependency: %w", err)
	}
	fmt.Printf("Removed dependency: task #%d no longer blocked by task #%d\n", taskID, depID)
	return nil
}

func runTaskReady(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	tasks, err := store.GetReadyTasks(db)
	if err != nil {
		return fmt.Errorf("listing ready tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No ready tasks found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSESSION\tSTATUS\tOWNER\tDESCRIPTION")
	for _, t := range tasks {
		owner := t.Owner
		if owner == "" {
			owner = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", t.ID, t.SessionID, t.Status, owner, t.Description)
	}
	return w.Flush()
}

func formatIDs(ids []int64) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
