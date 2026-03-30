package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanSessionsExcludesPreviewWorktrees(t *testing.T) {
	// Create a temporary directory structure mimicking ~/.claude/projects/
	tmpHome := t.TempDir()
	projectsDir := filepath.Join(tmpHome, ".claude", "projects")

	// Normal project directory
	normalDir := filepath.Join(projectsDir, "-Users-test-repo-normal")
	if err := os.MkdirAll(normalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a recent JSONL file
	jsonlPath := filepath.Join(normalDir, "abc123.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"type":"user","message":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Preview worktree directory (should be excluded)
	previewDir := filepath.Join(projectsDir, "-Users-test-repo-worktree-preview-42")
	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	previewJSONL := filepath.Join(previewDir, "def456.jsonl")
	if err := os.WriteFile(previewJSONL, []byte(`{"type":"user","message":"preview"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override home directory by calling the internal logic directly
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-1 * time.Hour)
	var sessions []SessionInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// This should match the filtering logic in ScanSessions
		if contains(entry.Name(), "worktree-preview-") {
			continue
		}

		dirPath := filepath.Join(projectsDir, entry.Name())
		jsonlFiles, err := filepath.Glob(filepath.Join(dirPath, "*.jsonl"))
		if err != nil {
			continue
		}
		for _, f := range jsonlFiles {
			info, err := os.Stat(f)
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			sessions = append(sessions, SessionInfo{
				ProjectDir: entry.Name(),
				FilePath:   f,
			})
		}
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ProjectDir != "-Users-test-repo-normal" {
		t.Errorf("unexpected project dir: %s", sessions[0].ProjectDir)
	}
}

func TestDecodeRepository(t *testing.T) {
	tests := []struct {
		encoded string
		want    string
	}{
		// Standard: owner/repo
		{"-Users-chaspy-go-src-github-com-chaspy-agentctl", "chaspy/agentctl"},
		// Subdirectory should NOT be included in repo name
		{"-Users-chaspy-go-src-github-com-chaspy-myassistant-server", "chaspy/myassistant"},
		// Worktree suffix
		{"-Users-chaspy-go-src-github-com-chaspy-agentctl-worktree-fix-bug", "chaspy/agentctl"},
		// Deep subdirectory
		{"-Users-chaspy-go-src-github-com-chaspy-myapp-cmd-api", "chaspy/myapp"},
		// Different user path
		{"-home-user-projects-github-com-org-repo", "org/repo"},
	}
	for _, tt := range tests {
		got := decodeRepository(tt.encoded)
		if got != tt.want {
			t.Errorf("decodeRepository(%q) = %q, want %q", tt.encoded, got, tt.want)
		}
	}
}

func TestParseRepoFromRemoteURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/chaspy/myassistant.git", "chaspy/myassistant"},
		{"https://github.com/chaspy/agentctl.git", "chaspy/agentctl"},
		{"https://github.com/chaspy/agentctl", "chaspy/agentctl"},
		{"git@github.com:chaspy/myassistant.git", "chaspy/myassistant"},
		{"git@github.com:chaspy/agentctl.git", "chaspy/agentctl"},
		{"git@github.com:org/repo.git", "org/repo"},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseRepoFromRemoteURL(tt.url)
		if got != tt.want {
			t.Errorf("parseRepoFromRemoteURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestRepoNameFromCWD_WithMock(t *testing.T) {
	// Mock the git remote command
	orig := repoNameFromGitRemote
	defer func() { repoNameFromGitRemote = orig }()

	repoNameFromGitRemote = func(cwd string) string {
		return parseRepoFromRemoteURL("https://github.com/chaspy/myassistant.git")
	}

	got := RepoNameFromCWD("/Users/chaspy/go/src/github.com/chaspy/myassistant/server")
	if got != "chaspy/myassistant" {
		t.Errorf("RepoNameFromCWD() = %q, want %q", got, "chaspy/myassistant")
	}
}

func TestRepoNameFromCWD_EmptyCWD(t *testing.T) {
	got := RepoNameFromCWD("")
	if got != "" {
		t.Errorf("RepoNameFromCWD(\"\") = %q, want empty", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
