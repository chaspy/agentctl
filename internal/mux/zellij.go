package mux

import (
	"fmt"
	"os"
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

// focusFirstPane moves focus to the top-left pane where the agent runs.
func (zellijAdapter) focusFirstPane(resolved string) {
	for _, dir := range []string{"up", "left"} {
		focus := exec.Command("zellij", "--session", resolved, "action", "move-focus", dir)
		_ = focus.Run()
	}
}

func (z zellijAdapter) SendKeys(session string, text string) error {
	resolved, err := z.ResolveSession(session)
	if err != nil {
		return err
	}

	z.focusFirstPane(resolved)

	writeChars := exec.Command("zellij", "--session", resolved, "action", "write-chars", text)
	output, err := writeChars.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zellij write-chars failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return z.sendEnterResolved(resolved)
}

func (z zellijAdapter) SendEnter(session string) error {
	resolved, err := z.ResolveSession(session)
	if err != nil {
		return err
	}
	z.focusFirstPane(resolved)
	return z.sendEnterResolved(resolved)
}

func (zellijAdapter) sendEnterResolved(resolved string) error {
	writeEnter := exec.Command("zellij", "--session", resolved, "action", "write", "13")
	output, err := writeEnter.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zellij write enter failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (z zellijAdapter) DumpScreen(session string) (string, error) {
	resolved, err := z.ResolveSession(session)
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "agentctl-screen-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	z.focusFirstPane(resolved)

	cmd := exec.Command("zellij", "--session", resolved, "action", "dump-screen", tmpPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("zellij dump-screen failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("reading screen dump: %w", err)
	}

	return string(data), nil
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
