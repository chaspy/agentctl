package cmd

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

func TestResolveCodexResumeTargetFromJSONL(t *testing.T) {
	origScan := codexResumeScanCodex
	t.Cleanup(func() { codexResumeScanCodex = origScan })

	codexResumeScanCodex = func(maxAge time.Duration) ([]provider.SessionInfo, error) {
		return []provider.SessionInfo{
			{Agent: provider.AgentCodex, SessionID: "old-session", CWD: "/tmp/old", Repository: "owner/old", GitBranch: "main", ModTime: time.Now().Add(-2 * time.Hour)},
			{Agent: provider.AgentCodex, SessionID: "target-session", CWD: "/tmp/target", Repository: "owner/target", GitBranch: "feat/x", ModTime: time.Now().Add(-time.Minute)},
		}, nil
	}

	target, err := resolveCodexResumeTargetFromJSONL("target-session")
	if err != nil {
		t.Fatalf("resolveCodexResumeTargetFromJSONL: %v", err)
	}
	if target.CWD != "/tmp/target" {
		t.Fatalf("cwd = %q, want /tmp/target", target.CWD)
	}
	if target.Repository != "owner/target" {
		t.Fatalf("repository = %q, want owner/target", target.Repository)
	}
	if target.GitBranch != "feat/x" {
		t.Fatalf("branch = %q, want feat/x", target.GitBranch)
	}
}

func TestRunCodexResumeRegistersProtectedSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agentctl.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	db.Close()

	origOpenDB := codexResumeOpenDB
	origScan := codexResumeScanCodex
	origList := codexResumeListZellij
	origStart := codexResumeStart
	origProtected := codexResumeProtected
	origName := codexResumeName
	t.Cleanup(func() {
		codexResumeOpenDB = origOpenDB
		codexResumeScanCodex = origScan
		codexResumeListZellij = origList
		codexResumeStart = origStart
		codexResumeProtected = origProtected
		codexResumeName = origName
	})

	codexResumeOpenDB = func(path string) (*sql.DB, error) {
		return store.Open(dbPath)
	}
	codexResumeScanCodex = func(maxAge time.Duration) ([]provider.SessionInfo, error) {
		return []provider.SessionInfo{
			{Agent: provider.AgentCodex, SessionID: "resume-me", CWD: "/Users/test/go/src/github.com/owner/repo", Repository: "owner/repo", GitBranch: "feature/resume", ModTime: time.Now()},
		}, nil
	}
	codexResumeListZellij = func() ([]string, error) { return nil, nil }

	var gotName, gotCWD, gotSource string
	codexResumeStart = func(sessionName, cwd, sourceSessionID string) error {
		gotName = sessionName
		gotCWD = cwd
		gotSource = sourceSessionID
		return nil
	}

	codexResumeProtected = true
	codexResumeName = "adopt-codex"

	if err := runCodexResume(codexResumeCmd, []string{"resume-me"}); err != nil {
		t.Fatalf("runCodexResume: %v", err)
	}
	if gotName != "adopt-codex" {
		t.Fatalf("sessionName = %q, want adopt-codex", gotName)
	}
	if gotCWD != "/Users/test/go/src/github.com/owner/repo" {
		t.Fatalf("cwd = %q", gotCWD)
	}
	if gotSource != "resume-me" {
		t.Fatalf("sourceSessionID = %q", gotSource)
	}

	verifyDB, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer verifyDB.Close()

	got, err := store.GetSessionBySessionID(verifyDB, "zellij-adopt-codex")
	if err != nil {
		t.Fatalf("GetSessionBySessionID: %v", err)
	}
	if got.Agent != string(provider.AgentCodex) {
		t.Fatalf("agent = %q", got.Agent)
	}
	if got.DesiredState != store.DesiredStateRunning {
		t.Fatalf("desired_state = %q", got.DesiredState)
	}
	if got.LifecycleState != store.LifecycleStateRunning {
		t.Fatalf("lifecycle_state = %q", got.LifecycleState)
	}
	if got.RuntimeStatus != "running" {
		t.Fatalf("runtime_status = %q", got.RuntimeStatus)
	}
	if !got.IsProtected {
		t.Fatalf("expected protected session")
	}
	if got.Repository != "owner/repo" {
		t.Fatalf("repository = %q", got.Repository)
	}
}
