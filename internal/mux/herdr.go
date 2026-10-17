package mux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Environment variables understood by the herdr backend.
const (
	// EnvHerdrSession selects a named herdr session (`herdr --session <name>`).
	// Empty targets the default session.
	EnvHerdrSession = "AGENTCTL_HERDR_SESSION"
	// EnvHerdrAutostart controls whether agentctl starts a headless `herdr server`
	// when no server is running. "0"/"false" disables autostart.
	EnvHerdrAutostart = "AGENTCTL_HERDR_AUTOSTART"
)

// herdr agent kinds that agentctl may launch through `herdr agent start`.
var herdrAgentKinds = map[string]bool{
	"claude": true,
	"codex":  true,
}

var herdrAgentNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Timings used by the herdr backend; variables so tests can shorten them.
var (
	HerdrServerStartTimeout  = 10 * time.Second
	HerdrDeliveryWaitTimeout = 20 * time.Second
)

// herdrBackend talks to a herdr server through the `herdr` CLI, which prints
// JSON for every socket-API command.
type herdrBackend struct {
	session string
	binary  string
}

func newHerdrBackend() *herdrBackend {
	return &herdrBackend{
		session: strings.TrimSpace(os.Getenv(EnvHerdrSession)),
		binary:  "herdr",
	}
}

func (*herdrBackend) Name() string { return "herdr" }

func (h *herdrBackend) available() bool {
	_, err := exec.LookPath(h.binary)
	return err == nil
}

// --- CLI plumbing -----------------------------------------------------------

type herdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *herdrError) Error() string {
	if e.Message == "" {
		return "herdr: " + e.Code
	}
	return fmt.Sprintf("herdr: %s (%s)", e.Message, e.Code)
}

type herdrEnvelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *herdrError     `json:"error"`
}

