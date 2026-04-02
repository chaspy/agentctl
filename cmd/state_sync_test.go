package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/process"
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
	if !s1.Alive {
		t.Error("alive should not be changed by sync")
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

	// Zellij has no sessions
	restore := mockZellijDetailed([]mux.ZellijSessionState{})
	defer restore()

	syncRuntimeStatus(db)

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "gone" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "gone")
	}
	// alive must NOT change — it's intent, not runtime
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

	// alive=false (killed), zellij still has it
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
	// alive must remain false — kill intent preserved
	if s1.Alive {
		t.Error("alive should not be changed by sync (was killed)")
	}
}

func TestSyncRuntimeStatus_CreatesNewFromZellij(t *testing.T) {
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

	s1, err := store.GetSession(db, "claude::new-session")
	if err != nil {
		t.Fatalf("expected new session: %v", err)
	}
	if !s1.Alive {
		t.Error("new session should be alive=true")
	}
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "running")
	}

	s2, err := store.GetSession(db, "claude::exited-session")
	if err != nil {
		t.Fatalf("expected exited session: %v", err)
	}
	if !s2.Alive {
		t.Error("new exited session should be alive=true")
	}
	if s2.RuntimeStatus != "exited" {
		t.Errorf("runtime_status = %q, want %q", s2.RuntimeStatus, "exited")
	}
}

func TestSyncRuntimeStatus_DoesNotDuplicateExisting(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		CWD: "/path/to/a/b", LastActive: time.Now(),
	})

	restore := mockZellijDetailed([]mux.ZellijSessionState{
		{Name: "a-b", Exited: false},
	})
	defer restore()

	syncRuntimeStatus(db)

	// Should NOT create a duplicate "claude::a-b" session
	_, err = store.GetSession(db, "claude::a-b")
	if err == nil {
		t.Error("should not create duplicate session for already-tracked zellij session")
	}

	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status = %q, want %q", s1.RuntimeStatus, "running")
	}
	if s1.Repository != "a/b" {
		t.Errorf("repository should be preserved, got %q", s1.Repository)
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

	// Should not change anything when mux is unavailable
	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if s1.RuntimeStatus != "running" {
		t.Errorf("runtime_status should not change when mux unavailable, got %q", s1.RuntimeStatus)
	}
}

func TestInferZellijSession(t *testing.T) {
	muxSet := map[string]bool{
		"agentctl":                 true,
		"agentctl-fix-sync":       true,
		"myassistant":             true,
		"myassistant-fix-tts-bug": true,
	}

	tests := []struct {
		cwd  string
		want string
	}{
		{"/Users/chaspy/go/src/github.com/chaspy/agentctl", "agentctl"},
		{"/Users/chaspy/go/src/github.com/chaspy/myassistant", "myassistant"},
		{"/Users/chaspy/go/src/github.com/chaspy/agentctl/worktree-fix-sync", "agentctl-fix-sync"},
		{"/Users/chaspy/go/src/github.com/chaspy/myassistant/worktree-fix-tts-bug", "myassistant-fix-tts-bug"},
		{"/Users/chaspy/go/src/github.com/chaspy/unknown-repo", ""},
		{"/Users/chaspy/go/src/github.com/chaspy/agentctl/worktree-unknown-branch", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := inferZellijSession(tt.cwd, muxSet)
		if got != tt.want {
			t.Errorf("inferZellijSession(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

// --- parseGitHubRepo tests ---

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

func TestEnrichAllEmptySessions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Session with empty metadata
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude::agentctl-fix-bug", Agent: "claude", ZellijSession: "agentctl-fix-bug",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})
	// Session with existing metadata (should not be touched)
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:owner/existing:s1", Agent: "claude", Repository: "owner/existing",
		SessionID: "s1", CWD: "/existing/path", ZellijSession: "existing",
		Status: "active", Alive: true, LastActive: time.Now(),
	})

	// Mock: claude process running in a worktree that matches the session name
	origProcs := findClaudeProcs
	findClaudeProcs = func() ([]process.ProcessInfo, error) {
		return []process.ProcessInfo{
			{PID: 1234, CWD: "/Users/test/go/src/github.com/owner/agentctl/worktree-fix-bug"},
			{PID: 5678, CWD: "/existing/path"}, // already tracked
		}, nil
	}
	defer func() { findClaudeProcs = origProcs }()

	origRepo := gitRepoName
	origBranch := gitBranchName
	gitRepoName = func(cwd string) string {
		if cwd == "/Users/test/go/src/github.com/owner/agentctl/worktree-fix-bug" {
			return "owner/agentctl"
		}
		return ""
	}
	gitBranchName = func(cwd string) string {
		if cwd == "/Users/test/go/src/github.com/owner/agentctl/worktree-fix-bug" {
			return "fix/bug"
		}
		return ""
	}
	defer func() { gitRepoName = origRepo; gitBranchName = origBranch }()

	enrichAllEmptySessions(db)

	s, _ := store.GetSession(db, "claude::agentctl-fix-bug")
	if s.CWD != "/Users/test/go/src/github.com/owner/agentctl/worktree-fix-bug" {
		t.Errorf("cwd = %q, want worktree path", s.CWD)
	}
	if s.Repository != "owner/agentctl" {
		t.Errorf("repository = %q, want %q", s.Repository, "owner/agentctl")
	}
	if s.GitBranch != "fix/bug" {
		t.Errorf("git_branch = %q, want %q", s.GitBranch, "fix/bug")
	}

	// Existing session should not be modified
	s2, _ := store.GetSession(db, "claude:owner/existing:s1")
	if s2.Repository != "owner/existing" {
		t.Errorf("existing session repository changed to %q", s2.Repository)
	}
}

func TestEnrichAllEmptySessions_NoProcs(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = store.UpsertSession(db, &store.Session{
		ID: "claude::my-session", Agent: "claude", ZellijSession: "my-session",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})

	origProcs := findClaudeProcs
	findClaudeProcs = func() ([]process.ProcessInfo, error) {
		return nil, nil
	}
	defer func() { findClaudeProcs = origProcs }()

	enrichAllEmptySessions(db)

	s, _ := store.GetSession(db, "claude::my-session")
	if s.CWD != "" {
		t.Errorf("cwd should remain empty when no procs, got %q", s.CWD)
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
