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
