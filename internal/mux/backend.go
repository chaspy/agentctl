package mux

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// EnvMux selects the session backend used by agentctl commands that create,
// inspect, or destroy agent sessions. Supported values: "zellij" (default), "herdr".
const EnvMux = "AGENTCTL_MUX"

// AgentState is the lifecycle state of the agent running inside a session.
// Values mirror herdr's agent_status; zellij cannot observe them and reports StateNone.
type AgentState string

const (
	StateNone    AgentState = ""
	StateIdle    AgentState = "idle"
	StateWorking AgentState = "working"
	StateBlocked AgentState = "blocked"
	StateDone    AgentState = "done"
	StateUnknown AgentState = "unknown"
)

// SessionState describes one live mux session (a zellij session or a herdr workspace).
type SessionState struct {
	Name   string
	Exited bool // zellij only: the session is in EXITED state (attach to resurrect)
	// CWD is the working directory when the backend can report it directly
	// (herdr). Empty for zellij, where callers fall back to dump-layout.
	CWD string
	// Agent is the detected agent kind ("claude", "codex", ...) when known.
	Agent string
	// State is the backend-reported agent state when known.
	State AgentState
	// Target is a backend-specific handle (herdr pane id). Informational only.
	Target string
}

// ZellijSessionState is kept as an alias for existing callers and tests.
type ZellijSessionState = SessionState

// SpawnOptions describes a session to create.
type SpawnOptions struct {
	// Name is the mux session name (zellij session name / herdr workspace label).
	Name string
	// CWD is the working directory for the session's shell.
	CWD string
	// Command is the full command line launched in the session, e.g.
	// "claude --dangerously-skip-permissions". May be any shell command.
	Command string
	// Agent is an optional hint ("claude", "codex") describing the agent that
	// Command starts. Backends that recognise agents (herdr) use it to launch
	// the command through their agent surface.
	Agent string
	// ReadyTimeout bounds how long Spawn waits for the backend to report the
	// session (and the agent, when supported) as ready. Zero selects a default.
	ReadyTimeout time.Duration
}

// SpawnResult reports the host process agentctl owns for a spawned session,
// used for runtime ownership tracking (`agentctl runtime reconcile`). Zero
// values mean the backend owns no dedicated process for the session.
type SpawnResult struct {
	RuntimePID  int
	RuntimePGID int
	StartedAt   time.Time
}

// Backend is the high-level session interface implemented by every supported
// terminal multiplexer. It extends Adapter, which keeps the low-level
// type/enter/dump primitives used by the delivery verification helpers.
type Backend interface {
	Adapter
	// Spawn creates a new session named opts.Name in opts.CWD and starts opts.Command in it.
	Spawn(opts SpawnOptions) (*SpawnResult, error)
	// Send submits text followed by Enter to the session's agent.
	Send(session, text string) error
	// Read returns recent terminal output of the session's main pane.
	Read(session string) (string, error)
	// Kill terminates the session and its processes.
	Kill(session string) error
	// List returns all live sessions.
	List() ([]SessionState, error)
	// Status returns the backend-observed agent state, or StateNone when the
	// backend cannot observe it (zellij) or no agent is running.
	Status(session string) (AgentState, error)
}

// BackendName returns the configured backend name (AGENTCTL_MUX, default zellij).
func BackendName() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvMux)))
	if v == "" {
		return "zellij"
	}
	return v
}

// ResolveBackend returns the backend selected by AGENTCTL_MUX.
// It is a variable so tests can substitute a fake.
var ResolveBackend = func() (Backend, error) {
	return resolveBackendByName(BackendName())
}

func resolveBackendByName(name string) (Backend, error) {
	switch name {
	case "zellij":
		b := zellijAdapter{}
		if !b.available() {
			return nil, fmt.Errorf("zellij is not installed")
		}
		return b, nil
	case "herdr":
		b := newHerdrBackend()
		if !b.available() {
			return nil, fmt.Errorf("herdr is not installed")
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown %s backend %q (supported: zellij, herdr)", EnvMux, name)
	}
}

// ListSessionsDetailed lists sessions through the configured backend.
func ListSessionsDetailed() ([]SessionState, error) {
	b, err := ResolveBackend()
	if err != nil {
		return nil, err
	}
	return b.List()
}

// SessionExists reports whether a session with the exact name is live.
func SessionExists(b Backend, name string) (bool, error) {
	sessions, err := b.List()
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// WaitForSession polls until a session with the exact name appears.
func WaitForSession(b Backend, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok, err := SessionExists(b, name); err == nil && ok {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s session %q to appear", b.Name(), name)
}
