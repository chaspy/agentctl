package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
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
	Short: "Resume a dead Claude session in a new mux session",
	Long: `Finds a dead Claude session by project name or session UUID,
creates a new mux session (zellij/herdr), and starts claude --resume <session-id> in it.

Examples:
  agentctl resume my-session
  agentctl resume 06ca580e-cc73-4a1a-8765-78a0f2a500ae`,
	Args: cobra.ExactArgs(1),
	RunE: runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
	resumeCmd.Flags().StringVar(&resumeName, "name", "", "Mux session name (auto-generated if not set)")
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

	// Determine mux session name
	sessionName := resumeName
	if sessionName == "" {
		// Use project name with dashes
		name := strings.ReplaceAll(target.Repository, "/", "-")
		sessionName = name + "-resumed"
	}

	backend, err := resolveMuxBackend()
	if err != nil {
		return err
	}
	if muxSessionExists(sessionName) {
		return fmt.Errorf("%s session %q already exists", backend.Name(), sessionName)
	}

	resumeCommand := fmt.Sprintf("claude --resume %s", target.SessionID)
	if _, err := backend.Spawn(mux.SpawnOptions{
		Name:    sessionName,
		CWD:     target.CWD,
		Command: resumeCommand,
		Agent:   string(provider.AgentClaude),
	}); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Resumed session %s in %s session %q\n", target.SessionID, backend.Name(), sessionName)

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
