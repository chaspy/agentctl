package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

const managerSessionName = "agent-manager"

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Manage the dedicated AI Manager session",
}

var managerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the dedicated AI Manager zellij session",
	Long: `Creates a dedicated zellij session for the AI Manager.
The AI Manager session runs persistently and manages all worker sessions.
State is persisted in SQLite, surviving remote control disconnects.`,
	RunE: runManagerStart,
}

var managerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the AI Manager session is running",
	RunE:  runManagerStatus,
}

func init() {
	rootCmd.AddCommand(managerCmd)
	managerCmd.AddCommand(managerStartCmd)
	managerCmd.AddCommand(managerStatusCmd)
}

func runManagerStart(cmd *cobra.Command, args []string) error {
	// Check if manager session already exists
	if isManagerAlive() {
		fmt.Printf("AI Manager session %q is already running\n", managerSessionName)
		return nil
	}

	// Check for EXITED sessions and clean them up
	out, _ := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, managerSessionName) && strings.Contains(line, "EXITED") {
			fmt.Printf("Cleaning up exited session %q...\n", managerSessionName)
			exec.Command("env", "-u", "ZELLIJ", "zellij", "delete-session", managerSessionName).Run()
		}
	}

	// Use the existing spawn logic but with fixed session name
	fmt.Printf("Starting AI Manager session %q...\n", managerSessionName)

	// Resolve the agentctl repo path
	repo, err := ResolveRepoPath("agentctl")
	if err != nil {
		return fmt.Errorf("could not find agentctl repo: %w", err)
	}

	// Set spawn parameters and reuse spawn logic
	spawnName = managerSessionName
	spawnBranch = ""
	spawnMessage = "/start"

	if err := runSpawn(cmd, []string{repo.ShortName}); err != nil {
		return err
	}

	// Record in database
	db, err := store.Open("")
	if err != nil {
		return nil // session started, DB recording is best-effort
	}
	defer db.Close()

	_ = store.SetState(db, "manager_session_name", managerSessionName)
	_ = store.SetState(db, "manager_started_at", fmt.Sprintf("%d", cmd.Context().Err()))
	_ = store.LogAction(db, &store.Action{
		ActionType: "spawn",
		Content:    fmt.Sprintf("Started AI Manager session %q", managerSessionName),
	})

	// Mark manager session with role='manager' in sessions table
	_ = store.SetSessionRole(db, managerSessionName, "manager")

	return nil
}

func runManagerStatus(cmd *cobra.Command, args []string) error {
	if isManagerAlive() {
		fmt.Printf("AI Manager session %q is running\n", managerSessionName)
	} else {
		fmt.Printf("AI Manager session %q is NOT running\n", managerSessionName)
		fmt.Println("Start it with: go run . manager start")
	}

	// Show DB state if available
	db, err := store.Open("")
	if err != nil {
		return nil
	}
	defer db.Close()

	if name, err := store.GetState(db, "manager_session_name"); err == nil && name != "" {
		fmt.Printf("  Registered session: %s\n", name)
	}
	if started, err := store.GetState(db, "manager_started_at"); err == nil && started != "" {
		fmt.Printf("  Started at: %s\n", started)
	}

	return nil
}

func isManagerAlive() bool {
	out, err := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == managerSessionName {
			return true
		}
	}
	return false
}
