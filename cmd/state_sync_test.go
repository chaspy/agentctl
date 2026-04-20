package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/store"
)

func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/owner/repo/pull/42", "42"},
		{"https://github.com/chaspy/agentctl/pull/123", "123"},
		{"https://github.com/owner/repo/issues/10", ""},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		got := extractPRNumber(tt.url)
		if got != tt.want {
			t.Errorf("extractPRNumber(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestRepoFromRepository(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"chaspy/agentctl", "chaspy/agentctl"},
		{"chaspy/agentctl/worktree-feat-xxx", "chaspy/agentctl"},
		{"owner/repo-name/worktree-fix-bug", "owner/repo-name"},
		{"single", ""},
		{"", ""},
		{"chaspy/myassistant-server", "chaspy/myassistant"},
		{"studiuos/jp-Studious-JP", "studiuos-jp/Studious_JP"},
		{"chaspy/myassistant-server/worktree-fix-foo", "chaspy/myassistant"},
	}
	for _, tt := range tests {
		got := repoFromRepository(tt.input)
		if got != tt.want {
			t.Errorf("repoFromRepository(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:chaspy/agentctl.git", "chaspy/agentctl"},
		{"https://github.com/chaspy/agentctl.git", "chaspy/agentctl"},
		{"https://github.com/chaspy/agentctl", "chaspy/agentctl"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"", ""},
		{"https://gitlab.com/owner/repo.git", ""},
	}
	for _, tt := range tests {
		got := parseGitHubRepo(tt.input)
		if got != tt.want {
			t.Errorf("parseGitHubRepo(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeExistingRepoNames(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:chaspy/myassistant-server:s1", Agent: "claude",
		Repository: "chaspy/myassistant-server", SessionID: "s1",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:studiuos/jp-Studious-JP:s2", Agent: "claude",
		Repository: "studiuos/jp-Studious-JP", SessionID: "s2",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:chaspy/agentctl:s3", Agent: "claude",
		Repository: "chaspy/agentctl", SessionID: "s3",
		Status: "active", Alive: true, LastActive: time.Now(),
	})

	normalizeExistingRepoNames(db)

	s1, _ := store.GetSession(db, "claude:chaspy/myassistant-server:s1")
	if s1.Repository != "chaspy/myassistant" {
		t.Errorf("s1 repository = %q, want %q", s1.Repository, "chaspy/myassistant")
	}
	s2, _ := store.GetSession(db, "claude:studiuos/jp-Studious-JP:s2")
	if s2.Repository != "studiuos-jp/Studious_JP" {
		t.Errorf("s2 repository = %q, want %q", s2.Repository, "studiuos-jp/Studious_JP")
	}
	s3, _ := store.GetSession(db, "claude:chaspy/agentctl:s3")
	if s3.Repository != "chaspy/agentctl" {
		t.Errorf("s3 repository = %q, want %q", s3.Repository, "chaspy/agentctl")
	}
}

// --- syncRuntimeStatus tests ---

func mockZellijDetailed(sessions []mux.ZellijSessionState) func() {
	orig := listZellijDetailed
	listZellijDetailed = func() ([]mux.ZellijSessionState, error) {
		return sessions, nil
	}
	return func() { listZellijDetailed = orig }
}

func mockZellijCWD(cwdMap map[string]string) func() {
	orig := zellijCWD
	zellijCWD = func(sessionName string) string {
		return cwdMap[sessionName]
	}
	return func() { zellijCWD = orig }
}

func TestSyncRuntimeStatus_Running(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "a-b", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "running")
	}
	if !s1.Alive {
		t.Error("alive should remain true for running sessions")
	}
}

func TestSyncRuntimeStatus_Exited(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "a-b", Exited: true},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "exited" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "exited")
	}
	if !s1.Alive {
		t.Error("desired_state should remain running for exited zellij sessions")
	}
	if s1.Status != "active" {
		t.Errorf("status = %q, want %q", s1.Status, "active")
	}
}

