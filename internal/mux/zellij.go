package mux

import (
	"fmt"
	"os/exec"
	"strings"
)

type zellijAdapter struct{}

func (zellijAdapter) Name() string {
	return "zellij"
}

func (zellijAdapter) available() bool {
	_, err := exec.LookPath("zellij")
	return err == nil
}

func (z zellijAdapter) ResolveSession(query string) (string, error) {
	return resolveSession(z, query)
}

func (z zellijAdapter) SendKeys(session string, text string) error {
	resolved, err := z.ResolveSession(session)
	if err != nil {
		return err
	}

	// Focus the first pane (top-left) by moving focus up and left.
	// This ensures we send to pane #1 where the agent is expected to run.
	for _, dir := range []string{"up", "left"} {
		focus := exec.Command("zellij", "--session", resolved, "action", "move-focus", dir)
		_ = focus.Run()
	}

	writeChars := exec.Command("zellij", "--session", resolved, "action", "write-chars", text)
	output, err := writeChars.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zellij write-chars failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	writeEnter := exec.Command("zellij", "--session", resolved, "action", "write", "13")
	output, err = writeEnter.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zellij write enter failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (zellijAdapter) ListSessions() ([]string, error) {
	cmd := exec.Command("zellij", "list-sessions", "--short")
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "No active zellij sessions found.") {
			return nil, nil
		}
		return nil, fmt.Errorf("zellij list-sessions failed: %w: %s", err, text)
	}
	return splitSessions(string(output)), nil
}

func splitSessions(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	sessions := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sessions = append(sessions, line)
	}
	return sessions
}
