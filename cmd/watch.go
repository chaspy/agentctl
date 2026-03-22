package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var (
	watchAgent    string
	watchHours    int
	watchTimeout  int
	watchInterval int
)

var watchCmd = &cobra.Command{
	Use:   "watch <project-name>",
	Short: "Watch a session until a new assistant message appears",
	Long:  "Polls the session log for the given project until the latest assistant message changes, then prints the new message and exits.",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatch,
}

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.Flags().StringVar(&watchAgent, "agent", "all", "Filter by agent: all, claude, codex")
	watchCmd.Flags().IntVar(&watchHours, "hours", 24, "Search sessions active within the last N hours")
	watchCmd.Flags().IntVar(&watchTimeout, "timeout", 600, "Timeout in seconds (default 10 minutes)")
	watchCmd.Flags().IntVar(&watchInterval, "interval", 5, "Poll interval in seconds")
}

func runWatch(cmd *cobra.Command, args []string) error {
	query := args[0]

	matched, err := findWatchSessions(query)
	if err != nil {
		return err
	}

	// Record baseline: file sizes and current last-message checksums
	type baseline struct {
		session  provider.SessionInfo
		fileSize int64
	}
	baselines := make([]baseline, 0, len(matched))
	for _, s := range matched {
		size, err := fileSize(s.FilePath)
		if err != nil {
			continue
		}
		baselines = append(baselines, baseline{session: s, fileSize: size})
	}

	if len(baselines) == 0 {
		return fmt.Errorf("no session files found for %q", query)
	}

	fmt.Fprintf(os.Stderr, "Watching %d session(s) matching %q (timeout %ds, interval %ds)...\n",
		len(baselines), query, watchTimeout, watchInterval)

	timeout := time.Duration(watchTimeout) * time.Second
	interval := time.Duration(watchInterval) * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		for _, b := range baselines {
			currentSize, err := fileSize(b.session.FilePath)
			if err != nil || currentSize <= b.fileSize {
				continue
			}

			// File grew — check if there's a new assistant message
			raw := &session.SessionInfo{FilePath: b.session.FilePath}
			msg := session.LastAssistantMessage(raw)
			if msg == "" {
				continue
			}

			_ = session.EnrichSession(raw)
			if raw.LastRole != "assistant" {
				// Still generating (last entry is user/tool), keep waiting
				continue
			}

			fmt.Printf("[%s] %s (%s)\n\n%s\n",
				b.session.Agent, b.session.Repository, b.session.GitBranch, msg)
			return nil
		}
	}

	return fmt.Errorf("timed out after %s waiting for new message", timeout)
}

func findWatchSessions(query string) ([]provider.SessionInfo, error) {
	agents, err := selectedAgents(watchAgent)
	if err != nil {
		return nil, err
	}

	maxAge := time.Duration(watchHours) * time.Hour
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

	q := strings.ToLower(query)
	var matched []provider.SessionInfo
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Repository), q) {
			matched = append(matched, s)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no session found matching %q", query)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ModTime.After(matched[j].ModTime)
	})

	return matched, nil
}
