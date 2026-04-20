package cmd

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/chaspy/agentctl/internal/controlplane"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

func buildAgentTaskDecision(db *sql.DB, task *store.AgentTask) (*store.AgentTaskDecision, error) {
	repoMode := "branch"
	profileSource := controlplane.RepoProfileSourceDefault
	modeSource := controlplane.RepoProfileSourceDefault
	agentPref := spawnAgentAuto
	agentSource := controlplane.RepoProfileSourceDefault
	routingPolicyRef := task.RoutingPolicyRef

	if profile, err := controlplane.GetRepoProfile(db, task.Repository); err == nil && profile != nil {
		profileSource = profile.PrimarySource
		repoMode = profile.Mode
		modeSource = profile.ModeSource
		agentPref = profile.Agent
		agentSource = profile.AgentSource
		if routingPolicyRef == "" && profile.ManagedRepo != nil {
			routingPolicyRef = profile.ManagedRepo.DefaultRoutingPolicyRef
		}
	}

	normalizedTask, err := normalizeTaskType(task.TaskType)
	if err != nil {
		return nil, err
	}
	normalizedPref, err := normalizeAgentPreference(agentPref)
	if err != nil {
		return nil, err
	}

	claude := inspectAgent(provider.AgentClaude)
	codex := inspectAgent(provider.AgentCodex)
	selectedAgent, routeReason, err := chooseSpawnAgent(agentPref, task.TaskType)
	if err != nil {
		return nil, err
	}

	candidateScores := buildDecisionCandidateScores(normalizedTask, normalizedPref, claude, codex)
	eligibleAgents := make([]string, 0, len(candidateScores))
	for _, candidate := range candidateScores {
		if candidate.Available {
			eligibleAgents = append(eligibleAgents, candidate.Agent)
		}
	}

	policyVersion := "none"
	if routingPolicyRef != "" {
		policyVersion = "unresolved"
	}

	selectionMode := "auto_task_type"
	if normalizedPref != spawnAgentAuto {
		selectionMode = "explicit"
	}

	return &store.AgentTaskDecision{
		AgentTaskName:     task.Name,
		RepoRef:           task.RepoRef,
		Repository:        task.Repository,
		TaskType:          normalizedTask,
		Risk:              task.Risk,
		RoutingPolicyRef:  routingPolicyRef,
		PolicyVersion:     policyVersion,
		SelectionMode:     selectionMode,
		SelectedAgent:     string(selectedAgent),
		SelectedRepoMode:  repoMode,
		RepoProfileSource: profileSource,
		ModeSource:        modeSource,
		AgentSource:       agentSource,
		EligibleAgents:    eligibleAgents,
		CandidateScores:   candidateScores,
		RouteReason:       routeReason,
		Status:            "recorded",
	}, nil
}

func buildDecisionCandidateScores(taskType, agentPref string, claude, codex agentCandidate) []store.AgentTaskDecisionCandidate {
	scores := map[provider.Agent]float64{
		provider.AgentClaude: float64(claude.remaining),
		provider.AgentCodex:  float64(codex.remaining),
	}

	switch taskType {
	case taskTypeResearch, taskTypeDocs:
		scores[provider.AgentCodex] += 25
	case taskTypeImplementation, taskTypeReview:
		scores[provider.AgentClaude] += 25
	default:
		scores[provider.AgentClaude] += 10
		scores[provider.AgentCodex] += 10
	}

	reasonFor := func(agent provider.Agent) string {
		switch agentPref {
		case string(agent):
			return fmt.Sprintf("explicit preference for %s", agent)
		case spawnAgentAuto:
			if taskType == taskTypeDocs || taskType == taskTypeResearch {
				if agent == provider.AgentCodex {
					return fmt.Sprintf("task-type %s preference bonus", taskType)
				}
				return fmt.Sprintf("task-type %s fallback candidate", taskType)
			}
			if taskType == taskTypeImplementation || taskType == taskTypeReview {
				if agent == provider.AgentClaude {
					return fmt.Sprintf("task-type %s preference bonus", taskType)
				}
				return fmt.Sprintf("task-type %s fallback candidate", taskType)
			}
			return fmt.Sprintf("task-type %s balanced candidate", taskType)
		default:
			return "repo profile preference"
		}
	}

	candidates := []store.AgentTaskDecisionCandidate{
		{
			Agent:     string(provider.AgentClaude),
			Score:     scores[provider.AgentClaude],
			Available: claude.available,
			Reason:    reasonFor(provider.AgentClaude),
		},
		{
			Agent:     string(provider.AgentCodex),
			Score:     scores[provider.AgentCodex],
			Available: codex.available,
			Reason:    reasonFor(provider.AgentCodex),
		},
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Agent < candidates[j].Agent
		}
		return candidates[i].Score > candidates[j].Score
	})
	return candidates
}
