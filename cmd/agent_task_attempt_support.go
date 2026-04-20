package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

type agentTaskAttemptPlan struct {
	Attempt *store.AgentTaskAttempt
	Spawn   spawnExecutionRequest
}

var runAgentTaskAttemptSpawn = func(plan *agentTaskAttemptPlan) (*spawnExecutionResult, error) {
	return executeSpawnRequest(plan.Spawn)
}

func buildAgentTaskAttemptPlan(task *store.AgentTask, decision *store.AgentTaskDecision, branchOverride, sessionNameOverride, messageOverride, summaryOverride string) (*agentTaskAttemptPlan, error) {
	if task == nil {
		return nil, fmt.Errorf("agent task is required")
	}
	if decision == nil {
		return nil, fmt.Errorf("decision is required")
	}

	repo, err := resolveAgentTaskRepo(task, decision)
	if err != nil {
		return nil, err
	}

	selectedAgent, err := normalizeAgentPreference(decision.SelectedAgent)
	if err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(branchOverride)
	if branch == "" && decision.SelectedRepoMode == "branch" {
		branch = defaultAgentTaskAttemptBranch(task)
	}

	repoBaseName := filepath.Base(repo.FullPath)
	sessionName := defaultSpawnSessionName(repoBaseName, branch, strings.TrimSpace(sessionNameOverride))
	message := strings.TrimSpace(messageOverride)
	if message == "" {
		message = defaultAgentTaskAttemptMessage(task)
	}
	summary := strings.TrimSpace(summaryOverride)
	if summary == "" {
		summary = task.Objective
	}
	launchCommand := agentLaunchCommand(provider.Agent(selectedAgent))

	attempt := &store.AgentTaskAttempt{
		DecisionID:     decision.ID,
		AgentTaskName:  task.Name,
		RepoRef:        task.RepoRef,
		Repository:     task.Repository,
		TaskType:       task.TaskType,
		Risk:           task.Risk,
		Agent:          selectedAgent,
		RepoMode:       decision.SelectedRepoMode,
		Branch:         branch,
		SessionName:    sessionName,
		LaunchCommand:  launchCommand,
		InitialMessage: message,
		Summary:        summary,
		Status:         "starting",
	}

	return &agentTaskAttemptPlan{
		Attempt: attempt,
		Spawn: spawnExecutionRequest{
			Repo:          repo,
			RepoMode:      decision.SelectedRepoMode,
			Branch:        branch,
			SessionName:   sessionName,
			Message:       message,
			Summary:       summary,
			Loop:          false,
			SelectedAgent: provider.Agent(selectedAgent),
		},
	}, nil
}

func resolveAgentTaskRepo(task *store.AgentTask, decision *store.AgentTaskDecision) (RepoEntry, error) {
	candidates := []string{
		strings.TrimSpace(task.Repository),
		strings.TrimSpace(decision.Repository),
		strings.TrimSpace(task.RepoRef),
		strings.TrimSpace(decision.RepoRef),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		repo, err := ResolveRepoPath(candidate)
		if err == nil {
			return repo, nil
		}
	}
	return RepoEntry{}, fmt.Errorf("could not resolve local repo path for task %q", task.Name)
}

func defaultAgentTaskAttemptBranch(task *store.AgentTask) string {
	if task == nil {
		return ""
	}
	return sanitizeBranchName(strings.TrimSpace(task.Name))
}

func defaultAgentTaskAttemptMessage(task *store.AgentTask) string {
	if task == nil {
		return ""
	}
	lines := []string{strings.TrimSpace(task.Objective)}
	if len(task.DesiredOutcome) > 0 {
		lines = append(lines, "", "Desired outcome:")
		for _, item := range task.DesiredOutcome {
			lines = append(lines, "- "+item)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
