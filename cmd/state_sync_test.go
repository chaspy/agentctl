package cmd

import (
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

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "running")
	}
	if !s1.Alive {
		t.Error("alive should not be changed by sync")
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

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "exited" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "exited")
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
		LastActive:     time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{})
	defer restore()

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "gone" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "gone")
	}
	if !s1.Alive {
		t.Error("alive should not be changed by sync (alive=intent)")
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

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.Alive {
		t.Error("alive should not be changed by sync (was killed)")
	}
}

func TestSyncRuntimeStatus_DoesNotCreateNewFromZellij(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "new-session", Exited: false},
		{Name: "exited-session", Exited: true},
	})
	defer restore()

	syncRuntimeStatus(db)

	all, _ := store.ListSessions(db)
	if len(all) != 0 {
		t.Errorf("expected 0 sessions, got %d — sync should not create new records", len(all))
	}
}

func TestSyncRuntimeStatus_NoZellijSession_MarksGone(t *testing.T) {
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

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "gone" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "gone")
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
		return nil, nil
	}
	defer func() { listZellijDetailed = orig }()

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status should not change when mux unavailable, got %q", s1.RuntimeStatus)
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

	syncRuntimeStatus(db)

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

	syncRuntimeStatus(db)

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

	syncRuntimeStatus(db)

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
