package mux

import (
	"fmt"
	"os/exec"
	"strings"
)

type tmuxAdapter struct{}

func (tmuxAdapter) Name() string {
	return "tmux"
}

func (tmuxAdapter) available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func (t tmuxAdapter) ResolveSession(query string) (string, error) {
	return resolveSession(t, query)
}

func (t tmuxAdapter) SendKeys(session string, text string) error {
	resolved, err := t.ResolveSession(session)
	if err != nil {
		return err
	}
	cmd := exec.Command("tmux", "send-keys", "-t", resolved, text, "Enter")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (t tmuxAdapter) SendEnter(session string) error {
	resolved, err := t.ResolveSession(session)
	if err != nil {
		return err
	}
	cmd := exec.Command("tmux", "send-keys", "-t", resolved, "Enter")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-enter failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (t tmuxAdapter) DumpScreen(session string) (string, error) {
	resolved, err := t.ResolveSession(session)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("tmux", "capture-pane", "-t", resolved, "-p")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (tmuxAdapter) ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "failed to connect to server") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions failed: %w: %s", err, text)
	}
	return splitSessions(string(output)), nil
}
