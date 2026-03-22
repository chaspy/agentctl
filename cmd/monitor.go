package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var (
	monitorInterval int
	monitorTarget   string
	monitorAgent    string
	monitorHours    int
	monitorExclude  string
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Watch all sessions and notify this zellij session on changes",
	Long: `Runs as a long-lived process that polls all sessions for new assistant messages.
When a change is detected, sends a notification message to the specified zellij session
(typically the agentctl session itself) so the agent can react.`,
	RunE: runMonitor,
}

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.Flags().IntVar(&monitorInterval, "interval", 30, "Poll interval in seconds")
	monitorCmd.Flags().StringVar(&monitorTarget, "target", "", "Zellij session name to notify (required)")
	monitorCmd.Flags().StringVar(&monitorAgent, "agent", "all", "Filter by agent: all, claude, codex")
	monitorCmd.Flags().IntVar(&monitorHours, "hours", 24, "Search sessions active within the last N hours")
	monitorCmd.Flags().StringVar(&monitorExclude, "exclude", "", "Exclude sessions matching this project name (e.g. agentctl)")
	_ = monitorCmd.MarkFlagRequired("target")
}

type sessionSnapshot struct {
	fileSize int64
	lastRole string
}

func runMonitor(cmd *cobra.Command, args []string) error {
	adapter, err := mux.Resolve("zellij")
	if err != nil {
		return fmt.Errorf("monitor requires zellij: %w", err)
	}

	// Verify target session exists
	if _, err := adapter.ResolveSession(monitorTarget); err != nil {
		return fmt.Errorf("target session %q not found: %w", monitorTarget, err)
	}

	interval := time.Duration(monitorInterval) * time.Second
	snapshots := make(map[string]sessionSnapshot)

	fmt.Fprintf(os.Stderr, "monitor: watching sessions every %ds, notifying %q, excluding %q\n",
		monitorInterval, monitorTarget, monitorExclude)

	// Take initial snapshot
	sessions := scanMonitorSessions()
	for _, s := range sessions {
		size, _ := fileSize(s.FilePath)
		raw := &session.SessionInfo{FilePath: s.FilePath}
		_ = session.EnrichSession(raw)
		snapshots[s.FilePath] = sessionSnapshot{
			fileSize: size,
			lastRole: raw.LastRole,
		}
	}
	fmt.Fprintf(os.Stderr, "monitor: baseline taken for %d sessions\n", len(snapshots))

	for {
		time.Sleep(interval)

		sessions = scanMonitorSessions()
		var changed []string

		for _, s := range sessions {
			prev, known := snapshots[s.FilePath]
			currentSize, err := fileSize(s.FilePath)
			if err != nil {
				continue
			}

			if !known {
				snapshots[s.FilePath] = sessionSnapshot{fileSize: currentSize, lastRole: ""}
				continue
			}

			if currentSize <= prev.fileSize {
				continue
			}

			raw := &session.SessionInfo{FilePath: s.FilePath}
			_ = session.EnrichSession(raw)

			// Notify when file grew AND last role is assistant
			// This catches: user→assistant transitions AND assistant→assistant (new response after previous one)
			if raw.LastRole == "assistant" {
				changed = append(changed, s.Repository)
			}

			snapshots[s.FilePath] = sessionSnapshot{
				fileSize: currentSize,
				lastRole: raw.LastRole,
			}
		}

		if len(changed) > 0 {
			// Keep notification ASCII-safe and short to avoid zellij UTF-8 issues
			notification := sanitizeForZellij(
				fmt.Sprintf("sessions responded: %s. check and report.", strings.Join(changed, ", ")))
			fmt.Fprintf(os.Stderr, "monitor: [%s] notifying: %s\n",
				time.Now().Format("15:04:05"), notification)

			if err := adapter.SendKeys(monitorTarget, notification); err != nil {
				fmt.Fprintf(os.Stderr, "monitor: failed to notify: %v\n", err)
			}
		}
	}
}

func scanMonitorSessions() []provider.SessionInfo {
	agents, _ := selectedAgents(monitorAgent)
	maxAge := time.Duration(monitorHours) * time.Hour

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

	// Filter out excluded projects
	if monitorExclude != "" {
		excl := strings.ToLower(monitorExclude)
		filtered := sessions[:0]
		for _, s := range sessions {
			if !strings.Contains(strings.ToLower(s.Repository), excl) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	return sessions
}

// sanitizeForZellij removes non-ASCII characters and newlines to avoid
// zellij write-chars UTF-8 encoding issues.
func sanitizeForZellij(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' || r == '\r' {
			b.WriteByte(' ')
		} else if r < 128 {
			b.WriteByte(byte(r))
		} else {
			// Skip non-ASCII to avoid zellij issues
		}
		i += size
	}
	return b.String()
}