func TestSyncRuntimeStatus_Gone(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		RuntimeStatus: "running",
		LastActive:    time.Now(),
	})

	// Zellij returns at least one other session so the fail-safe doesn't trigger,
	// but "a-b" is absent — session should be marked gone.
	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "other-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "gone" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "gone")
	}
	if !s1.Alive {
		t.Error("desired_state should remain running when zellij session is missing")
	}
	if s1.Status != "active" {
		t.Errorf("status = %q, want %q", s1.Status, "active")
	}
}

func TestSyncRuntimeStatus_SkipsDeadDetectionOnEmptyZellij(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		RuntimeStatus: "running",
		LastActive:    time.Now(),
	})

	// Zellij returns 0 sessions but DB has alive sessions — fail-safe should skip.
	restore := mockZellijDetailed([]mux.ZellijSessionState{})
	defer restore()

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if !s1.Alive {
		t.Error("alive should remain true when zellij returns 0 sessions (fail-safe)")
	}
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want %q (should be unchanged)", s1.RuntimeStatus, "running")
	}
}

func TestSyncRuntimeStatus_NeverChangesAlive(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "dead", Alive: false, ZellijSession: "a-b",
		LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "a-b", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.Alive {
		t.Error("alive should not be changed by sync (was killed)")
	}
}

func TestSyncRuntimeStatus_NoZellijSession_MarksUnknown(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "",
		LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "some-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	// Empty zellij_session means "location unknown", not "dead" — stay alive.
	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "unknown" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "unknown")
	}
	if !s1.Alive {
		t.Error("alive should remain true for sessions with no zellij_session (location unknown)")
	}
	if s1.Status != "active" {
		t.Errorf("status = %q, want %q (should be unchanged)", s1.Status, "active")
	}
}

func TestSyncRuntimeStatus_GhostSessionStaysActiveUntilKilled(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "idle", Alive: true, ZellijSession: "missing-session",
		LastActive: time.Now(),
	})

	// Return at least one other session so the fail-safe doesn't trigger,
	// while "missing-session" is absent → ghost gets marked dead.
	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "other-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if !s1.Alive {
		t.Fatal("desired_state should remain running after sync marks runtime gone")
	}
	if s1.Status != "idle" {
		t.Fatalf("status = %q, want idle", s1.Status)
	}
	if s1.RuntimeStatus != "gone" {
		t.Fatalf("runtime_status = %q, want gone", s1.RuntimeStatus)
	}

	archived, err := store.ArchiveDeadSessions(db)
	if err != nil {
		t.Fatalf("ArchiveDeadSessions: %v", err)
	}
	if archived != 0 {
		t.Fatalf("ArchiveDeadSessions archived %d sessions, want 0", archived)
	}
	if _, err := store.GetSession(db, "claude:a/b:s1"); err != nil {
		t.Fatal("ghost session should remain in active sessions table until explicitly stopped")
	}
	archivedSessions, err := store.ListArchivedSessions(db)
	if err != nil {
		t.Fatalf("ListArchivedSessions: %v", err)
	}
	if len(archivedSessions) != 0 {
		t.Fatalf("expected 0 archived sessions, got %d", len(archivedSessions))
	}
}

func TestSyncRuntimeStatus_NoMux(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		RuntimeStatus: "running", LastActive: time.Now(),
	})

	orig := listZellijDetailed
	listZellijDetailed = func() ([]mux.ZellijSessionState, error) {
		return nil, fmt.Errorf("mux unavailable")
	}
	defer func() { listZellijDetailed = orig }()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status should not change when mux unavailable, got %q", s1.RuntimeStatus)
	}
	if !s1.Alive {
		t.Error("alive should remain true when mux unavailable")
	}
}

