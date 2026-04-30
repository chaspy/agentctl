package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunConfigGetShowsMergedRepoProfile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "myassistant",
		Repository:  "chaspy/myassistant",
		Role:        "personal-ops-console",
		Visibility:  "private",
		Notes:       "managed repo note",
		RawSpecJSON: `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private","notes":"managed repo note"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.SetRepoConfig(db, "chaspy/myassistant", "main"); err != nil {
		t.Fatalf("SetRepoConfig: %v", err)
	}
	if err := store.SetRepoAgent(db, "chaspy/myassistant", "codex"); err != nil {
		t.Fatalf("SetRepoAgent: %v", err)
	}
	if err := store.SetRepoDescription(db, "chaspy/myassistant", "legacy override"); err != nil {
		t.Fatalf("SetRepoDescription: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	t.Cleanup(func() {
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	got, err := captureConfigStdout(t, func() error {
		return runConfigGet(configGetCmd, []string{"chaspy/myassistant"})
	})
	if err != nil {
		t.Fatalf("runConfigGet: %v", err)
	}
	if !strings.Contains(got, "PrimarySource:     managed_repo") {
		t.Fatalf("expected managed repo primary source, got %q", got)
	}
	if !strings.Contains(got, "Mode:              main (repo_config)") {
		t.Fatalf("expected repo_config mode override, got %q", got)
	}
	if !strings.Contains(got, "Agent:             codex (repo_config)") {
		t.Fatalf("expected repo_config agent override, got %q", got)
	}
	if !strings.Contains(got, "Description:       legacy override (repo_config)") {
		t.Fatalf("expected repo_config description override, got %q", got)
	}
	if !strings.Contains(got, "ManagedRepoTier:   control-plane") {
		t.Fatalf("expected managed repo tier, got %q", got)
	}
}

func TestRunConfigListShowsMergedRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "agentctl",
		Repository:  "chaspy/agentctl",
		Role:        "agentops-control-plane",
		Visibility:  "public",
		Notes:       "control plane",
		RawSpecJSON: `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public","notes":"control plane"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.SetRepoConfig(db, "chaspy/legacy-only", "main"); err != nil {
		t.Fatalf("SetRepoConfig: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	t.Cleanup(func() {
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	got, err := captureConfigStdout(t, func() error {
		return runConfigList(configListCmd, nil)
	})
	if err != nil {
		t.Fatalf("runConfigList: %v", err)
	}
	if !strings.Contains(got, "chaspy/agentctl") || !strings.Contains(got, "managed_repo") {
		t.Fatalf("expected managed repo row, got %q", got)
	}
	if !strings.Contains(got, "chaspy/legacy-only") || !strings.Contains(got, "repo_config") {
		t.Fatalf("expected repo config row, got %q", got)
	}
	if !strings.Contains(got, "branch(default)") {
		t.Fatalf("expected default mode source for managed repo row, got %q", got)
	}
	if !strings.Contains(got, "main(repo_config)") {
		t.Fatalf("expected repo_config mode source for legacy row, got %q", got)
	}
}

func captureConfigStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	runErr := fn()
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	_ = r.Close()
	return buf.String(), runErr
}
