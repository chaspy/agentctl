package cmd

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

func TestParseZellijLayoutResumeSessionID(t *testing.T) {
	layout := `layout {
    pane command="node" {
        args "/Users/test/.nvm/versions/node/v22.14.0/bin/codex" "--search" "resume" "019d681f-db16-73c3-826c-5846d0e88e3e"
    }
}`

	got := parseZellijLayoutResumeSessionID(layout)
	want := "019d681f-db16-73c3-826c-5846d0e88e3e"
	if got != want {
		t.Fatalf("parseZellijLayoutResumeSessionID() = %q, want %q", got, want)
	}
}

func TestRunAdoptZellijRegistersProtectedSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agentctl.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	db.Close()

	origOpenDB := adoptZellijOpenDB
	origList := adoptZellijList
	origLayout := adoptZellijLayout
	origProtected := adoptZellijProtected
	origAgent := adoptZellijAgent
	origRepository := adoptZellijRepository
	origBranch := adoptZellijBranch
	origSessionID := adoptZellijSessionID
	origRepoName := gitRepoName
	origBranchName := gitBranchName
	t.Cleanup(func() {
		adoptZellijOpenDB = origOpenDB
		adoptZellijList = origList
		adoptZellijLayout = origLayout
		adoptZellijProtected = origProtected
		adoptZellijAgent = origAgent
		adoptZellijRepository = origRepository
		adoptZellijBranch = origBranch
		adoptZellijSessionID = origSessionID
		gitRepoName = origRepoName
		gitBranchName = origBranchName
	})

	adoptZellijOpenDB = func(path string) (*sql.DB, error) {
		return store.Open(dbPath)
	}
	adoptZellijList = func() ([]string, error) {
		return []string{"atama-local-app-protected"}, nil
	}
	adoptZellijLayout = func(sessionName string) (string, error) {
		return `layout {
    cwd "/Users/test/go/src/github.com/studiuos-jp/Studious_JP"
    pane command="node" {
        args "/Users/test/.nvm/versions/node/v22.14.0/bin/codex" "--search" "resume" "019d681f-db16-73c3-826c-5846d0e88e3e"
    }
}`, nil
	}
	gitRepoName = func(cwd string) string { return "studiuos-jp/Studious_JP" }
	gitBranchName = func(cwd string) string { return "main" }

	adoptZellijProtected = true
	adoptZellijAgent = "auto"
	adoptZellijRepository = ""
	adoptZellijBranch = ""
	adoptZellijSessionID = ""

	if err := runAdoptZellij(adoptZellijCmd, []string{"atama-local-app-protected"}); err != nil {
		t.Fatalf("runAdoptZellij: %v", err)
	}

	verifyDB, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer verifyDB.Close()

	got, err := store.GetSessionBySessionID(verifyDB, "019d681f-db16-73c3-826c-5846d0e88e3e")
	if err != nil {
		t.Fatalf("GetSessionBySessionID: %v", err)
	}
	if got.Agent != string(provider.AgentCodex) {
		t.Fatalf("agent = %q", got.Agent)
	}
	if got.ZellijSession != "atama-local-app-protected" {
		t.Fatalf("zellij_session = %q", got.ZellijSession)
	}
	if got.Repository != "studiuos-jp/Studious_JP" {
		t.Fatalf("repository = %q", got.Repository)
	}
	if got.GitBranch != "main" {
		t.Fatalf("git_branch = %q", got.GitBranch)
	}
	if got.RuntimeStatus != "running" {
		t.Fatalf("runtime_status = %q", got.RuntimeStatus)
	}
	if got.LifecycleState != store.LifecycleStateRunning {
		t.Fatalf("lifecycle_state = %q", got.LifecycleState)
	}
	if got.DesiredState != store.DesiredStateRunning {
		t.Fatalf("desired_state = %q", got.DesiredState)
	}
	if !got.IsProtected {
		t.Fatalf("expected protected session")
	}
}
