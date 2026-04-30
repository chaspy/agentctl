package controlplane

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
)

type PolicyMatchContext struct {
	TaskType string
	Risk     string
	RepoRole string
	RepoTier string
}

type RoutingPolicyEvaluation struct {
	PolicyRef     string
	PolicyVersion string
	PolicyFound   bool
	Matched       bool
	Prefer        string
	Secondary     string
	FallbackAgent string
	Approval      string
	Reason        string
}

type ApprovalPolicyEvaluation struct {
	PolicyRef             string
	PolicyVersion         string
	PolicyFound           bool
	Matched               bool
	RequiresHumanApproval bool
	RequiredBefore        []string
	Reason                string
}

type policyWhen struct {
	TaskType string `json:"taskType"`
	Risk     string `json:"risk"`
	RepoRole string `json:"repoRole"`
}

type routingPolicySpec struct {
	Version  any `json:"version"`
	Defaults struct {
		FallbackAgent string `json:"fallbackAgent"`
	} `json:"defaults"`
	Rules []struct {
		When      policyWhen `json:"when"`
		Prefer    string     `json:"prefer"`
		Secondary string     `json:"secondary"`
		Approval  string     `json:"approval"`
		Reason    string     `json:"reason"`
	} `json:"rules"`
}

type approvalPolicySpec struct {
	Version any `json:"version"`
	Rules   []struct {
		When                  policyWhen `json:"when"`
		RequiresHumanApproval bool       `json:"requiresHumanApproval"`
		RequiredBefore        []string   `json:"requiredBefore"`
		Reason                string     `json:"reason"`
	} `json:"rules"`
}

func ResolveRoutingPolicy(db *sql.DB, policyRef string, ctx PolicyMatchContext) (RoutingPolicyEvaluation, error) {
	result := RoutingPolicyEvaluation{PolicyRef: strings.TrimSpace(policyRef)}
	if result.PolicyRef == "" {
		return result, nil
	}

	resource, err := store.GetControlPlaneResource(db, "RoutingPolicy", result.PolicyRef)
	if err != nil {
		return result, err
	}
	if resource == nil {
		return result, nil
	}
	result.PolicyFound = true

	var spec routingPolicySpec
	if err := json.Unmarshal([]byte(resource.RawSpecJSON), &spec); err != nil {
		result.Reason = "routing policy parse failed"
		return result, nil
	}

	result.PolicyVersion = policyVersionString(spec.Version)
	result.FallbackAgent = normalizePolicyAgent(spec.Defaults.FallbackAgent)

	for _, rule := range spec.Rules {
		if !policyWhenMatches(rule.When, ctx) {
			continue
		}
		result.Matched = true
		result.Prefer = normalizePolicyAgent(rule.Prefer)
		result.Secondary = normalizePolicyAgent(rule.Secondary)
		result.Approval = strings.TrimSpace(strings.ToLower(rule.Approval))
		result.Reason = strings.TrimSpace(rule.Reason)
		return result, nil
	}

	result.Reason = "no matching routing rule"
	return result, nil
}

func ResolveApprovalPolicy(db *sql.DB, policyRef string, ctx PolicyMatchContext) (ApprovalPolicyEvaluation, error) {
	result := ApprovalPolicyEvaluation{PolicyRef: strings.TrimSpace(policyRef)}
	if result.PolicyRef == "" {
		return result, nil
	}

	resource, err := store.GetControlPlaneResource(db, "ApprovalPolicy", result.PolicyRef)
	if err != nil {
		return result, err
	}
	if resource == nil {
		return result, nil
	}
	result.PolicyFound = true

	var spec approvalPolicySpec
	if err := json.Unmarshal([]byte(resource.RawSpecJSON), &spec); err != nil {
		result.Reason = "approval policy parse failed"
		return result, nil
	}

	result.PolicyVersion = policyVersionString(spec.Version)
	for _, rule := range spec.Rules {
		if !policyWhenMatches(rule.When, ctx) {
			continue
		}
		result.Matched = true
		result.RequiresHumanApproval = rule.RequiresHumanApproval
		result.RequiredBefore = append([]string(nil), rule.RequiredBefore...)
		result.Reason = strings.TrimSpace(rule.Reason)
		return result, nil
	}

	result.Reason = "no matching approval rule"
	return result, nil
}

func policyWhenMatches(when policyWhen, ctx PolicyMatchContext) bool {
	if value := normalizePolicyValue(when.TaskType); value != "" && value != normalizePolicyValue(ctx.TaskType) {
		return false
	}
	if value := normalizePolicyValue(when.Risk); value != "" && value != normalizePolicyValue(ctx.Risk) {
		return false
	}
	if value := normalizePolicyValue(when.RepoRole); value != "" {
		role := normalizePolicyValue(ctx.RepoRole)
		tier := normalizePolicyValue(ctx.RepoTier)
		if value != role && value != tier {
			return false
		}
	}
	return true
}

func normalizePolicyValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizePolicyAgent(value string) string {
	switch normalizePolicyValue(value) {
	case "claude", "codex":
		return normalizePolicyValue(value)
	default:
		return ""
	}
}

func policyVersionString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		out, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return strings.Trim(strings.TrimSpace(string(out)), "\"")
	}
}