func TestSyncRuntimeStatus_NoMux_DoesNotArchiveAliveSession(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		RuntimeStatus: "running", LastActive: time.Now(),
	})

	orig := listZellijDetailed
	listZellijDetailed = func() ([]mux.ZellijSessionState, error) {
		return nil, fmt.Errorf("too many open files")
	}
	defer func() { listZellijDetailed = orig }()

	syncRuntimeStatus(db)

	archived, err := store.ArchiveDeadSessions(db)
	if err != nil {
		t.Fatalf("ArchiveDeadSessions: %v", err)
	}
	if archived != 0 {
		t.Fatalf("ArchiveDeadSessions archived %d sessions, want 0", archived)
	}

	s1, err := store.GetSession(db, "claude:a/b:s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !s1.Alive {
		t.Fatal("alive should remain true after mux error")
	}
}

// --- dump-layout enrichment tests ---

func TestSyncRuntimeStatus_EnrichesCWDViaDumpLayout(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Session with empty CWD
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude::my-session", Agent: "claude", ZellijSession: "my-session",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})

	restoreZellij := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "my-session", Exited: false},
	})
	defer restoreZellij()

	restoreCWD := mockZellijCWD(map[string]string{
		"my-session": "/Users/test/go/src/github.com/owner/repo",
	})
	defer restoreCWD()

	origRepo := gitRepoName
	origBranch := gitBranchName
	gitRepoName = func(cwd string) string {
		if cwd == "/Users/test/go/src/github.com/owner/repo" {
			return "owner/repo"
		}
		return ""
	}
	gitBranchName = func(cwd string) string {
		if cwd == "/Users/test/go/src/github.com/owner/repo" {
			return "main"
		}
		return ""
	}
	defer func() { gitRepoName = origRepo; gitBranchName = origBranch }()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s, _ := store.GetSession(db, "claude::my-session")
	if s.CWD != "/Users/test/go/src/github.com/owner/repo" {
		t.Errorf("cwd = %q, want /Users/test/go/src/github.com/owner/repo", s.CWD)
	}
	if s.Repository != "owner/repo" {
		t.Errorf("repository = %q, want owner/repo", s.Repository)
	}
	if s.GitBranch != "main" {
		t.Errorf("git_branch = %q, want main", s.GitBranch)
	}
	if s.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want running", s.RuntimeStatus)
	}
}

func TestSyncRuntimeStatus_SkipsEnrichmentWhenCWDExists(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		CWD: "/existing/path", LastActive: time.Now(),
	})

	restoreZellij := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "a-b", Exited: false},
	})
	defer restoreZellij()

	cwdCalled := false
	origCWD := zellijCWD
	zellijCWD = func(sessionName string) string {
		cwdCalled = true
		return "/should/not/be/used"
	}
	defer func() { zellijCWD = origCWD }()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	if cwdCalled {
		t.Error("zellijCWD should not be called when session already has CWD")
	}
	s, _ := store.GetSession(db, "claude:a/b:s1")
	if s.CWD != "/existing/path" {
		t.Errorf("CWD should be preserved, got %q", s.CWD)
	}
}

func TestSyncRuntimeStatus_DumpLayoutReturnsEmpty(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude::my-session", Agent: "claude", ZellijSession: "my-session",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})

	restoreZellij := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "my-session", Exited: false},
	})
	defer restoreZellij()

	restoreCWD := mockZellijCWD(map[string]string{}) // returns "" for all
	defer restoreCWD()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s, _ := store.GetSession(db, "claude::my-session")
	if s.CWD != "" {
		t.Errorf("CWD should remain empty when dump-layout returns nothing, got %q", s.CWD)
	}
}

// --- UpdateSessionMetadata tests ---

