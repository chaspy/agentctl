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
