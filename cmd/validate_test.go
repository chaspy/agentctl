package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateManifestFileManagedRepo(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "managed-repo.yaml")
	content := []byte(`
apiVersion: myassistant.dev/v1alpha1
kind: ManagedRepo
metadata:
  name: myassistant
spec:
  repo: github.com/chaspy/myassistant
  tier: control-plane
  role: personal-ops-console
  visibility: private
`)
	if err := os.WriteFile(manifestPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := validateManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("validateManifestFile: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid manifest, got errors: %+v", result.Errors)
	}
	if result.Kind != resourceKindManagedRepo {
		t.Fatalf("kind = %q", result.Kind)
	}
	if result.Name != "myassistant" {
		t.Fatalf("name = %q", result.Name)
	}
}

func TestValidateManifestDirMixedKinds(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"ecosystem.yaml": `
apiVersion: myassistant.dev/v1alpha1
kind: AgentOpsEcosystem
metadata:
  name: chaspy-agentops
spec:
  owner: chaspy
  controlPlane:
    repoRef: agentctl
  console:
    repoRef: myassistant
  managedRepos:
    - myassistant
  selfHosting:
    enabled: true
    policyRef: self-hosting-default
`,
		"self-hosting-policy.yaml": `
apiVersion: myassistant.dev/v1alpha1
kind: SelfHostingPolicy
metadata:
  name: self-hosting-default
spec:
  principle: Self-hosting is allowed. Self-approval is not allowed.
  tiers:
    - name: control-plane
      repos:
        - myassistant
      automation:
        allowPullRequestCreation: true
        allowDirectPush: false
        allowAutoMerge: false
        requireHumanApproval: true
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	results, err := validateManifestDir(tmpDir)
	if err != nil {
		t.Fatalf("validateManifestDir: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, result := range results {
		if !result.Valid {
			t.Fatalf("expected valid manifest for %s, got %+v", result.Path, result.Errors)
		}
	}
}

func TestValidateManifestFileAgentTask(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "agent-task.yaml")
	content := []byte(`
apiVersion: myassistant.dev/v1alpha1
kind: AgentTask
metadata:
  name: agentctl-create-repo-contract
spec:
  repoRef: agentctl
  objective: Add .agent/repo.yaml
  taskType: docs
  risk: low
  desiredOutcome:
    - .agent/repo.yaml exists
`)
	if err := os.WriteFile(manifestPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := validateManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("validateManifestFile: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid AgentTask manifest, got errors: %+v", result.Errors)
	}
	if result.Kind != resourceKindAgentTask {
		t.Fatalf("kind = %q", result.Kind)
	}
	if result.Name != "agentctl-create-repo-contract" {
		t.Fatalf("name = %q", result.Name)
	}
}
