package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/chaspy/agentctl/internal/process"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	resumeName string
)

var resumeCmd = &cobra.Command{
	Use:   "resume <project-or-session-id>",
	Short: "Resume a dead Claude session in a new zellij session",
	Long: `Finds a dead Claude session by project name or session UUID,
creates a new zellij session, and starts claude --resume <session-id> in it.

Examples:
  agentctl resume my-session
  agentctl resume 06ca580e-cc73-4a1a-8765-78a0f2a500ae`,
	Args: cobra.ExactArgs(1),
	RunE: runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
	resumeCmd.Flags().StringVar(&resumeName, "name", "", "Zellij session name (auto-generated if not set)")
}

func runResume(cmd *cobra.Command, args []string) error {
	query := args[0]

	// Scan all Claude sessions (use a wide window to find dead sessions)
	sessions, err := provider.ScanClaudeSessions(7 * 24 * time.Hour)
	if err != nil {
		return fmt.Errorf("scanning sessions: %w", err)
	}

	// Find matching sessions
	claudeProcs, _ := process.FindClaudeProcesses()

	var candidates []provider.SessionInfo
	q := strings.ToLower(query)
	for _, s := range sessions {
		alive := process.IsAliveForCWD(claudeProcs, s.CWD)
		statusMsg := s.LastFullMessage
		if statusMsg == "" {
			statusMsg = s.LastMessage
		}
		status := session.DetectStatus(statusMsg, s.LastRole, alive, s.ErrorType, s.IsAPIError)

		// Match by session UUID or project name
		if s.SessionID == query || strings.Contains(strings.ToLower(s.Repository), q) {
			s.Status = status
			candidates = append(candidates, s)
		}
	}

	if len(candidates) == 0 {
		return fmt.Errorf("no session found matching %q", query)
	}

	// Sort by ModTime descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ModTime.After(candidates[j].ModTime)
	})

	// Prefer dead sessions, but allow resuming any
	var target *provider.SessionInfo
	for i := range candidates {
		if candidates[i].Status == session.StatusDead {
			target = &candidates[i]
			break
		}
	}
	if target == nil {
		// No dead session found — use the most recent one
		target = &candidates[0]
		fmt.Fprintf(os.Stderr, "No dead session found; resuming most recent session (status: %s)\n", target.Status)
	}

	// Check CWD exists
	if _, err := os.Stat(target.CWD); err != nil {
		return fmt.Errorf("session CWD %s does not exist (worktree may have been deleted)", target.CWD)
	}

	// Determine zellij session name
	sessionName := resumeName
	if sessionName == "" {
		// Use project name with dashes
		name := strings.ReplaceAll(target.Repository, "/", "-")
		sessionName = name + "-resumed"
	}

	// Check if session already exists
	existing, _ := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(existing)), "\n") {
		if strings.TrimSpace(line) == sessionName {
			return fmt.Errorf("zellij session %q already exists", sessionName)
		}
	}

	// Create a new zellij session
	bgCmd := exec.Command("script", "-q", "/dev/null",
		"env", "-u", "ZELLIJ", "-u", "CLAUDECODE",
		"zellij", "-s", sessionName)
	bgCmd.Dir = target.CWD
	bgCmd.Stdin = nil
	bgCmd.Stdout = nil
	bgCmd.Stderr = nil
	bgCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := bgCmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Creating zellij session %q in %s...\n", sessionName, target.CWD)
	if err := waitForSession(sessionName, 10*time.Second); err != nil {
		return err
	}

	// Dismiss any Zellij tip overlay
	dismissTip := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "27") // ESC
	_ = dismissTip.Run()
	time.Sleep(500 * time.Millisecond)

	// Start claude --resume <session-id>
	resumeCommand := fmt.Sprintf("claude --resume %s", target.SessionID)
	writeChars := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write-chars", resumeCommand)
	if out, err := writeChars.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send resume command: %w\n%s", err, string(out))
	}
	writeEnter := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "13")
	if out, err := writeEnter.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send enter: %w\n%s", err, string(out))
	}

	fmt.Fprintf(os.Stderr, "Resumed session %s in zellij session %q\n", target.SessionID, sessionName)

	// Log to database (fire-and-forget)
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  sessionName,
			ActionType: "resume",
			Content:    fmt.Sprintf("Resumed %s (session %s) in %s", target.Repository, target.SessionID, target.CWD),
		})
	}

	return nil
}
