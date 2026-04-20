package controlplane

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildReconcileReportObservesRemoteMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home")
	repoPath := filepath.Join(home, "go", "src", "github.com", "chaspy", "myassistant")
	remotePath := filepath.Join(tmpDir, "remote", "myassistant.git")
	contractPath := filepath.Join(repoPath, ".agent", "repo.yaml")

	runGit(t, "", "init", "--bare", "--initial-branch=main", remotePath)
	runGit(t, "", "init", "--initial-branch=main", repoPath)
	runGit(t, repoPath, "config", "user.name", "AgentCtl Test")
	runGit(t, repoPath, "config", "user.email", "agentctl@example.com")
	runGit(t, repoPath, "remote", "add", "origin", remotePath)

	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# myassistant\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	if err := os.WriteFile(contractPath, []byte("kind: RepoContract\n"), 0o644); err != nil {
		t.Fatalf("WriteFile contract: %v", err)
	}
	runGit(t, repoPath, "add", "README.md", ".agent/repo.yaml")
	runGit(t, repoPath, "commit", "-m", "init")
	runGit(t, repoPath, "push", "-u", "origin", "main")

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

	report, err := BuildReconcileReport(db)
	if err != nil {
		t.Fatalf("BuildReconcileReport: %v", err)
	}
	if report.Summary.RemoteURLsResolved != 1 {
		t.Fatalf("remote URLs resolved = %d, want 1", report.Summary.RemoteURLsResolved)
	}
	if report.Summary.RemoteReachable != 1 {
		t.Fatalf("remote reachable = %d, want 1", report.Summary.RemoteReachable)
	}
	if report.Summary.RemoteDefaultBranchesResolved != 1 {
		t.Fatalf("remote default branches resolved = %d, want 1", report.Summary.RemoteDefaultBranchesResolved)
	}
	if len(report.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(report.Repos))
	}

	repo := report.Repos[0]
	if repo.RemoteSource != "origin" {
		t.Fatalf("remote source = %q, want origin", repo.RemoteSource)
	}
	if repo.RemoteURL != remotePath {
		t.Fatalf("remote URL = %q, want %q", repo.RemoteURL, remotePath)
	}
	if !repo.RemoteReachable {
		t.Fatalf("expected remote to be reachable: %+v", repo)
	}
	if repo.RemoteDefaultBranch != "main" {
		t.Fatalf("remote default branch = %q, want main", repo.RemoteDefaultBranch)
	}
	if repo.NeedsAttention {
		t.Fatalf("expected no attention: %+v", repo)
	}
}

func TestParseGitHubRepoFromRemoteURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/chaspy/myassistant.git", "chaspy/myassistant"},
		{"git@github.com:chaspy/agentctl.git", "chaspy/agentctl"},
		{"ssh://git@github.com/org/repo.git", "org/repo"},
		{"/tmp/local/remote.git", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := parseGitHubRepoFromRemoteURL(tt.url); got != tt.want {
			t.Fatalf("parseGitHubRepoFromRemoteURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
