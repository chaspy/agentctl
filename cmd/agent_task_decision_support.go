package cmd

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

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
	repoRole := ""
	repoTier := ""

	if profile, err := controlplane.GetRepoProfile(db, task.Repository); err == nil && profile != nil {
		profileSource = profile.PrimarySource
		repoMode = profile.Mode
		modeSource = profile.ModeSource
		agentPref = profile.Agent
		agentSource = profile.AgentSource
		if routingPolicyRef == "" && profile.ManagedRepo != nil {
			routingPolicyRef = profile.ManagedRepo.DefaultRoutingPolicyRef
		}
		if profile.ManagedRepo != nil {
			repoRole = profile.ManagedRepo.Role
			repoTier = profile.ManagedRepo.Tier
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
	routingEval, err := controlplane.ResolveRoutingPolicy(db, routingPolicyRef, controlplane.PolicyMatchContext{
		TaskType: normalizedTask,
		Risk:     task.Risk,
		RepoRole: repoRole,
		RepoTier: repoTier,
	})
	if err != nil {
		return nil, err
	}
	selectedAgent, routeReason, selectionMode, err := chooseDecisionAgent(normalizedPref, normalizedTask, routingEval, claude, codex)
	if err != nil {
		return nil, err
	}

	candidateScores := buildDecisionCandidateScores(normalizedTask, normalizedPref, routingEval, claude, codex)
	eligibleAgents := make([]string, 0, len(candidateScores))
	for _, candidate := range candidateScores {
		if candidate.Available {
			eligibleAgents = append(eligibleAgents, candidate.Agent)
		}
	}

	policyVersion := "none"
	if routingPolicyRef != "" {
		if routingEval.PolicyVersion != "" {
			policyVersion = routingEval.PolicyVersion
		} else {
			policyVersion = "unresolved"
		}
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

func chooseDecisionAgent(agentPref, taskType string, routingEval controlplane.RoutingPolicyEvaluation, claude, codex agentCandidate) (provider.Agent, string, string, error) {
	if agentPref != spawnAgentAuto {
		selected := candidateByAgent(claude, codex, provider.Agent(agentPref))
		if !selected.available {
			return "", "", "", fmt.Errorf("%s is unavailable", agentPref)
		}
		return selected.agent, fmt.Sprintf("explicit %s selection", agentPref), "explicit", nil
	}

	if routingEval.PolicyFound && routingEval.Matched {
		ordered := orderedPolicyCandidates(routingEval)
		for idx, name := range ordered {
			candidate := candidateByAgent(claude, codex, provider.Agent(name))
			if !candidate.available {
				continue
			}
			return candidate.agent, policySelectionReason(routingEval, candidate.agent, idx), "policy", nil
		}
	}

	selectedAgent, routeReason, err := chooseSpawnAgent(agentPref, taskType)
	if err != nil {
		return "", "", "", err
	}
	if routingEval.PolicyFound && !routingEval.Matched {
		routeReason = fmt.Sprintf("routing policy %s had no matching rule; %s", routingEval.PolicyRef, routeReason)
	} else if routingEval.PolicyFound && routingEval.Matched {
		routeReason = fmt.Sprintf("routing policy %s preferred unavailable agents; %s", routingEval.PolicyRef, routeReason)
	}
	return selectedAgent, routeReason, "auto_task_type", nil
}

func orderedPolicyCandidates(routingEval controlplane.RoutingPolicyEvaluation) []string {
	seen := map[string]struct{}{}
	ordered := []string{}
	for _, candidate := range []string{routingEval.Prefer, routingEval.Secondary, routingEval.FallbackAgent} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		ordered = append(ordered, candidate)
	}
	return ordered
}

func policySelectionReason(routingEval controlplane.RoutingPolicyEvaluation, agent provider.Agent, index int) string {
	role := "preferred"
	switch {
	case string(agent) == routingEval.Secondary:
		role = "secondary"
	case string(agent) == routingEval.FallbackAgent && index > 0:
		role = "fallback"
	case index > 0:
		role = "fallback"
	}

	reason := strings.TrimSpace(routingEval.Reason)
	if reason == "" {
		return fmt.Sprintf("routing policy %s selected %s %s candidate", routingEval.PolicyRef, agent, role)
	}
	return fmt.Sprintf("routing policy %s selected %s %s candidate: %s", routingEval.PolicyRef, agent, role, reason)
}

func buildDecisionCandidateScores(taskType, agentPref string, routingEval controlplane.RoutingPolicyEvaluation, claude, codex agentCandidate) []store.AgentTaskDecisionCandidate {
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
	if routingEval.PolicyFound && routingEval.Matched {
		if routingEval.Prefer != "" {
			scores[provider.Agent(routingEval.Prefer)] += 40
		}
		if routingEval.Secondary != "" {
			scores[provider.Agent(routingEval.Secondary)] += 20
		}
		if routingEval.FallbackAgent != "" {
			scores[provider.Agent(routingEval.FallbackAgent)] += 10
		}
	}

	reasonFor := func(agent provider.Agent) string {
		if agentPref == string(agent) {
			return fmt.Sprintf("explicit preference for %s", agent)
		}
		if agentPref == spawnAgentAuto && routingEval.PolicyFound && routingEval.Matched {
			switch string(agent) {
			case routingEval.Prefer:
				return fmt.Sprintf("routing policy %s preferred candidate", routingEval.PolicyRef)
			case routingEval.Secondary:
				return fmt.Sprintf("routing policy %s secondary candidate", routingEval.PolicyRef)
			case routingEval.FallbackAgent:
				return fmt.Sprintf("routing policy %s fallback candidate", routingEval.PolicyRef)
			}
		}
		switch agentPref {
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