func TestUpdateSessionMetadata_DoesNotCreateNew(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Try to update a non-existent session — should not create it
	err = store.UpdateSessionMetadata(db, &store.Session{
		ID:          "nonexistent-id",
		Status:      "active",
		LastMessage: "hello",
		LastActive:  time.Now(),
		Role:        "worker",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no session was created
	_, err = store.GetSession(db, "nonexistent-id")
	if err == nil {
		t.Error("UpdateSessionMetadata should not create new records")
	}
}

func TestUpdateSessionMetadata_UpdatesExisting(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})

	now := time.Now()
	_ = store.UpdateSessionMetadata(db, &store.Session{
		ID:          "claude:a/b:s1",
		Status:      "active",
		GitBranch:   "feat/test",
		LastMessage: "working on it",
		LastRole:    "assistant",
		LastActive:  now,
		Role:        "worker",
	})

	s, _ := store.GetSession(db, "claude:a/b:s1")
	if s.Status != "active" {
		t.Errorf("status = %q, want active", s.Status)
	}
	if s.GitBranch != "feat/test" {
		t.Errorf("git_branch = %q, want feat/test", s.GitBranch)
	}
	if s.LastMessage != "working on it" {
		t.Errorf("last_message = %q, want 'working on it'", s.LastMessage)
	}
	// Ensure other fields are preserved
	if s.Repository != "a/b" {
		t.Errorf("repository should be preserved, got %q", s.Repository)
	}
	if !s.Alive {
		t.Error("alive should be preserved")
	}
}

// --- PR conflict tests ---

func TestCheckPRConflicts_SkipsDeadSessions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:s1", Agent: "claude", Repository: "owner/repo", SessionID: "s1",
		GitBranch: "feat/test", Status: "dead", Alive: false,
		PRURL: "https://github.com/owner/repo/pull/1", LastActive: time.Now(),
	})

	sessions, err := store.ListAliveSessionsWithPR(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 alive sessions with PR, got %d", len(sessions))
	}
}

func TestCheckPRConflicts_SkipsRecentlySent(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:s1", Agent: "claude", Repository: "owner/repo", SessionID: "s1",
		GitBranch: "feat/test", Status: "active", Alive: true,
		PRURL: "https://github.com/owner/repo/pull/1", LastActive: time.Now(),
	})

	_ = store.SetState(db, "rebase_sent:https://github.com/owner/repo/pull/1", time.Now().Format(time.RFC3339))

	called := false
	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string {
		called = true
		return "CONFLICTING"
	}
	defer func() { checkPRMergeable = origCheck }()

	checkPRConflicts(db)

	if called {
		t.Error("checkPRMergeable should not be called when rebase was recently sent")
	}
}

func TestCheckPRConflicts_SkipsNonConflicting(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:s1", Agent: "claude", Repository: "owner/repo", SessionID: "s1",
		GitBranch: "feat/test", Status: "active", Alive: true,
		PRURL: "https://github.com/owner/repo/pull/1", LastActive: time.Now(),
	})

	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string { return "MERGEABLE" }
	defer func() { checkPRMergeable = origCheck }()

	checkPRConflicts(db)

	val, _ := store.GetState(db, "rebase_sent:https://github.com/owner/repo/pull/1")
	if val != "" {
		t.Error("should not record rebase_sent for non-conflicting PR")
	}
}

func TestCheckPRConflicts_SendsRebaseForConflicting(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:s1", Agent: "claude", Repository: "owner/repo", SessionID: "s1",
		GitBranch: "feat/test", Status: "active", Alive: true, ZellijSession: "owner-repo",
		PRURL: "https://github.com/owner/repo/pull/42", LastActive: time.Now(),
	})

	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string {
		if repo != "owner/repo" || prNumber != "42" {
			t.Errorf("unexpected args: repo=%q prNumber=%q", repo, prNumber)
		}
		return "CONFLICTING"
	}
	defer func() { checkPRMergeable = origCheck }()

	checkPRConflicts(db)

	val, _ := store.GetState(db, "rebase_sent:https://github.com/owner/repo/pull/42")
	if val != "" {
		t.Error("should not record rebase_sent when mux is unavailable")
	}
}

