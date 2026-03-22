package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	sendMux     string
	sendAgent   string
	sendHours   int
	sendTimeout int
	sendNoWait  bool
)

var sendCmd = &cobra.Command{
	Use:   "send <mux-session> <instruction>",
	Short: "Send an instruction to a Claude/Codex session and wait for a response",
	Long:  "Sends text to the specified mux session, then polls the session log until a new assistant message appears.",
	Args:  cobra.ExactArgs(2),
	RunE:  runSend,
}

func init() {
	rootCmd.AddCommand(sendCmd)
	sendCmd.Flags().StringVar(&sendMux, "mux", "auto", "Mux backend: auto, tmux, zellij")
	sendCmd.Flags().StringVar(&sendAgent, "agent", "all", "Filter by agent: all, claude, codex")
	sendCmd.Flags().IntVar(&sendHours, "hours", 24, "Search sessions active within the last N hours")
	sendCmd.Flags().IntVar(&sendTimeout, "timeout", 300, "Timeout in seconds to wait for response")
	sendCmd.Flags().BoolVar(&sendNoWait, "no-wait", false, "Send without waiting for a response")
}

func runSend(cmd *cobra.Command, args []string) error {
	sessionName := args[0]
	instruction := args[1]

	adapter, err := mux.Resolve(sendMux)
	if err != nil {
		return err
	}

	if sendNoWait {
		if err := adapter.SendKeys(sessionName, instruction); err != nil {
			return fmt.Errorf("sending to %s session %q: %w", adapter.Name(), sessionName, err)
		}
		if err := mux.VerifySend(adapter, sessionName, instruction); err != nil {
			return fmt.Errorf("send verification failed for %s session %q: %w", adapter.Name(), sessionName, err)
		}
		fmt.Printf("Sent instruction to %s session %q\n", adapter.Name(), sessionName)
		logSendAction(sessionName, instruction, "(no-wait)")
		return nil
	}

	// Find all matching sessions and record their baseline file sizes
	matched, err := findMatchingSessions(sessionName)
	if err != nil {
		return fmt.Errorf("could not find session for %q: %w", sessionName, err)
	}

	baselines := make(map[string]int64)
	for _, s := range matched {
		size, err := fileSize(s.FilePath)
		if err != nil {
			continue
		}
		baselines[s.FilePath] = size
	}

	// Send the instruction
	if err := adapter.SendKeys(sessionName, instruction); err != nil {
		return fmt.Errorf("sending to %s session %q: %w", adapter.Name(), sessionName, err)
	}
	if err := mux.VerifySend(adapter, sessionName, instruction); err != nil {
		return fmt.Errorf("send verification failed for %s session %q: %w", adapter.Name(), sessionName, err)
	}
	fmt.Fprintf(os.Stderr, "Sent instruction to %s session %q. Waiting for response...\n", adapter.Name(), sessionName)

	// Poll all matching sessions for a new assistant message
	timeout := time.Duration(sendTimeout) * time.Second
	pollInterval := 3 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		for _, s := range matched {
			baseline, ok := baselines[s.FilePath]
			if !ok {
				continue
			}

			currentSize, err := fileSize(s.FilePath)
			if err != nil || currentSize <= baseline {
				continue
			}

			// File has grown — check for a new assistant message
			raw := &session.SessionInfo{FilePath: s.FilePath}
			msg := session.LastAssistantMessage(raw)
			if msg == "" {
				continue
			}

			_ = session.EnrichSession(raw)
			if raw.LastRole != "assistant" {
				continue
			}

			fmt.Printf("[%s] %s (%s)\n\n%s\n",
				s.Agent, s.Repository, s.GitBranch, msg)
			logSendAction(sessionName, instruction, msg)
			return nil
		}
	}

	return fmt.Errorf("timed out after %s waiting for response", timeout)
}

func findMatchingSessions(query string) ([]provider.SessionInfo, error) {
	agents, err := selectedAgents(sendAgent)
	if err != nil {
		return nil, err
	}

	maxAge := time.Duration(sendHours) * time.Hour
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

	var matched []provider.SessionInfo
	q := strings.ToLower(query)
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

// logSendAction logs a send action to the database (fire-and-forget).
func logSendAction(sessionName, instruction, result string) {
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  sessionName,
			ActionType: "send",
			Content:    instruction,
			Result:     result,
		})
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
