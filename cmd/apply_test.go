package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunApplyManagedRepo(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	manifestPath := filepath.Join(tmpDir, "managed-repo.yaml")

	manifest := strings.TrimSpace(`
apiVersion: myassistant.dev/v1alpha1
kind: ManagedRepo
metadata:
  name: book-assistant
spec:
  repo: github.com/chaspy/book-assistant
  role: product
  visibility: public
  repoContractPath: .agent/repo.yaml
  defaultRoutingPolicyRef: coding-default
  defaultReviewPolicyRef: product-default
  defaultApprovalPolicyRef: default
  defaultBenchmarkPolicyRef: routing-evaluation
`) + "\n"

	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origApplyFile := applyFile
	t.Cleanup(func() {
		applyFile = origApplyFile
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})

	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	applyFile = manifestPath

	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	repo, err := store.GetManagedRepo(db, "book-assistant")
	if err != nil {
		t.Fatalf("GetManagedRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("expected managed repo to be stored")
	}
	if repo.Repository != "chaspy/book-assistant" {
		t.Fatalf("repository = %q, want chaspy/book-assistant", repo.Repository)
	}
	if repo.DefaultRoutingPolicyRef != "coding-default" {
		t.Fatalf("default routing policy = %q", repo.DefaultRoutingPolicyRef)
	}
	if repo.SpecHash == "" {
		t.Fatal("expected spec hash to be populated")
	}
	if repo.RawSpecJSON == "" {
		t.Fatal("expected raw spec json to be populated")
	}
}

func TestRunApplyAgentTask(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	managedRepoPath := filepath.Join(tmpDir, "managed-repo.yaml")
	agentTaskPath := filepath.Join(tmpDir, "agent-task.yaml")

	managedRepoManifest := strings.TrimSpace(`
apiVersion: myassistant.dev/v1alpha1
kind: ManagedRepo
metadata:
  name: agentctl
spec:
  repo: github.com/chaspy/agentctl
  role: control-plane
  visibility: public
`) + "\n"
	if err := os.WriteFile(managedRepoPath, []byte(managedRepoManifest), 0o644); err != nil {
		t.Fatalf("WriteFile(managedRepo): %v", err)
	}

	agentTaskManifest := strings.TrimSpace(`
apiVersion: myassistant.dev/v1alpha1
kind: AgentTask
metadata:
  name: agentctl-create-repo-contract
spec:
  repoRef: agentctl
  objective: "Add .agent/repo.yaml"
  taskType: docs
  risk: low
  contextRefs:
    - agentops-vision
  desiredOutcome:
    - ".agent/repo.yaml exists"
  reviewPolicyRef: control-plane-review
  approvalPolicyRef: self-hosting-approval
  approval:
    requiredBeforeMerge: false
`) + "\n"
	if err := os.WriteFile(agentTaskPath, []byte(agentTaskManifest), 0o644); err != nil {
		t.Fatalf("WriteFile(agentTask): %v", err)
	}

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origApplyFile := applyFile
	t.Cleanup(func() {
		applyFile = origApplyFile
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})

	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	applyFile = managedRepoPath
	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply(managedRepo): %v", err)
	}

	applyFile = agentTaskPath
	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply(agentTask): %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	task, err := store.GetAgentTask(db, "agentctl-create-repo-contract")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected agent task to be stored")
	}
	if task.RepoRef != "agentctl" {
		t.Fatalf("repoRef = %q, want agentctl", task.RepoRef)
	}
	if task.Repository != "chaspy/agentctl" {
		t.Fatalf("repository = %q, want chaspy/agentctl", task.Repository)
	}
	if len(task.ContextRefs) != 1 || task.ContextRefs[0] != "agentops-vision" {
		t.Fatalf("contextRefs mismatch: %+v", task.ContextRefs)
	}
}

func TestRunApplyControlPlaneResource(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	manifestPath := filepath.Join(tmpDir, "routing-policy.yaml")

	manifest := strings.TrimSpace(`
apiVersion: myassistant.dev/v1alpha1
kind: RoutingPolicy
metadata:
  name: control-plane-default
spec:
  version: "2026-04-25"
  rules:
    - prefer: codex
`) + "\n"

	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origApplyFile := applyFile
	t.Cleanup(func() {
		applyFile = origApplyFile
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})

	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	applyFile = manifestPath

	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	resource, err := store.GetControlPlaneResource(db, resourceKindRoutingPolicy, "control-plane-default")
	if err != nil {
		t.Fatalf("GetControlPlaneResource: %v", err)
	}
	if resource == nil {
		t.Fatal("expected control plane resource to be stored")
	}
	if resource.APIVersion != "myassistant.dev/v1alpha1" {
		t.Fatalf("apiVersion = %q", resource.APIVersion)
	}
	if resource.SpecHash == "" {
		t.Fatal("expected spec hash to be populated")
	}
	if resource.RawSpecJSON == "" {
		t.Fatal("expected raw spec json to be populated")
	}
}
