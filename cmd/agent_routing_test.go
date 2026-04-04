package cmd

import (
	"fmt"
	"testing"

	"github.com/chaspy/agentctl/internal/provider"
)

func TestChooseSpawnAgent(t *testing.T) {
	origLookup := lookupExecutable
	origClaudeRate := claudeRateFn
	origCodexRate := codexRateFn
	t.Cleanup(func() {
		lookupExecutable = origLookup
		claudeRateFn = origClaudeRate
		codexRateFn = origCodexRate
	})

	lookupExecutable = func(name string) (string, error) {
		switch name {
		case "claude", "codex":
			return "/usr/bin/" + name, nil
		default:
			return "", fmt.Errorf("missing %s", name)
		}
	}

	t.Run("research prefers codex", func(t *testing.T) {
		claudeRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentClaude, Summary: "allowed", RemainingPct: 80}, nil
		}
		codexRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentCodex, Summary: "available", RemainingPct: 60}, nil
		}

		agent, _, err := chooseSpawnAgent("auto", "research")
		if err != nil {
			t.Fatalf("chooseSpawnAgent: %v", err)
		}
		if agent != provider.AgentCodex {
			t.Fatalf("agent = %q, want %q", agent, provider.AgentCodex)
		}
	})

	t.Run("implementation falls back to codex when claude limited", func(t *testing.T) {
		claudeRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentClaude, Summary: "RATE LIMITED (resets 15:00)", RemainingPct: 0}, nil
		}
		codexRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentCodex, Summary: "available", RemainingPct: 40}, nil
		}

		agent, _, err := chooseSpawnAgent("auto", "implementation")
		if err != nil {
			t.Fatalf("chooseSpawnAgent: %v", err)
		}
		if agent != provider.AgentCodex {
			t.Fatalf("agent = %q, want %q", agent, provider.AgentCodex)
		}
	})

	t.Run("explicit claude is honored", func(t *testing.T) {
		claudeRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentClaude, Summary: "allowed", RemainingPct: 70}, nil
		}
		codexRateFn = func() (provider.RateInfo, error) {
			return provider.RateInfo{Agent: provider.AgentCodex, Summary: "available", RemainingPct: 100}, nil
		}

		agent, _, err := chooseSpawnAgent("claude", "docs")
		if err != nil {
			t.Fatalf("chooseSpawnAgent: %v", err)
		}
		if agent != provider.AgentClaude {
			t.Fatalf("agent = %q, want %q", agent, provider.AgentClaude)
		}
	})
}

func TestNormalizeTaskType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "general", want: taskTypeGeneral},
		{input: "fix", want: taskTypeImplementation},
		{input: "doc", want: taskTypeDocs},
		{input: "review", want: taskTypeReview},
	}

	for _, tc := range tests {
		got, err := normalizeTaskType(tc.input)
		if err != nil {
			t.Fatalf("normalizeTaskType(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeTaskType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