// herdrErrorCode extracts the herdr error code from err, or "".
func herdrErrorCode(err error) string {
	var he *herdrError
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

func (h *herdrBackend) globalArgs() []string {
	if h.session == "" {
		return nil
	}
	return []string{"--session", h.session}
}

// cleanEnv strips variables that would make a child agent believe it is nested
// inside another agent or inside zellij.
func cleanEnv() []string {
	drop := map[string]bool{
		"CLAUDECODE": true, "ZELLIJ": true, "ZELLIJ_SESSION_NAME": true, "ZELLIJ_PANE_ID": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if drop[key] {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// runJSON executes a herdr CLI command and decodes its JSON envelope.
// When the server is not running and autostart is allowed, it starts a
// headless server once and retries.
func (h *herdrBackend) runJSON(args ...string) (json.RawMessage, error) {
	result, err := h.runJSONOnce(args...)
	if herdrErrorCode(err) == "server_not_running" && h.autostartEnabled() {
		if startErr := h.ensureServer(); startErr != nil {
			return nil, fmt.Errorf("%w (autostart failed: %v)", err, startErr)
		}
		return h.runJSONOnce(args...)
	}
	return result, err
}

func (h *herdrBackend) runJSONOnce(args ...string) (json.RawMessage, error) {
	full := append(h.globalArgs(), args...)
	cmd := exec.Command(h.binary, full...)
	cmd.Env = cleanEnv()
	out, runErr := cmd.Output()
	// herdr prints the JSON error on stderr with exit status 1; the envelope
	// may also land on stdout. Try both.
	var env herdrEnvelope
	if len(strings.TrimSpace(string(out))) > 0 && json.Unmarshal(out, &env) == nil {
		if env.Error != nil {
			return nil, env.Error
		}
		if runErr == nil {
			return env.Result, nil
		}
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		stderr := strings.TrimSpace(string(ee.Stderr))
		var env2 herdrEnvelope
		if stderr != "" && json.Unmarshal([]byte(stderr), &env2) == nil && env2.Error != nil {
			return nil, env2.Error
		}
		return nil, fmt.Errorf("herdr %s failed: %w: %s", strings.Join(args, " "), runErr, stderr)
	}
	if runErr != nil {
		return nil, fmt.Errorf("herdr %s failed: %w", strings.Join(args, " "), runErr)
	}
	return env.Result, nil
}

// runText executes a herdr CLI command that prints plain text (pane read).
func (h *herdrBackend) runText(args ...string) (string, error) {
	full := append(h.globalArgs(), args...)
	cmd := exec.Command(h.binary, full...)
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr := strings.TrimSpace(string(ee.Stderr))
			var env herdrEnvelope
			if stderr != "" && json.Unmarshal([]byte(stderr), &env) == nil && env.Error != nil {
				if env.Error.Code == "server_not_running" && h.autostartEnabled() {
					if startErr := h.ensureServer(); startErr == nil {
						return h.runText(args...)
					}
				}
				return "", env.Error
			}
			return "", fmt.Errorf("herdr %s failed: %w: %s", strings.Join(args, " "), err, stderr)
		}
		return "", fmt.Errorf("herdr %s failed: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (h *herdrBackend) autostartEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvHerdrAutostart)))
	return v != "0" && v != "false" && v != "no"
}

// ensureServer starts a detached headless `herdr server` and waits until the
// socket answers. herdr never starts its server from a CLI command, so agentctl
// does it the same way it used to spawn a detached zellij server.
func (h *herdrBackend) ensureServer() error {
	args := append(h.globalArgs(), "server")
	cmd := exec.Command(h.binary, args...)
	cmd.Env = cleanEnv()
	cmd.Dir = os.TempDir()
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting herdr server: %w", err)
	}
	// Do not wait on the child; it outlives this process.
	go func() { _ = cmd.Wait() }()

	fmt.Fprintf(os.Stderr, "Starting headless herdr server...\n")
	deadline := time.Now().Add(HerdrServerStartTimeout)
	for time.Now().Before(deadline) {
		if _, err := h.runJSONOnce("workspace", "list"); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("herdr server did not become ready within %s", HerdrServerStartTimeout)
}

// --- herdr object model -----------------------------------------------------

type herdrWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
	AgentStatus string `json:"agent_status"`
}

type herdrPane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	CWD         string `json:"cwd"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	Focused     bool   `json:"focused"`
}

type herdrAgent struct {
	Name             string `json:"name"`
	Agent            string `json:"agent"`
	AgentStatus      string `json:"agent_status"`
	PaneID           string `json:"pane_id"`
	WorkspaceID      string `json:"workspace_id"`
	CWD              string `json:"cwd"`
	InteractiveReady bool   `json:"interactive_ready"`
	StateChangeSeq   int    `json:"state_change_seq"`
}

func (h *herdrBackend) listWorkspaces() ([]herdrWorkspace, error) {
	raw, err := h.runJSON("workspace", "list")
	if err != nil {
		return nil, err
	}
	var res struct {
		Workspaces []herdrWorkspace `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decoding herdr workspace list: %w", err)
	}
	return res.Workspaces, nil
}

func (h *herdrBackend) listPanes(workspaceID string) ([]herdrPane, error) {
	args := []string{"pane", "list"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	raw, err := h.runJSON(args...)
	if err != nil {
		return nil, err
	}
	var res struct {
		Panes []herdrPane `json:"panes"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decoding herdr pane list: %w", err)
	}
	return res.Panes, nil
}

func (h *herdrBackend) listAgents() ([]herdrAgent, error) {
	raw, err := h.runJSON("agent", "list")
	if err != nil {
		return nil, err
	}
	var res struct {
		Agents []herdrAgent `json:"agents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decoding herdr agent list: %w", err)
	}
	return res.Agents, nil
}

func (h *herdrBackend) getAgent(target string) (*herdrAgent, error) {
	raw, err := h.runJSON("agent", "get", target)
	if err != nil {
		return nil, err
	}
	var res struct {
		Agent herdrAgent `json:"agent"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decoding herdr agent get: %w", err)
	}
	return &res.Agent, nil
}

// paneNumber extracts N from "wX:pN" for stable ordering.
func paneNumber(paneID string) int {
	i := strings.LastIndex(paneID, ":p")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(paneID[i+2:])
	return n
}

// resolved is a session name mapped to its herdr workspace and main pane.
type herdrResolved struct {
	Name      string
	Workspace herdrWorkspace
	Pane      herdrPane
}

// mainPane picks the pane agentctl treats as the session's agent pane:
// the pane hosting an agent if there is exactly one such pane, otherwise the
// lowest-numbered pane (the workspace root pane).
func mainPane(panes []herdrPane) (herdrPane, bool) {
	if len(panes) == 0 {
		return herdrPane{}, false
	}
	sort.Slice(panes, func(i, j int) bool { return paneNumber(panes[i].PaneID) < paneNumber(panes[j].PaneID) })
	var withAgent []herdrPane
	for _, p := range panes {
		if p.Agent != "" {
			withAgent = append(withAgent, p)
		}
	}
	if len(withAgent) == 1 {
		return withAgent[0], true
	}
	return panes[0], true
}

func (h *herdrBackend) resolve(query string) (*herdrResolved, error) {
	name, err := h.ResolveSession(query)
	if err != nil {
		return nil, err
	}
	workspaces, err := h.listWorkspaces()
	if err != nil {
		return nil, err
	}
	var ws *herdrWorkspace
	for i := range workspaces {
		if workspaces[i].Label == name {
			ws = &workspaces[i]
			break
		}
	}
	if ws == nil {
		return nil, fmt.Errorf("herdr workspace %q not found", name)
	}
	panes, err := h.listPanes(ws.WorkspaceID)
	if err != nil {
		return nil, err
	}
	pane, ok := mainPane(panes)
	if !ok {
		return nil, fmt.Errorf("herdr workspace %q has no panes", name)
	}
	return &herdrResolved{Name: name, Workspace: *ws, Pane: pane}, nil
}

// --- Adapter ---------------------------------------------------------------

func (h *herdrBackend) ListSessions() ([]string, error) {
	workspaces, err := h.listWorkspaces()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.Label != "" {
			names = append(names, ws.Label)
		}
	}
	return names, nil
}

func (h *herdrBackend) ResolveSession(query string) (string, error) {
	return resolveSession(h, query)
}

func (h *herdrBackend) SendKeys(session, text string) error {
	return h.Send(session, text)
}

func (h *herdrBackend) TypeText(session, text string) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	_, err = h.runJSON("pane", "send-text", r.Pane.PaneID, text)
	return err
}

func (h *herdrBackend) SendEnter(session string) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	_, err = h.runJSON("pane", "send-keys", r.Pane.PaneID, "enter")
	return err
}

func (h *herdrBackend) ClearInput(session string) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	_, err = h.runJSON("pane", "send-keys", r.Pane.PaneID, "ctrl+u")
	return err
}

// DumpScreen returns the currently rendered viewport, the closest equivalent
// of `zellij action dump-screen`.
func (h *herdrBackend) DumpScreen(session string) (string, error) {
	r, err := h.resolve(session)
	if err != nil {
		return "", err
	}
	return h.runText("pane", "read", r.Pane.PaneID, "--source", "visible")
}

// --- Backend ---------------------------------------------------------------

// herdrAgentName derives a valid, unique herdr agent name from a session name.
func herdrAgentName(session string, taken map[string]bool) string {
	var b strings.Builder
	for _, r := range strings.ToLower(session) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name == "" {
		name = "agentctl"
	} else if !(name[0] >= 'a' && name[0] <= 'z') {
		name = "a" + name
	}
	if len(name) > 32 {
		name = strings.TrimRight(name[:32], "-_")
	}
	if !herdrAgentNameRe.MatchString(name) {
		name = "agentctl"
	}
	candidate := name
	for i := 2; taken[candidate]; i++ {
		suffix := "-" + strconv.Itoa(i)
		base := name
		if len(base)+len(suffix) > 32 {
			base = strings.TrimRight(base[:32-len(suffix)], "-_")
		}
		candidate = base + suffix
	}
	return candidate
}

// splitCommand splits a launch command line on whitespace. Launch commands
// used by agentctl never contain quoted arguments.
func splitCommand(command string) []string {
	return strings.Fields(command)
}

// Spawn creates a herdr workspace labelled opts.Name and starts opts.Command in
// its root pane. Commands that start a known agent kind go through
// `herdr agent start` so herdr tracks the agent's lifecycle state from the start.
func (h *herdrBackend) Spawn(opts SpawnOptions) (*SpawnResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("herdr spawn: session name is required")
	}
	if exists, err := SessionExists(h, opts.Name); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("herdr session %q already exists", opts.Name)
	}

	args := []string{"workspace", "create", "--label", opts.Name, "--no-focus"}
	if opts.CWD != "" {
		args = append(args, "--cwd", opts.CWD)
	}
	raw, err := h.runJSON(args...)
	if err != nil {
		return nil, fmt.Errorf("creating herdr workspace: %w", err)
	}
	var created struct {
		Workspace herdrWorkspace `json:"workspace"`
		RootPane  herdrPane      `json:"root_pane"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("decoding herdr workspace create: %w", err)
	}
	paneID := created.RootPane.PaneID
	fmt.Fprintf(os.Stderr, "Created herdr workspace %q (%s, pane %s) in %s\n",
		opts.Name, created.Workspace.WorkspaceID, paneID, opts.CWD)

	result := &SpawnResult{StartedAt: time.Now()}
	result.RuntimePID, result.RuntimePGID = h.paneShellProcess(paneID)

	fields := splitCommand(opts.Command)
	if len(fields) == 0 {
		return result, nil
	}

	kind := strings.ToLower(opts.Agent)
	if kind == "" && herdrAgentKinds[fields[0]] {
		kind = fields[0]
	}
	if herdrAgentKinds[kind] && fields[0] == kind {
		if err := h.startAgent(opts.Name, kind, paneID, fields[1:], opts.ReadyTimeout); err != nil {
			return nil, err
		}
		return result, nil
	}

	if _, err := h.runJSON("pane", "run", paneID, opts.Command); err != nil {
		return nil, fmt.Errorf("running launch command in herdr pane %s: %w", paneID, err)
	}
	return result, nil
}

// paneShellProcess returns the pane shell's pid and process group for runtime
// ownership records. The pane shell is owned by the shared herdr server, so the
// pgid is only reported when the shell leads its own process group; otherwise
// signalling it could reach the server itself and both values stay zero.
func (h *herdrBackend) paneShellProcess(paneID string) (pid, pgid int) {
	raw, err := h.runJSON("pane", "process-info", "--pane", paneID)
	if err != nil {
		return 0, 0
	}
	var res struct {
		ProcessInfo struct {
			ShellPID int `json:"shell_pid"`
		} `json:"process_info"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.ProcessInfo.ShellPID <= 1 {
		return 0, 0
	}
	shellPID := res.ProcessInfo.ShellPID
	group, err := syscall.Getpgid(shellPID)
	if err != nil || group != shellPID {
		return shellPID, 0
	}
	return shellPID, group
}

func (h *herdrBackend) startAgent(session, kind, paneID string, agentArgs []string, timeout time.Duration) error {
	taken := map[string]bool{}
	if agents, err := h.listAgents(); err == nil {
		for _, a := range agents {
			taken[a.Name] = true
		}
	}
	name := herdrAgentName(session, taken)

	args := []string{"agent", "start", name, "--kind", kind, "--pane", paneID}
	if timeout > 0 {
		args = append(args, "--timeout", strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	if len(agentArgs) > 0 {
		args = append(args, "--")
		args = append(args, agentArgs...)
	}
	_, err := h.runJSON(args...)
	switch herdrErrorCode(err) {
	case "":
		fmt.Fprintf(os.Stderr, "Started %s agent %q in herdr pane %s\n", kind, name, paneID)
		return nil
	case "agent_not_ready":
		// The agent is up but blocked on a startup dialog (trust prompt etc.).
		// Callers handle that through Read/SendEnter, exactly as with zellij.
		fmt.Fprintf(os.Stderr, "Started %s agent %q in herdr pane %s (blocked during startup)\n", kind, name, paneID)
		return nil
	default:
		return fmt.Errorf("starting %s agent in herdr pane %s: %w", kind, paneID, err)
	}
}

// Send submits text plus Enter. When herdr recognises an agent in the pane,
// `agent prompt` performs the submission atomically (honouring bracketed
// paste). Without an agent, `pane run` submits it to the shell.
func (h *herdrBackend) Send(session, text string) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	if r.Pane.Agent == "" {
		_, err = h.runJSON("pane", "run", r.Pane.PaneID, text)
		return err
	}
	_, err = h.runJSON("agent", "prompt", r.Pane.PaneID, text)
	if herdrErrorCode(err) == "agent_blocked" {
		// Keep zellij parity: raw input still reaches a blocked agent.
		fmt.Fprintf(os.Stderr, "warning: herdr reports agent in %q is blocked; sending raw input\n", r.Name)
		if _, err := h.runJSON("pane", "send-text", r.Pane.PaneID, text); err != nil {
			return err
		}
		_, err = h.runJSON("pane", "send-keys", r.Pane.PaneID, "enter")
	}
	return err
}

// Prompt is the atomic submission path used by `agentctl send`.
func (h *herdrBackend) Prompt(session, text string) error {
	return h.Send(session, text)
}

// Read returns recent unwrapped output (scrollback included), which is the
// best source for transcripts of agent responses.
func (h *herdrBackend) Read(session string) (string, error) {
	r, err := h.resolve(session)
	if err != nil {
		return "", err
	}
	return h.runText("pane", "read", r.Pane.PaneID, "--source", "recent-unwrapped", "--lines", "120")
}

// Kill closes the whole workspace; herdr terminates every pane process in it.
func (h *herdrBackend) Kill(session string) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	_, err = h.runJSON("workspace", "close", r.Workspace.WorkspaceID)
	if herdrErrorCode(err) == "workspace_group_close_required" {
		_, err = h.runJSON("workspace", "close", r.Workspace.WorkspaceID, "--group")
	}
	return err
}

// List returns one entry per labelled workspace with the agent kind and state
// of its main pane.
func (h *herdrBackend) List() ([]SessionState, error) {
	workspaces, err := h.listWorkspaces()
	if err != nil {
		return nil, err
	}
	panes, err := h.listPanes("")
	if err != nil {
		return nil, err
	}
	byWorkspace := map[string][]herdrPane{}
	for _, p := range panes {
		byWorkspace[p.WorkspaceID] = append(byWorkspace[p.WorkspaceID], p)
	}
	sessions := make([]SessionState, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.Label == "" {
			continue
		}
		state := SessionState{Name: ws.Label}
		if p, ok := mainPane(byWorkspace[ws.WorkspaceID]); ok {
			state.CWD = p.CWD
			state.Agent = p.Agent
			state.Target = p.PaneID
			if p.Agent != "" {
				state.State = AgentState(p.AgentStatus)
			}
		}
		sessions = append(sessions, state)
	}
	return sessions, nil
}

// Status returns herdr's agent_status for the session's main pane.
func (h *herdrBackend) Status(session string) (AgentState, error) {
	r, err := h.resolve(session)
	if err != nil {
		return StateNone, err
	}
	if r.Pane.Agent == "" {
		return StateNone, nil
	}
	agent, err := h.getAgent(r.Pane.PaneID)
	if herdrErrorCode(err) == "agent_not_found" {
		return StateNone, nil
	}
	if err != nil {
		return StateNone, err
	}
	return AgentState(agent.AgentStatus), nil
}

// WaitForOutput blocks until the pane output contains match, using herdr's
// server-side `pane wait-output`.
func (h *herdrBackend) WaitForOutput(session, match string, timeout time.Duration) error {
	r, err := h.resolve(session)
	if err != nil {
		return err
	}
	args := []string{"pane", "wait-output", r.Pane.PaneID, "--match", match}
	if timeout > 0 {
		args = append(args, "--timeout", strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	_, err = h.runJSON(args...)
	return err
}

// WaitForState blocks until the agent reaches one of the given states.
func (h *herdrBackend) WaitForState(session string, timeout time.Duration, states ...AgentState) (AgentState, error) {
	r, err := h.resolve(session)
	if err != nil {
		return StateNone, err
	}
	args := []string{"agent", "wait", r.Pane.PaneID}
	for _, s := range states {
		args = append(args, "--until", string(s))
	}
	if timeout > 0 {
		args = append(args, "--timeout", strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	raw, err := h.runJSON(args...)
	if err != nil {
		return StateNone, err
	}
	var res struct {
		Agent herdrAgent `json:"agent"`
	}
	if json.Unmarshal(raw, &res) == nil && res.Agent.AgentStatus != "" {
		return AgentState(res.Agent.AgentStatus), nil
	}
	return StateUnknown, nil
}

// VerifyDelivery implements `agentctl send --verify` on herdr. Instead of
// sleeping and scraping the screen, it asks herdr to wait until the agent
// reports working or blocked, which proves the prompt was accepted. If no
// activity is observed and the text is still sitting in the input box, the
// prompt is cleared and re-submitted once.
func (h *herdrBackend) VerifyDelivery(session, sentText string) (bool, error) {
	if _, err := h.WaitForState(session, HerdrDeliveryWaitTimeout, StateWorking, StateBlocked); err == nil {
		return false, nil
	}

	screen, err := h.DumpScreen(session)
	if err != nil {
		return false, fmt.Errorf("reading pane: %w", err)
	}
	if !HasDeliveryPending(screen, sentText) {
		// The turn completed before we looked, or the agent is idle again.
		return false, nil
	}

	if err := h.ClearInput(session); err != nil {
		return true, fmt.Errorf("clear input failed: %w", err)
	}
	if err := h.Send(session, sentText); err != nil {
		return true, fmt.Errorf("re-send failed: %w", err)
	}
	if _, err := h.WaitForState(session, HerdrDeliveryWaitTimeout, StateWorking, StateBlocked); err != nil {
		return true, fmt.Errorf("agent did not start working after retry: %w", err)
	}
	return true, nil
}
