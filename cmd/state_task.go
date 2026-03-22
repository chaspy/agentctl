package cmd

import (
	"fmt"
	"os"
	"strconv"
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

var taskSession string

func init() {
	stateCmd.AddCommand(stateTaskCmd)
	stateTaskCmd.AddCommand(taskListCmd)
	stateTaskCmd.AddCommand(taskAddCmd)
	stateTaskCmd.AddCommand(taskCompleteCmd)
	stateTaskCmd.AddCommand(taskCancelCmd)

	taskListCmd.Flags().StringVar(&taskSession, "session", "", "Filter by session ID")
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
	fmt.Fprintln(w, "ID\tSESSION\tSTATUS\tDESCRIPTION\tRESULT")
	for _, t := range tasks {
		result := t.Result
		if result == "" {
			result = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", t.ID, t.SessionID, t.Status, t.Description, result)
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
