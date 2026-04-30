package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunApplyManagedRepoDryRunDoesNotPersist(t *testing.T) {
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
`) + "\n"

	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origApplyFile := applyFile
	origApplyDryRun := applyDryRun
	var out bytes.Buffer
	applyCmd.SetOut(&out)
	t.Cleanup(func() {
		applyFile = origApplyFile
		applyDryRun = origApplyDryRun
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
	applyDryRun = true

	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply: %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("db should not be created in dry-run, stat err = %v", err)
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
	if repo != nil {
		t.Fatal("expected no managed repo to be persisted in dry-run")
	}
}
