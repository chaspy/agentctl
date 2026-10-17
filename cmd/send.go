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
	sendVerify  bool
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
	sendCmd.Flags().StringVar(&sendMux, "mux", "auto", "Mux backend: auto, tmux, zellij, herdr (auto honours AGENTCTL_MUX)")
	sendCmd.Flags().StringVar(&sendAgent, "agent", "all", "Filter by agent: all, claude, codex")
	sendCmd.Flags().IntVar(&sendHours, "hours", 24, "Search sessions active within the last N hours")
	sendCmd.Flags().IntVar(&sendTimeout, "timeout", 30, "Timeout in seconds to wait for response")
	sendCmd.Flags().BoolVar(&sendNoWait, "no-wait", false, "Send without waiting for a response")
	sendCmd.Flags().BoolVar(&sendVerify, "verify", false, "Verify delivery and retry if the prompt still contains the message (zellij: screen check after 20s; herdr: agent wait)")
	sendCmd.Flags().BoolVar(&sendVerify, "verify-delivery", false, "Alias for --verify")
}

func runSend(cmd *cobra.Command, args []string) error {
	sessionName := args[0]
	instruction := args[1]

	adapter, err := mux.Resolve(sendMux)
	if err != nil {
		return err
	}

	resolvedSessionName := sessionName
	if resolved, err := adapter.ResolveSession(sessionName); err == nil {
		resolvedSessionName = resolved
	}

	if sendNoWait {
		if err := sendInstruction(adapter, sessionName, instruction, inferSendAgent(resolvedSessionName, nil)); err != nil {
			return err
		}
		recordSessionLastSent(resolvedSessionName)
		fmt.Printf("Sent instruction to %s session %q\n", adapter.Name(), sessionName)
		logSendAction(sessionName, instruction, "(no-wait)")
		return nil
	}

	// Find all matching sessions and record their baseline file sizes
	matched, err := findMatchingSessions(sessionName, adapter)
	if err != nil {
		return fmt.Errorf("could not find session for %q: %w", sessionName, err)
	}

	targetAgent := inferSendAgent(resolvedSessionName, matched)

	// Mux session matched but no JSONL sessions available for monitoring — just send.
	if len(matched) == 0 {
		if err := sendInstruction(adapter, sessionName, instruction, targetAgent); err != nil {
			return err
		}
		recordSessionLastSent(resolvedSessionName)
		fmt.Fprintf(os.Stderr, "Sent instruction to %s session %q (no JSONL session found for monitoring)\n", adapter.Name(), sessionName)
		logSendAction(sessionName, instruction, "(sent, no monitoring)")
		return nil
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
	if err := sendInstruction(adapter, sessionName, instruction, targetAgent); err != nil {
		return err
	}
	recordSessionLastSent(resolvedSessionName)
	fmt.Fprintf(os.Stderr, "Sent instruction to %s session %q. Waiting for response...\n", adapter.Name(), sessionName)

	timeout := time.Duration(sendTimeout) * time.Second

	// Backends that track agent state (herdr) can wait for the turn to settle
	// instead of polling JSONL files, which also covers sessions whose JSONL
	// did not exist yet when the baseline was taken.
	if waiter, ok := adapter.(mux.StateWaiter); ok {
		if result, ok := waitForResponseViaBackend(waiter, adapter, resolvedSessionName, timeout); ok {
			fmt.Printf("[%s] %s (%s)\n\n%s\n", result.Agent, result.Repository, result.GitBranch, result.Message)
			logSendAction(sessionName, instruction, result.Message)
			return nil
		}
	}

	// Poll all matching sessions for a new assistant message
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

func sendInstruction(adapter mux.Adapter, sessionName, instruction string, targetAgent provider.Agent) error {
	restarted, err := mux.EnsureSessionReady(adapter, sessionName, string(targetAgent), launchCommandForSend(targetAgent))
	if err != nil {
		return fmt.Errorf("pre-send session check failed for %s session %q: %w", adapter.Name(), sessionName, err)
	}
	if restarted {
		fmt.Fprintf(os.Stderr, "Restarted %s in %s session %q before sending.\n", targetAgentLabel(targetAgent), adapter.Name(), sessionName)
	}

	if prompter, ok := adapter.(mux.PromptSender); ok {
		// Backends with an atomic prompt surface (herdr `agent prompt`) submit
		// text and Enter together, so the typed-input routing check is unnecessary.
		if err := prompter.Prompt(sessionName, instruction); err != nil {
			return fmt.Errorf("prompting %s session %q: %w", adapter.Name(), sessionName, err)
		}
	} else {
		if err := adapter.TypeText(sessionName, instruction); err != nil {
			return fmt.Errorf("typing into %s session %q: %w", adapter.Name(), sessionName, err)
		}
		verifyErr := mux.VerifyTypedInputVisible(adapter, sessionName, instruction)
		if err := adapter.SendEnter(sessionName); err != nil {
			return fmt.Errorf("sending enter to %s session %q: %w", adapter.Name(), sessionName, err)
		}
		if verifyErr != nil {
			fmt.Fprintf(os.Stderr, "warning: typed input not visible in %s session %q (worker may be active): %v\n", adapter.Name(), sessionName, verifyErr)
		}
	}
	if err := mux.VerifySend(adapter, sessionName, instruction); err != nil {
		return fmt.Errorf("send verification failed for %s session %q: %w", adapter.Name(), sessionName, err)
	}
	if !sendVerify {
		return nil
	}

	retried, err := mux.VerifyDelivery(adapter, sessionName, instruction)
	if err != nil {
		return fmt.Errorf("delivery verification failed for %s session %q: %w", adapter.Name(), sessionName, err)
	}
	if retried {
		fmt.Println("送達確認: リトライ実行")
		return nil
	}

	fmt.Println("送達確認: OK")
	return nil
}

type backendResponse struct {
	Agent      provider.Agent
	Repository string
	GitBranch  string
	Message    string
}

// waitForResponseViaBackend waits until the backend reports the agent settled
// (idle/done/blocked) and then returns the newest assistant message from the
// JSONL session living in the mux session's CWD.
func waitForResponseViaBackend(waiter mux.StateWaiter, adapter mux.Adapter, sessionName string, timeout time.Duration) (backendResponse, bool) {
	// Gate on observed activity so a prompt that has not started its turn yet
	// does not satisfy the settled wait with the previous idle state.
	_, _ = waiter.WaitForState(sessionName, mux.ActivityGateTimeout, mux.StateWorking, mux.StateBlocked)

	state, err := waiter.WaitForState(sessionName, timeout, mux.SettledStates...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s wait failed, falling back to JSONL polling: %v\n", adapter.Name(), err)
		return backendResponse{}, false
	}
	if state == mux.StateBlocked {
		fmt.Fprintf(os.Stderr, "%s reports the agent is blocked (waiting for approval or input)\n", adapter.Name())
	}

	backend, ok := adapter.(mux.Backend)
	if !ok {
		return backendResponse{}, false
	}
	sessions, err := backend.List()
	if err != nil {
		return backendResponse{}, false
	}
	var cwd string
	for _, s := range sessions {
		if s.Name == sessionName {
			cwd = s.CWD
			break
		}
	}
	if cwd == "" {
		return backendResponse{}, false
	}

	agents, err := selectedAgents(sendAgent)
	if err != nil {
		return backendResponse{}, false
	}
	var newest *provider.SessionInfo
	for _, agent := range agents {
		var items []provider.SessionInfo
		switch agent {
		case provider.AgentClaude:
			items, _ = provider.ScanClaudeSessions(time.Duration(sendHours) * time.Hour)
		case provider.AgentCodex:
			items, _ = provider.ScanCodexSessions(time.Duration(sendHours) * time.Hour)
		}
		for i := range items {
			if items[i].CWD != cwd {
				continue
			}
			if newest == nil || items[i].ModTime.After(newest.ModTime) {
				newest = &items[i]
			}
		}
	}
	if newest == nil {
		return backendResponse{}, false
	}

	msg := newest.LastMessage
	if newest.FilePath != "" {
		raw := &session.SessionInfo{FilePath: newest.FilePath}
		if full := session.LastAssistantMessage(raw); full != "" {
			msg = full
		}
	}
	if msg == "" {
		return backendResponse{}, false
	}
	return backendResponse{Agent: newest.Agent, Repository: newest.Repository, GitBranch: newest.GitBranch, Message: msg}, true
}

func inferSendAgent(sessionName string, matched []provider.SessionInfo) provider.Agent {
	if agent := inferSendAgentFromStore(sessionName); agent != "" {
		return agent
	}

	seen := map[provider.Agent]struct{}{}
	for _, s := range matched {
		seen[s.Agent] = struct{}{}
	}
	if len(seen) != 1 {
		return ""
	}
	for agent := range seen {
		return agent
	}
	return ""
}

func inferSendAgentFromStore(sessionName string) provider.Agent {
	db, err := store.Open("")
	if err != nil {
		return ""
	}
	defer db.Close()

	sessions, err := store.FindSessionByZellijSession(db, sessionName)
	if err != nil || len(sessions) == 0 {
		return ""
	}

	if agent := uniqueSessionAgent(filterExactZellijSessions(sessions, sessionName)); agent != "" {
		return provider.Agent(agent)
	}
	return provider.Agent(uniqueSessionAgent(sessions))
}

func filterExactZellijSessions(sessions []store.Session, sessionName string) []store.Session {
	var exact []store.Session
	for _, s := range sessions {
		if strings.EqualFold(strings.TrimSpace(s.ZellijSession), strings.TrimSpace(sessionName)) {
			exact = append(exact, s)
		}
	}
	return exact
}

func uniqueSessionAgent(sessions []store.Session) string {
	if len(sessions) == 0 {
		return ""
	}

	seen := map[string]struct{}{}
	for _, s := range sessions {
		if s.Agent == "" {
			continue
		}
		seen[s.Agent] = struct{}{}
	}
	if len(seen) != 1 {
		return ""
	}
	for agent := range seen {
		return agent
	}
	return ""
}

func launchCommandForSend(agent provider.Agent) string {
	if agent == "" {
		return ""
	}
	return agentLaunchCommand(agent)
}

func targetAgentLabel(agent provider.Agent) string {
	if agent == "" {
		return "agent"
	}
	return string(agent)
}

// sessionResolver is satisfied by mux.Adapter and allows resolving a mux session by name.
type sessionResolver interface {
	ResolveSession(query string) (string, error)
}

func findMatchingSessions(query string, resolver sessionResolver) ([]provider.SessionInfo, error) {
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

	return resolveSessionsForSend(sessions, query, resolver)
}

// resolveSessionsForSend selects sessions to use for response monitoring.
// It first tries repository substring match. If nothing matches, it falls back to checking
// the mux adapter for a direct session name match, returning all scanned sessions as
// best-effort monitoring candidates.
func resolveSessionsForSend(sessions []provider.SessionInfo, query string, resolver sessionResolver) ([]provider.SessionInfo, error) {
	matched := matchByRepository(sessions, query)
	if len(matched) > 0 {
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].ModTime.After(matched[j].ModTime)
		})
		return matched, nil
	}

	// No repository match — check if the query is a direct mux session name.
	if _, err := resolver.ResolveSession(query); err != nil {
		return nil, fmt.Errorf("no session found matching %q", query)
	}

	// Mux session found: return all scanned sessions for best-effort response monitoring.
	// We cannot directly link a mux session name to a specific JSONL file without a
	// CWD mapping, so we monitor all recent sessions and return the first one that responds.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	return sessions, nil
}

// matchByRepository returns sessions whose Repository field contains query (case-insensitive).
func matchByRepository(sessions []provider.SessionInfo, query string) []provider.SessionInfo {
	var matched []provider.SessionInfo
	q := strings.ToLower(query)
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Repository), q) {
			matched = append(matched, s)
		}
	}
	return matched
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

func recordSessionLastSent(zellijSession string) {
	if strings.TrimSpace(zellijSession) == "" {
		return
	}
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_, _ = store.TouchSessionLastSentByZellijSession(db, zellijSession, time.Now())
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
