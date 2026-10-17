package cmd

import (
	"fmt"
	"github.com/chaspy/agentctl/internal/mux"
	"os"

	"github.com/chaspy/agentctl/internal/process"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

const watchdogSessionName = "remote-control-watchdog"

// Shell script that runs claude remote-control in a loop with auto-restart
const watchdogScript = `while true; do echo "[$(date)] Starting claude remote-control..."; claude remote-control; EXIT_CODE=$?; echo "[$(date)] claude remote-control exited with code $EXIT_CODE. Restarting in 5s..."; sleep 5; done`

var watchdogCmd = &cobra.Command{
	Use:   "watchdog",
	Short: "Manage auto-recovery for claude remote-control sessions",
	Long: `Ensures a claude remote-control session is always running.
Creates a dedicated zellij session that runs claude remote-control in a loop,
automatically restarting it if it crashes or exits.

This is useful for maintaining remote access via mobile/tailscale when the
remote control session dies unexpectedly.`,
}

var watchdogStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the remote-control watchdog",
	Long: `Creates a zellij session that runs claude remote-control in a while loop.
If the remote-control process exits, it will be automatically restarted after 5 seconds.`,
	RunE: runWatchdogStart,
}

var watchdogStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the remote-control watchdog is running",
	RunE:  runWatchdogStatus,
}

var watchdogStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the remote-control watchdog",
	RunE:  runWatchdogStop,
}

func init() {
	rootCmd.AddCommand(watchdogCmd)
	watchdogCmd.AddCommand(watchdogStartCmd)
	watchdogCmd.AddCommand(watchdogStatusCmd)
	watchdogCmd.AddCommand(watchdogStopCmd)
}

func runWatchdogStart(cmd *cobra.Command, args []string) error {
	// Check if watchdog session already exists
	if isWatchdogAlive() {
		fmt.Printf("Watchdog session %q is already running\n", watchdogSessionName)
		printRemoteControlStatus()
		return nil
	}

	// Clean up exited sessions
	cleanupExitedMuxSession(watchdogSessionName)

	backend, err := resolveMuxBackend()
	if err != nil {
		return err
	}

	// Create a new mux session running the restart loop
	fmt.Printf("Starting watchdog session %q...\n", watchdogSessionName)
	if _, err := backend.Spawn(mux.SpawnOptions{
		Name:    watchdogSessionName,
		CWD:     os.TempDir(),
		Command: watchdogScript,
	}); err != nil {
		return err
	}

	fmt.Printf("Watchdog started in session %q\n", watchdogSessionName)
	fmt.Println("claude remote-control will auto-restart on exit")

	// Log to database
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  watchdogSessionName,
			ActionType: "watchdog-start",
			Content:    "Started remote-control watchdog",
		})
	}

	return nil
}

func runWatchdogStatus(cmd *cobra.Command, args []string) error {
	// Check mux session
	if isWatchdogAlive() {
		fmt.Printf("Watchdog session %q: running\n", watchdogSessionName)
	} else {
		fmt.Printf("Watchdog session %q: not running\n", watchdogSessionName)
		fmt.Println("Start it with: go run . watchdog start")
	}

	// Check remote-control processes
	printRemoteControlStatus()

	return nil
}

func runWatchdogStop(cmd *cobra.Command, args []string) error {
	if !isWatchdogAlive() {
		fmt.Printf("Watchdog session %q is not running\n", watchdogSessionName)
		return nil
	}

	// Kill the mux session
	backend, err := resolveMuxBackend()
	if err != nil {
		return err
	}
	if err := backend.Kill(watchdogSessionName); err != nil {
		return fmt.Errorf("failed to stop watchdog: %w", err)
	}

	fmt.Printf("Watchdog session %q stopped\n", watchdogSessionName)

	// Log to database
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  watchdogSessionName,
			ActionType: "watchdog-stop",
			Content:    "Stopped remote-control watchdog",
		})
	}

	return nil
}

func isWatchdogAlive() bool {
	return isMuxSessionRunning(watchdogSessionName)
}

func printRemoteControlStatus() {
	procs, err := process.FindRemoteControlProcesses()
	if err != nil {
		fmt.Printf("Remote-control processes: error (%v)\n", err)
		return
	}
	if len(procs) == 0 {
		fmt.Println("Remote-control processes: none")
	} else {
		fmt.Printf("Remote-control processes: %d running\n", len(procs))
		for _, p := range procs {
			fmt.Printf("  PID %d: %s\n", p.PID, p.Command)
		}
	}
}
