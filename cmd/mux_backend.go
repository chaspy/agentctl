package cmd

import (
	"fmt"
	"strings"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/session"
)

// resolveMuxBackend returns the session backend selected by AGENTCTL_MUX
// (zellij by default, herdr when AGENTCTL_MUX=herdr). Tests substitute it.
var resolveMuxBackend = func() (mux.Backend, error) {
	return mux.ResolveBackend()
}

// muxName is the configured backend name, used in user-facing messages.
func muxName() string {
	return mux.BackendName()
}

// muxSessionIDPrefix is prepended to the mux session name to form the
// sessions.session_id column ("zellij-<name>" / "herdr-<name>").
func muxSessionIDPrefix() string {
	return muxName() + "-"
}

// listMuxSessionNames returns the names of all live mux sessions.
func listMuxSessionNames() ([]string, error) {
	backend, err := resolveMuxBackend()
	if err != nil {
		return nil, err
	}
	sessions, err := backend.List()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sessions))
	for _, s := range sessions {
		names = append(names, s.Name)
	}
	return names, nil
}

// muxSessionExists reports whether a live mux session has exactly this name.
// Errors are treated as "not found" because every caller only uses it as a guard.
func muxSessionExists(name string) bool {
	names, err := listMuxSessionNames()
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// resolveMuxSessionQuery resolves a session query (exact, then case-insensitive
// substring) against live mux sessions.
func resolveMuxSessionQuery(query string) (string, error) {
	names, err := listMuxSessionNames()
	if err != nil {
		return "", fmt.Errorf("failed to list %s sessions: %w", muxName(), err)
	}
	for _, n := range names {
		if n == query {
			return n, nil
		}
	}
	q := strings.ToLower(query)
	var candidates []string
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), q) {
			candidates = append(candidates, n)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no %s session matching %q", muxName(), query)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("ambiguous query %q, matches: %s", query, strings.Join(candidates, ", "))
	}
}

// muxStatusOverride maps a backend-observed agent state onto agentctl's
// session status. It returns "" when the backend has no opinion, in which case
// callers keep the JSONL-derived status.
func muxStatusOverride(state mux.AgentState) string {
	switch state {
	case mux.StateWorking:
		return session.StatusActive
	case mux.StateBlocked:
		return session.StatusBlocked
	case mux.StateIdle, mux.StateDone:
		return session.StatusIdle
	default:
		return ""
	}
}

// muxStatesByName returns backend-observed agent states keyed by session name
// (lower-cased) and by CWD. Empty when the backend cannot observe states.
func muxStatesByName() (byName map[string]mux.AgentState, byCWD map[string]mux.AgentState) {
	byName = map[string]mux.AgentState{}
	byCWD = map[string]mux.AgentState{}
	backend, err := resolveMuxBackend()
	if err != nil {
		return byName, byCWD
	}
	sessions, err := backend.List()
	if err != nil {
		return byName, byCWD
	}
	for _, s := range sessions {
		if s.State == mux.StateNone {
			continue
		}
		byName[strings.ToLower(s.Name)] = s.State
		if s.CWD != "" {
			byCWD[s.CWD] = s.State
		}
	}
	return byName, byCWD
}

// isMuxSessionRunning reports whether the named session is live and not EXITED.
func isMuxSessionRunning(name string) bool {
	backend, err := resolveMuxBackend()
	if err != nil {
		return false
	}
	sessions, err := backend.List()
	if err != nil {
		return false
	}
	for _, s := range sessions {
		if s.Name == name {
			return !s.Exited
		}
	}
	return false
}

// cleanupExitedMuxSession removes a lingering EXITED session (zellij only) so
// its name can be reused.
func cleanupExitedMuxSession(name string) {
	backend, err := resolveMuxBackend()
	if err != nil {
		return
	}
	sessions, err := backend.List()
	if err != nil {
		return
	}
	for _, s := range sessions {
		if s.Name == name && s.Exited {
			fmt.Printf("Cleaning up exited session %q...\n", name)
			_ = backend.Kill(name)
		}
	}
}