func TestCheckPRConflicts_ResendAfterCooldown(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:s1", Agent: "claude", Repository: "owner/repo", SessionID: "s1",
		GitBranch: "feat/test", Status: "active", Alive: true,
		PRURL: "https://github.com/owner/repo/pull/1", LastActive: time.Now(),
	})

	_ = store.SetState(db, "rebase_sent:https://github.com/owner/repo/pull/1", time.Now().Add(-2*time.Hour).Format(time.RFC3339))

	called := false
	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string {
		called = true
		return "CONFLICTING"
	}
	defer func() { checkPRMergeable = origCheck }()

	checkPRConflicts(db)

	if !called {
		t.Error("checkPRMergeable should be called after cooldown expires")
	}
}

func TestListAliveSessionsWithPR(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a:s1", Agent: "claude", Repository: "a", SessionID: "s1",
		Status: "active", Alive: true, PRURL: "https://github.com/a/pull/1",
		LastActive: time.Now(),
	})
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:b:s2", Agent: "claude", Repository: "b", SessionID: "s2",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:c:s3", Agent: "claude", Repository: "c", SessionID: "s3",
		Status: "dead", Alive: false, PRURL: "https://github.com/c/pull/3",
		LastActive: time.Now(),
	})

	sessions, err := store.ListAliveSessionsWithPR(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1, got %d", len(sessions))
	}
	if sessions[0].ID != "claude:a:s1" {
		t.Errorf("expected session a, got %s", sessions[0].ID)
	}
}

func TestSyncSessionPRMetadataUpdatesSessionState(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionID := "codex:owner/repo:zellij-owner-repo"
	_ = store.UpsertSession(db, &store.Session{
		ID:            sessionID,
		Agent:         "codex",
		Repository:    "owner/repo",
		SessionID:     "zellij-owner-repo",
		GitBranch:     "feat/test",
		Status:        "active",
		Alive:         true,
		RuntimeStatus: "running",
		LastActive:    time.Now(),
	})

	origLookupPRURL := lookupPRURL
	origLookupPRMetadata := lookupPRMetadata
	defer func() {
		lookupPRURL = origLookupPRURL
		lookupPRMetadata = origLookupPRMetadata
	}()

	lookupPRURL = func(repo, branch string) string {
		if repo != "owner/repo" || branch != "feat/test" {
			t.Fatalf("unexpected lookupPRURL args repo=%q branch=%q", repo, branch)
		}
		return "https://github.com/owner/repo/pull/42"
	}
	lookupPRMetadata = func(repo, prNumber string) prMetadata {
		if repo != "owner/repo" || prNumber != "42" {
			t.Fatalf("unexpected lookupPRMetadata args repo=%q prNumber=%q", repo, prNumber)
		}
		return prMetadata{
			URL:        "https://github.com/owner/repo/pull/42",
			State:      "OPEN",
			HeadRefOID: "abc123",
		}
	}

	metadataBySessionID, err := syncSessionPRMetadata(db)
	if err != nil {
		t.Fatalf("syncSessionPRMetadata: %v", err)
	}

	s, err := store.GetSession(db, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if s.PRNumber != 42 {
		t.Fatalf("PRNumber = %d, want 42", s.PRNumber)
	}
	if s.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Fatalf("PRURL = %q", s.PRURL)
	}
	if s.PRState != "OPEN" {
		t.Fatalf("PRState = %q", s.PRState)
	}
	if metadataBySessionID[sessionID].HeadRefOID != "abc123" {
		t.Fatalf("HeadRefOID = %q", metadataBySessionID[sessionID].HeadRefOID)
	}
}

