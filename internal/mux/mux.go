package mux

import (
	"fmt"
	"os"
	"strings"
)

type Adapter interface {
	Name() string
	SendKeys(session string, text string) error
	ListSessions() ([]string, error)
	// ResolveSession finds the best matching session name from the list.
	ResolveSession(query string) (string, error)
	available() bool
}

func Resolve(name string) (Adapter, error) {
	switch name {
	case "tmux":
		adapter := tmuxAdapter{}
		if !adapter.available() {
			return nil, fmt.Errorf("tmux is not installed")
		}
		return adapter, nil
	case "zellij":
		adapter := zellijAdapter{}
		if !adapter.available() {
			return nil, fmt.Errorf("zellij is not installed")
		}
		return adapter, nil
	case "auto":
		return resolveAuto()
	default:
		return nil, fmt.Errorf("unknown mux %q", name)
	}
}

func resolveAuto() (Adapter, error) {
	zellij := zellijAdapter{}
	tmux := tmuxAdapter{}

	switch {
	case os.Getenv("ZELLIJ") != "" && zellij.available():
		return zellij, nil
	case os.Getenv("TMUX") != "" && tmux.available():
		return tmux, nil
	case zellij.available():
		return zellij, nil
	case tmux.available():
		return tmux, nil
	default:
		return nil, fmt.Errorf("no supported mux found (zellij/tmux)")
	}
}

// resolveSession finds the best matching session from the adapter's session list.
// It tries exact match first, then substring match (case-insensitive).
func resolveSession(adapter Adapter, query string) (string, error) {
	sessions, err := adapter.ListSessions()
	if err != nil {
		return "", err
	}

	// Exact match
	for _, s := range sessions {
		if s == query {
			return s, nil
		}
	}

	// Substring match (case-insensitive)
	q := strings.ToLower(query)
	var candidates []string
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s), q) {
			candidates = append(candidates, s)
		}
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("ambiguous session query %q, matches: %s", query, strings.Join(candidates, ", "))
	}

	return "", fmt.Errorf("%s session matching %q not found", adapter.Name(), query)
}
