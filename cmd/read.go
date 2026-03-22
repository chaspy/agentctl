package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var (
	readAgent string
	readHours int
)

var readCmd = &cobra.Command{
	Use:   "read <project-name>",
	Short: "Show the last assistant response from a session",
	Long:  "Finds the most recent session matching the project name and prints the last assistant message.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRead,
}

func init() {
	rootCmd.AddCommand(readCmd)
	readCmd.Flags().StringVar(&readAgent, "agent", "all", "Filter by agent: all, claude, codex")
	readCmd.Flags().IntVar(&readHours, "hours", 24, "Search sessions active within the last N hours")
}

func runRead(cmd *cobra.Command, args []string) error {
	query := args[0]

	agents, err := selectedAgents(readAgent)
	if err != nil {
		return err
	}

	maxAge := time.Duration(readHours) * time.Hour
	var sessions []provider.SessionInfo
	for _, agent := range agents {
		switch agent {
		case provider.AgentClaude:
			items, err := provider.ScanClaudeSessions(maxAge)
			if err != nil {
				continue
			}
			sessions = append(sessions, items...)
		case provider.AgentCodex:
			items, err := provider.ScanCodexSessions(maxAge)
			if err != nil {
				continue
			}
			sessions = append(sessions, items...)
		}
	}

	// Filter by repository name (substring match)
	var matched []provider.SessionInfo
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Repository), strings.ToLower(query)) {
			matched = append(matched, s)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("no session found matching %q", query)
	}

	// Pick the most recent one
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ModTime.After(matched[j].ModTime)
	})
	target := matched[0]

	// For Claude sessions, read the full last assistant message from JSONL
	if target.Agent == provider.AgentClaude && target.FilePath != "" {
		raw := &session.SessionInfo{FilePath: target.FilePath}
		if msg := session.LastAssistantMessage(raw); msg != "" {
			fmt.Printf("[%s] %s (%s) — %s\n\n%s\n",
				target.Agent, target.Repository, target.GitBranch, formatAge(time.Since(target.ModTime)), msg)
			return nil
		}
	}

	// Fallback to the truncated message from list scan
	if target.LastMessage != "" {
		fmt.Printf("[%s] %s (%s) — %s\n\n%s\n",
			target.Agent, target.Repository, target.GitBranch, formatAge(time.Since(target.ModTime)), target.LastMessage)
		return nil
	}

	return fmt.Errorf("no assistant message found in session %q", target.Repository)
}