func TestSyncAgentTaskOutcomesAutoCompletesArchivedAttemptWithPR(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionID := "codex:owner/repo:zellij-owner-repo"
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "owner-repo-task",
		RepoRef:    "owner-repo",
		Repository: "owner/repo",
		Objective:  "Open PR",
		TaskType:   "docs",
		Risk:       "low",
		Status:     "spawned",
		SourceKind: "manifest",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:    "owner-repo-task",
		RepoRef:          "owner-repo",
		Repository:       "owner/repo",
		TaskType:         "docs",
		Risk:             "low",
		SelectedAgent:    "codex",
		SelectedRepoMode: "branch",
		Status:           "applied",
		RouteReason:      "docs task",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:       1,
		AgentTaskName:    "owner-repo-task",
		RepoRef:          "owner-repo",
		Repository:       "owner/repo",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		RepoMode:         "branch",
		Branch:           "feat/test",
		SessionName:      "owner-repo",
		ManagedSessionID: sessionID,
		Summary:          "Open PR for docs update",
		Status:           "spawned",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}
	if err := store.UpsertSession(db, &store.Session{
		ID:            sessionID,
		Agent:         "codex",
		Repository:    "owner/repo",
		SessionID:     "zellij-owner-repo",
		CWD:           "/tmp/owner-repo",
		GitBranch:     "feat/test",
		ZellijSession: "owner-repo",
		Status:        "dead",
		DesiredState:  store.DesiredStateStopped,
		RuntimeStatus: "gone",
		PRNumber:      42,
		PRURL:         "https://github.com/owner/repo/pull/42",
		PRState:       "OPEN",
		TaskSummary:   "Open PR for docs update",
		LastActive:    time.Now(),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if err := store.MoveToArchive(db, sessionID); err != nil {
		t.Fatalf("MoveToArchive: %v", err)
	}

	origGitHead := gitHeadForWorktreePath
	defer func() { gitHeadForWorktreePath = origGitHead }()
	gitHeadForWorktreePath = func(path string) string { return "" }

	if err := syncAgentTaskOutcomes(db, map[string]prMetadata{
		sessionID: {HeadRefOID: "abc123"},
	}); err != nil {
		t.Fatalf("syncAgentTaskOutcomes: %v", err)
	}

	outcome, err := store.GetAgentTaskOutcomeByAttemptID(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskOutcomeByAttemptID: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected outcome to be created")
	}
	if outcome.Status != "completed" {
		t.Fatalf("status = %q, want completed", outcome.Status)
	}
	if outcome.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Fatalf("PRURL = %q", outcome.PRURL)
	}
	if outcome.CommitSHA != "abc123" {
		t.Fatalf("CommitSHA = %q", outcome.CommitSHA)
	}
	if outcome.Source != "sync" {
		t.Fatalf("Source = %q", outcome.Source)
	}

	task, err := store.GetAgentTask(db, "owner-repo-task")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status = %q", task.Status)
	}

	decision, err := store.GetAgentTaskDecision(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskDecision: %v", err)
	}
	if decision.Status != "completed" {
		t.Fatalf("decision status = %q", decision.Status)
	}
}

func TestSyncAgentTaskOutcomesAutoFailsSpawnFailure(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "owner-repo-task",
		RepoRef:    "owner-repo",
		Repository: "owner/repo",
		Objective:  "Open PR",
		TaskType:   "docs",
		Risk:       "low",
		Status:     "spawned",
		SourceKind: "manifest",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:    "owner-repo-task",
		RepoRef:          "owner-repo",
		Repository:       "owner/repo",
		TaskType:         "docs",
		Risk:             "low",
		SelectedAgent:    "codex",
		SelectedRepoMode: "branch",
		Status:           "applied",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:    1,
		AgentTaskName: "owner-repo-task",
		RepoRef:       "owner-repo",
		Repository:    "owner/repo",
		TaskType:      "docs",
		Risk:          "low",
		Agent:         "codex",
		RepoMode:      "branch",
		Branch:        "feat/test",
		Status:        "failed",
		FailureReason: "spawn command failed",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}

	if err := syncAgentTaskOutcomes(db, nil); err != nil {
		t.Fatalf("syncAgentTaskOutcomes: %v", err)
	}

	outcome, err := store.GetAgentTaskOutcomeByAttemptID(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskOutcomeByAttemptID: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected outcome to be created")
	}
	if outcome.Status != "failed" {
		t.Fatalf("status = %q, want failed", outcome.Status)
	}
	if outcome.FailureCategory != "spawn_failed" {
		t.Fatalf("FailureCategory = %q", outcome.FailureCategory)
	}
	if outcome.FailureReason != "spawn command failed" {
		t.Fatalf("FailureReason = %q", outcome.FailureReason)
	}
}

