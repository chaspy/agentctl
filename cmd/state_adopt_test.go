package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/store"
)

func withStateAdoptTestDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

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

	return dbPath
}

func stubStateAdoptDiscovery(t *testing.T) {
	t.Helper()

	origList := listZellijDetailed
	origDump := zellijDumpLayout
	origRepo := gitRepoName
	origBranch := gitBranchName
	t.Cleanup(func() {
		listZellijDetailed = origList
		zellijDumpLayout = origDump
		gitRepoName = origRepo
		gitBranchName = origBranch
	})

	listZellijDetailed = func() ([]mux.ZellijSessionState, error) {
		return []mux.ZellijSessionState{
			{Name: "atama-codex-1733", Exited: false},
		}, nil
	}
	zellijDumpLayout = func(sessionName string) (string, error) {
		return `layout {
    pane command="node" {
        args "/Users/test/.nvm/versions/node/v22/bin/codex" "--search"
    }
    cwd "/tmp/atama-agentctl"
}`, nil
	}
	gitRepoName = func(cwd string) string {
		if cwd != "/tmp/atama-agentctl" {
			t.Fatalf("unexpected cwd for gitRepoName: %q", cwd)
		}
		return "chaspy/agentctl"
	}
	gitBranchName = func(cwd string) string {
		if cwd != "/tmp/atama-agentctl" {
			t.Fatalf("unexpected cwd for gitBranchName: %q", cwd)
		}
		return "feat/protected-codex-adopt"
	}
}

func resetStateAdoptFlags(t *testing.T) {
	t.Helper()
	origAgent := adoptAgent
	origRepository := adoptRepository
	origBranch := adoptBranch
	origCWD := adoptCWD
	origExternalSessionID := adoptExternalSessionID
	origPermission := adoptPermission
	origNote := adoptNote
	origDryRun := adoptDryRun
	t.Cleanup(func() {
		adoptAgent = origAgent
		adoptRepository = origRepository
		adoptBranch = origBranch
		adoptCWD = origCWD
		adoptExternalSessionID = origExternalSessionID
		adoptPermission = origPermission
		adoptNote = origNote
		adoptDryRun = origDryRun
	})

	adoptAgent = ""
	adoptRepository = ""
	adoptBranch = ""
	adoptCWD = ""
	adoptExternalSessionID = ""
	adoptPermission = "suggest"
	adoptNote = ""
	adoptDryRun = false
}

func TestRunStateAdoptDryRunDoesNotWriteDB(t *testing.T) {
	dbPath := withStateAdoptTestDB(t)
	stubStateAdoptDiscovery(t)
	resetStateAdoptFlags(t)

	adoptDryRun = true
	adoptNote = "protect before apply"

	var out bytes.Buffer
	stateAdoptCmd.SetOut(&out)

	if err := runStateAdopt(stateAdoptCmd, []string{"1733"}); err != nil {
		t.Fatalf("runStateAdopt: %v", err)
	}

	if !strings.Contains(out.String(), "Protected adoption plan (dry-run)") {
		t.Fatalf("dry-run output = %q", out.String())
	}
	if !strings.Contains(out.String(), "atama-codex-1733") {
		t.Fatalf("dry-run output missing session name: %q", out.String())
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	adoptions, err := store.ListSessionAdoptions(db)
	if err != nil {
		t.Fatalf("ListSessionAdoptions: %v", err)
	}
	if len(adoptions) != 0 {
		t.Fatalf("expected no queued adoptions after dry-run, got %d", len(adoptions))
	}
}

func TestRunStateAdoptQueuesProtectedAdoptionOnly(t *testing.T) {
	dbPath := withStateAdoptTestDB(t)
	stubStateAdoptDiscovery(t)
	resetStateAdoptFlags(t)

	adoptPermission = "auto-read"
	adoptNote = "prepare protected codex adoption"

	var out bytes.Buffer
	stateAdoptCmd.SetOut(&out)

	if err := runStateAdopt(stateAdoptCmd, []string{"1733"}); err != nil {
		t.Fatalf("runStateAdopt: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	queued, err := store.ListQueuedSessionAdoptions(db)
	if err != nil {
		t.Fatalf("ListQueuedSessionAdoptions: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued adoptions = %d, want 1", len(queued))
	}

	got := queued[0]
	if got.Agent != "codex" {
		t.Fatalf("agent = %q", got.Agent)
	}
	if got.ZellijSession != "atama-codex-1733" {
		t.Fatalf("zellij_session = %q", got.ZellijSession)
	}
	if got.TargetPermissionLevel != store.PermissionAutoRead {
		t.Fatalf("target_permission_level = %d", got.TargetPermissionLevel)
	}
	if got.Status != store.AdoptionStatusQueued {
		t.Fatalf("status = %q", got.Status)
	}

	sessions, err := store.ListSessions(db)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions should remain untouched, got %d", len(sessions))
	}
	if !strings.Contains(out.String(), "No live session was imported.") {
		t.Fatalf("command output = %q", out.String())
	}
}
