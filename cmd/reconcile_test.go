package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildReconcileReportObservedLocalCloneAndRepoContract(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home")
	repoPath := filepath.Join(home, "go", "src", "github.com", "chaspy", "myassistant")
	contractPath := filepath.Join(repoPath, ".agent", "repo.yaml")
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(contractPath, []byte("kind: RepoContract\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:             "myassistant",
		Repository:       "chaspy/myassistant",
		Role:             "personal-ops-console",
		Visibility:       "private",
		RepoContractPath: ".agent/repo.yaml",
		RawSpecJSON:      `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private","repoContractPath":".agent/repo.yaml"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}

	report, err := buildReconcileReport(db)
	if err != nil {
		t.Fatalf("buildReconcileReport: %v", err)
	}
	if report.Mode != "read-only" {
		t.Fatalf("mode = %q", report.Mode)
	}
	if report.Summary.ManagedRepos != 1 {
		t.Fatalf("managed repos = %d, want 1", report.Summary.ManagedRepos)
	}
	if report.Summary.LocalClonesFound != 1 {
		t.Fatalf("local clones found = %d, want 1", report.Summary.LocalClonesFound)
	}
	if report.Summary.RepoContractsFound != 1 {
		t.Fatalf("repo contracts found = %d, want 1", report.Summary.RepoContractsFound)
	}
	if report.Summary.TaskProposals != 0 {
		t.Fatalf("task proposals = %d, want 0", report.Summary.TaskProposals)
	}
	if report.Summary.NeedsAttention != 0 {
		t.Fatalf("needs attention = %d, want 0", report.Summary.NeedsAttention)
	}
	if len(report.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(report.Repos))
	}

	repo := report.Repos[0]
	if !repo.LocalCloneFound {
		t.Fatalf("expected local clone to be found: %+v", repo)
	}
	if !repo.HasRepoContract {
		t.Fatalf("expected repo contract to be found: %+v", repo)
	}
	if repo.Tier != "control-plane" {
		t.Fatalf("tier = %q, want control-plane", repo.Tier)
	}
	if repo.LocalPath != repoPath {
		t.Fatalf("local path = %q, want %q", repo.LocalPath, repoPath)
	}
}

func TestRunReconcileOnceFlagsAttentionForMissingObservedState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "agentctl",
		Repository:  "chaspy/agentctl",
		Role:        "agentops-control-plane",
		Visibility:  "public",
		RawSpecJSON: `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origReadOnly := reconcileOnceReadOnly
	origJSON := reconcileOnceJSON
	var out bytes.Buffer
	reconcileOnceCmd.SetOut(&out)
	t.Cleanup(func() {
		reconcileOnceReadOnly = origReadOnly
		reconcileOnceJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	reconcileOnceReadOnly = true
	reconcileOnceJSON = true

	if err := runReconcileOnce(reconcileOnceCmd, nil); err != nil {
		t.Fatalf("runReconcileOnce: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"needsAttention": 1`) {
		t.Fatalf("expected JSON output to include summary attention count, got %q", got)
	}
	if !strings.Contains(got, `"repoContractPath is not configured"`) {
		t.Fatalf("expected JSON output to include missing repo contract issue, got %q", got)
	}
	if !strings.Contains(got, `"taskProposals": 2`) {
		t.Fatalf("expected JSON output to include task proposal count, got %q", got)
	}
	if !strings.Contains(got, `"category": "bootstrap_local_clone"`) {
		t.Fatalf("expected JSON output to include local clone proposal, got %q", got)
	}
}