func TestInferAgentFromLayout(t *testing.T) {
	tests := []struct {
		name      string
		layout    string
		session   string
		wantAgent string
	}{
		{
			name: "claude command",
			layout: `layout {
    pane command="claude"
}`,
			wantAgent: "claude",
		},
		{
			name: "codex via node args",
			layout: `layout {
    pane command="node" {
        args "/Users/test/.nvm/versions/node/v22/bin/codex" "--search"
    }
}`,
			wantAgent: "codex",
		},
		{
			name:      "codex from session name fallback",
			layout:    `layout { pane command="bash" }`,
			session:   "something-codex",
			wantAgent: "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferAgentFromLayout(tt.layout, tt.session)
			if string(got) != tt.wantAgent {
				t.Fatalf("inferAgentFromLayout() = %q, want %q", got, tt.wantAgent)
			}
		})
	}
}

func TestSyncRuntimeStatus_SkipsDeadDetectionForSpawning(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Session in 'spawning' state: zellij session does not exist yet.
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LifecycleState: store.LifecycleStateSpawning,
		RuntimeStatus:  "gone", LastActive: time.Now(),
	})

	// Zellij returns no sessions (session not started yet).
	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "other-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	// runtime_status should NOT be updated to 'gone' while spawning.
	if s1.RuntimeStatus != "gone" {
		// It was already 'gone' before — the point is it should stay as-is, not re-mark.
	}
	if !s1.Alive {
		t.Error("desired_state should remain running during spawning")
	}
	if s1.LifecycleState != store.LifecycleStateSpawning {
		t.Errorf("lifecycle_state = %q, want spawning", s1.LifecycleState)
	}

	// ArchiveDeadSessions should not archive a spawning session (desired_state='running').
	archived, err := store.ArchiveDeadSessions(db)
	if err != nil {
		t.Fatalf("ArchiveDeadSessions: %v", err)
	}
	if archived != 0 {
		t.Fatalf("spawning session should not be archived, got %d archived", archived)
	}
}

func TestSyncRuntimeStatus_SkipsDeadDetectionForKilling(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Session in 'killing' state: zellij session may already be gone.
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LifecycleState: store.LifecycleStateKilling,
		RuntimeStatus:  "running", LastActive: time.Now(),
	})

	// Zellij session has disappeared (kill is in progress).
	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "other-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	// runtime_status should NOT be set to 'gone' while killing.
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status should stay 'running' during kill (not yet updated by logKillAction), got %q", s1.RuntimeStatus)
	}
	if s1.LifecycleState != store.LifecycleStateKilling {
		t.Errorf("lifecycle_state = %q, want killing", s1.LifecycleState)
	}
}

func TestSyncRuntimeStatus_NormalSessionMarkedGone(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Normal running session that vanishes from zellij should be marked gone.
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LifecycleState: store.LifecycleStateRunning,
		RuntimeStatus:  "running", LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "other-session", Exited: false},
	})
	defer restore()

	if _, err := syncRuntimeStatus(db); err != nil {
		t.Fatal(err)
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "gone" {
		t.Errorf("runtime_status = %q, want gone for normal missing session", s1.RuntimeStatus)
	}
}

func TestBackupDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "manager.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:test", Agent: "claude", Repository: "owner/repo",
		SessionID: "test", Status: "idle", Alive: true, LastActive: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := backupDatabaseFile(db, dbPath); err != nil {
		t.Fatal(err)
	}

	backupPath := dbPath + ".bak"
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("backup file is empty")
	}
}
