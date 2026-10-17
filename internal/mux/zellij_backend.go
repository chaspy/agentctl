package mux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// zellijCommand builds a zellij invocation that ignores the caller's own
// ZELLIJ environment so it targets the named session, not the enclosing one.
func zellijCommand(args ...string) *exec.Cmd {
	full := append([]string{"-u", "ZELLIJ", "zellij"}, args...)
	return exec.Command("env", full...)
}

// Spawn creates a detached zellij session in opts.CWD and types opts.Command into it.
func (z zellijAdapter) Spawn(opts SpawnOptions) (*SpawnResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("zellij spawn: session name is required")
	}
	if exists, err := SessionExists(z, opts.Name); err == nil && exists {
		return nil, fmt.Errorf("zellij session %q already exists", opts.Name)
	}

	bgCmd := exec.Command("script", "-q", "/dev/null",
		"env", "-u", "ZELLIJ", "-u", "CLAUDECODE",
		"zellij", "-s", opts.Name)
	bgCmd.Dir = opts.CWD
	bgCmd.Stdin = nil
	bgCmd.Stdout = nil
	bgCmd.Stderr = nil
	bgCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := bgCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to create zellij session: %w", err)
	}
	result := &SpawnResult{RuntimePID: bgCmd.Process.Pid, StartedAt: time.Now()}
	result.RuntimePGID, _ = syscall.Getpgid(result.RuntimePID)

	timeout := opts.ReadyTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	fmt.Fprintf(os.Stderr, "Creating zellij session %q in %s...\n", opts.Name, opts.CWD)
	if err := WaitForSession(z, opts.Name, timeout); err != nil {
		return nil, err
	}

	// Dismiss any zellij tip overlay before typing.
	_ = zellijCommand("-s", opts.Name, "action", "write", "27").Run()
	time.Sleep(500 * time.Millisecond)

	if strings.TrimSpace(opts.Command) == "" {
		return result, nil
	}
	if out, err := zellijCommand("-s", opts.Name, "action", "write-chars", opts.Command).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to send launch command: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if out, err := zellijCommand("-s", opts.Name, "action", "write", "13").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to send enter: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return result, nil
}

// Send types text and presses Enter in the session (same as SendKeys).
func (z zellijAdapter) Send(session, text string) error {
	return z.SendKeys(session, text)
}

// Read returns the visible screen of the focused pane.
func (z zellijAdapter) Read(session string) (string, error) {
	return z.DumpScreen(session)
}

// Kill terminates the session. kill-session moves an active session to EXITED,
// so delete-session always follows to free the name; for sessions that are
// already EXITED only delete-session succeeds.
func (z zellijAdapter) Kill(session string) error {
	killOut, killErr := zellijCommand("kill-session", session).CombinedOutput()
	delOut, delErr := zellijCommand("delete-session", session).CombinedOutput()
	if killErr != nil && delErr != nil {
		return fmt.Errorf("failed to kill/delete zellij session %q: kill: %s, delete: %s",
			session, strings.TrimSpace(string(killOut)), strings.TrimSpace(string(delOut)))
	}
	if killErr == nil && delErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete zellij session %q after kill: %s\n",
			session, strings.TrimSpace(string(delOut)))
	}
	return nil
}

// List returns all zellij sessions including EXITED ones.
func (zellijAdapter) List() ([]SessionState, error) {
	return ListZellijSessionsDetailed()
}

// Status always reports StateNone: zellij has no notion of agent state.
func (zellijAdapter) Status(string) (AgentState, error) {
	return StateNone, nil
}
