package controlplane

import (
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestResolveRoutingPolicyMatchesTier(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        "RoutingPolicy",
		Name:        "control-plane-default",
		RawSpecJSON: `{"version":"2026-04-25","defaults":{"fallbackAgent":"claude"},"rules":[{"when":{"repoRole":"control-plane","risk":"low"},"prefer":"codex","secondary":"claude","reason":"low-risk control-plane work"}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}

	result, err := ResolveRoutingPolicy(db, "control-plane-default", PolicyMatchContext{
		TaskType: "docs",
		Risk:     "low",
		RepoRole: "personal-ops-console",
		RepoTier: "control-plane",
	})
	if err != nil {
		t.Fatalf("ResolveRoutingPolicy: %v", err)
	}

	if !result.PolicyFound || !result.Matched {
		t.Fatalf("expected policy match, got %+v", result)
	}
	if result.Prefer != "codex" {
		t.Fatalf("prefer = %q", result.Prefer)
	}
	if result.Secondary != "claude" {
		t.Fatalf("secondary = %q", result.Secondary)
	}
	if result.PolicyVersion != "2026-04-25" {
		t.Fatalf("policyVersion = %q", result.PolicyVersion)
	}
}

func TestResolveApprovalPolicyMatchesLowRiskDocs(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        "ApprovalPolicy",
		Name:        "default",
		RawSpecJSON: `{"version":"2026-04-25","rules":[{"when":{"risk":"high"},"requiresHumanApproval":true},{"when":{"taskType":"docs","risk":"low"},"requiresHumanApproval":false,"reason":"docs can proceed"}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}

	result, err := ResolveApprovalPolicy(db, "default", PolicyMatchContext{
		TaskType: "docs",
		Risk:     "low",
	})
	if err != nil {
		t.Fatalf("ResolveApprovalPolicy: %v", err)
	}

	if !result.PolicyFound || !result.Matched {
		t.Fatalf("expected approval policy match, got %+v", result)
	}
	if result.RequiresHumanApproval {
		t.Fatalf("requiresHumanApproval = true, want false")
	}
	if result.PolicyVersion != "2026-04-25" {
		t.Fatalf("policyVersion = %q", result.PolicyVersion)
	}
	if result.Reason != "docs can proceed" {
		t.Fatalf("reason = %q", result.Reason)
	}
}
