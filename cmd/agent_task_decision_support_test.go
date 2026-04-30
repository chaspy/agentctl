package cmd

import (
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildAgentTaskDecisionUsesRoutingPolicy(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:                    "agentctl",
		Repository:              "chaspy/agentctl",
		Role:                    "agentops-control-plane",
		Visibility:              "public",
		DefaultRoutingPolicyRef: "control-plane-default",
		RawSpecJSON:             `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public","defaultRoutingPolicyRef":"control-plane-default"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        "RoutingPolicy",
		Name:        "control-plane-default",
		RawSpecJSON: `{"version":"2026-04-25","defaults":{"fallbackAgent":"claude"},"rules":[{"when":{"repoRole":"control-plane","risk":"low"},"prefer":"codex","secondary":"claude","reason":"low-risk control-plane work prefers codex"}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}

	origLookup := lookupExecutable
	origClaudeRate := claudeRateFn
	origCodexRate := codexRateFn
	t.Cleanup(func() {
		lookupExecutable = origLookup
		claudeRateFn = origClaudeRate
		codexRateFn = origCodexRate
	})
	lookupExecutable = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	claudeRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentClaude, Summary: "allowed", RemainingPct: 80}, nil
	}
	codexRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentCodex, Summary: "available", RemainingPct: 60}, nil
	}

	task := &store.AgentTask{
		Name:       "agentctl-routing-policy-smoke",
		RepoRef:    "agentctl",
		Repository: "chaspy/agentctl",
		TaskType:   "docs",
		Risk:       "low",
	}

	decision, err := buildAgentTaskDecision(db, task)
	if err != nil {
		t.Fatalf("buildAgentTaskDecision: %v", err)
	}

	if decision.SelectedAgent != "codex" {
		t.Fatalf("selected_agent = %q", decision.SelectedAgent)
	}
	if decision.SelectionMode != "policy" {
		t.Fatalf("selection_mode = %q", decision.SelectionMode)
	}
	if decision.PolicyVersion != "2026-04-25" {
		t.Fatalf("policy_version = %q", decision.PolicyVersion)
	}
	if decision.RouteReason == "" || !strings.Contains(decision.RouteReason, "routing policy control-plane-default") {
		t.Fatalf("route_reason = %q", decision.RouteReason)
	}
}

func TestBuildAgentTaskDecisionFallsBackToPolicySecondary(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:                    "agentctl",
		Repository:              "chaspy/agentctl",
		Role:                    "agentops-control-plane",
		Visibility:              "public",
		DefaultRoutingPolicyRef: "control-plane-default",
		RawSpecJSON:             `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public","defaultRoutingPolicyRef":"control-plane-default"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        "RoutingPolicy",
		Name:        "control-plane-default",
		RawSpecJSON: `{"version":"2026-04-25","defaults":{"fallbackAgent":"claude"},"rules":[{"when":{"repoRole":"control-plane","risk":"low"},"prefer":"codex","secondary":"claude","reason":"low-risk control-plane work prefers codex"}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}

	origLookup := lookupExecutable
	origClaudeRate := claudeRateFn
	origCodexRate := codexRateFn
	t.Cleanup(func() {
		lookupExecutable = origLookup
		claudeRateFn = origClaudeRate
		codexRateFn = origCodexRate
	})
	lookupExecutable = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	claudeRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentClaude, Summary: "allowed", RemainingPct: 80}, nil
	}
	codexRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentCodex, Summary: "RATE LIMITED (resets 15:00)", RemainingPct: 0}, nil
	}

	task := &store.AgentTask{
		Name:       "agentctl-routing-policy-secondary-smoke",
		RepoRef:    "agentctl",
		Repository: "chaspy/agentctl",
		TaskType:   "docs",
		Risk:       "low",
	}

	decision, err := buildAgentTaskDecision(db, task)
	if err != nil {
		t.Fatalf("buildAgentTaskDecision: %v", err)
	}

	if decision.SelectedAgent != "claude" {
		t.Fatalf("selected_agent = %q", decision.SelectedAgent)
	}
	if decision.SelectionMode != "policy" {
		t.Fatalf("selection_mode = %q", decision.SelectionMode)
	}
	if decision.RouteReason == "" || !strings.Contains(decision.RouteReason, "secondary") {
		t.Fatalf("route_reason = %q", decision.RouteReason)
	}
}
