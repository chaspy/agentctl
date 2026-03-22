package cmd

import (
	"fmt"

	"github.com/chaspy/agentctl/internal/provider"
)

func selectedAgents(filter string) ([]provider.Agent, error) {
	switch filter {
	case "", "all":
		return []provider.Agent{provider.AgentClaude, provider.AgentCodex}, nil
	case string(provider.AgentClaude):
		return []provider.Agent{provider.AgentClaude}, nil
	case string(provider.AgentCodex):
		return []provider.Agent{provider.AgentCodex}, nil
	default:
		return nil, fmt.Errorf("unknown agent %q (expected all, claude, codex)", filter)
	}
}
