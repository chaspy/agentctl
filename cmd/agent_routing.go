package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/provider"
)

const (
	spawnAgentAuto         = "auto"
	taskTypeGeneral        = "general"
	taskTypeImplementation = "implementation"
	taskTypeResearch       = "research"
	taskTypeDocs           = "docs"
	taskTypeReview         = "review"
)

var (
	lookupExecutable = exec.LookPath
	claudeRateFn     = provider.ClaudeRate
	codexRateFn      = provider.CodexRate
	resolveSpawnMux  = func() (mux.Adapter, error) { return mux.Resolve("zellij") }
)

type agentCandidate struct {
	agent     provider.Agent
	available bool
	remaining int
}

func normalizeTaskType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", taskTypeGeneral:
		return taskTypeGeneral, nil
	case "code", "fix", "refactor", taskTypeImplementation:
		return taskTypeImplementation, nil
	case taskTypeResearch:
		return taskTypeResearch, nil
	case "doc", taskTypeDocs:
		return taskTypeDocs, nil
	case taskTypeReview:
		return taskTypeReview, nil
	default:
		return "", fmt.Errorf("invalid task type %q: must be one of general, implementation, research, docs, review", value)
	}
}

func normalizeAgentPreference(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", spawnAgentAuto:
		return spawnAgentAuto, nil
	case string(provider.AgentClaude):
		return string(provider.AgentClaude), nil
	case string(provider.AgentCodex):
		return string(provider.AgentCodex), nil
	default:
		return "", fmt.Errorf("invalid agent %q: must be auto, claude, or codex", value)
	}
}

func chooseSpawnAgent(agentPref, taskType string) (provider.Agent, string, error) {
	normalizedPref, err := normalizeAgentPreference(agentPref)
	if err != nil {
		return "", "", err
	}
	normalizedTask, err := normalizeTaskType(taskType)
	if err != nil {
		return "", "", err
	}

	claude := inspectAgent(provider.AgentClaude)
	codex := inspectAgent(provider.AgentCodex)

	if normalizedPref != spawnAgentAuto {
		selected := candidateByAgent(claude, codex, provider.Agent(normalizedPref))
		if !selected.available {
			return "", "", fmt.Errorf("%s is unavailable", normalizedPref)
		}
		return selected.agent, fmt.Sprintf("explicit %s selection", normalizedPref), nil
	}

	ordered := orderCandidates(normalizedTask, claude, codex)
	for i, candidate := range ordered {
		if !candidate.available {
			continue
		}
		reason := fmt.Sprintf("task-type %s prefers %s", normalizedTask, candidate.agent)
		if i > 0 {
			reason = fmt.Sprintf("task-type %s fallback to %s", normalizedTask, candidate.agent)
		}
		return candidate.agent, reason, nil
	}

	return "", "", fmt.Errorf("no available agent for task type %s", normalizedTask)
}

func inspectAgent(agent provider.Agent) agentCandidate {
	candidate := agentCandidate{agent: agent, available: true, remaining: 50}

	binary := string(agent)
	if _, err := lookupExecutable(binary); err != nil {
		candidate.available = false
		candidate.remaining = 0
		return candidate
	}

	var (
		info provider.RateInfo
		err  error
	)
	switch agent {
	case provider.AgentClaude:
		info, err = claudeRateFn()
	case provider.AgentCodex:
		info, err = codexRateFn()
	}

	if err != nil {
		return candidate
	}

	if info.RemainingPct >= 0 {
		candidate.remaining = info.RemainingPct
	}

	summary := strings.ToLower(info.Summary)
	if strings.Contains(summary, "rate limited") || strings.Contains(summary, "hit limit") {
		candidate.available = false
		candidate.remaining = 0
		return candidate
	}
	if info.RemainingPct == 0 {
		candidate.available = false
	}

	return candidate
}

func orderCandidates(taskType string, claude, codex agentCandidate) []agentCandidate {
	weights := map[provider.Agent]int{
		provider.AgentClaude: claude.remaining,
		provider.AgentCodex:  codex.remaining,
	}

	switch taskType {
	case taskTypeResearch, taskTypeDocs:
		weights[provider.AgentCodex] += 25
	case taskTypeImplementation, taskTypeReview:
		weights[provider.AgentClaude] += 25
	default:
		weights[provider.AgentClaude] += 10
		weights[provider.AgentCodex] += 10
	}

	if weights[provider.AgentCodex] > weights[provider.AgentClaude] {
		return []agentCandidate{codex, claude}
	}
	return []agentCandidate{claude, codex}
}

func candidateByAgent(claude, codex agentCandidate, agent provider.Agent) agentCandidate {
	if agent == provider.AgentCodex {
		return codex
	}
	return claude
}

func agentLaunchCommand(agent provider.Agent) string {
	switch agent {
	case provider.AgentCodex:
		return "codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen"
	default:
		return "claude --dangerously-skip-permissions"
	}
}

func waitForCodexReady(sessionName string, timeout time.Duration) error {
	adapter, err := resolveSpawnMux()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		screen, err := adapter.DumpScreen(sessionName)
		if err == nil {
			if strings.Contains(screen, "Do you trust the contents of this directory?") {
				if err := adapter.SendEnter(sessionName); err != nil {
					return fmt.Errorf("accept codex trust prompt: %w", err)
				}
				time.Sleep(2 * time.Second)
				continue
			}
			if strings.Contains(screen, "OpenAI Codex") || strings.Contains(screen, "/model to change") {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for codex to become ready")
}
