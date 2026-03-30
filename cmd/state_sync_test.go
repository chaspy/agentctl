package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
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
		// Known corrections
		{"chaspy/myassistant-server", "chaspy/myassistant"},
		{"studiuos/jp-Studious-JP", "studiuos-jp/Studious_JP"},
		// Worktree + correction
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

	// Insert sessions with incorrect repo names
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
	// Correct repo name should not be changed
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:chaspy/agentctl:s3", Agent: "claude",
		Repository: "chaspy/agentctl", SessionID: "s3",
		Status: "active", Alive: true, LastActive: time.Now(),
	})

	normalizeExistingRepoNames(db)

	s1, err := store.GetSession(db, "claude:chaspy/myassistant-server:s1")
	if err != nil {
		t.Fatal(err)
	}
	if s1.Repository != "chaspy/myassistant" {
		t.Errorf("s1 repository = %q, want %q", s1.Repository, "chaspy/myassistant")
	}

	s2, err := store.GetSession(db, "claude:studiuos/jp-Studious-JP:s2")
	if err != nil {
		t.Fatal(err)
	}
	if s2.Repository != "studiuos-jp/Studious_JP" {
		t.Errorf("s2 repository = %q, want %q", s2.Repository, "studiuos-jp/Studious_JP")
	}

	s3, err := store.GetSession(db, "claude:chaspy/agentctl:s3")
	if err != nil {
		t.Fatal(err)
	}
	if s3.Repository != "chaspy/agentctl" {
		t.Errorf("s3 repository = %q, want %q", s3.Repository, "chaspy/agentctl")
	}
}

func TestMarkOrphanedSessionsDead(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Alive session with zellij session that exists
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LastActive: time.Now(),
	})
	// Alive session with zellij session that does NOT exist
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:c/d:s2", Agent: "claude", Repository: "c/d", SessionID: "s2",
		Status: "active", Alive: true, ZellijSession: "c-d",
		LastActive: time.Now(),
	})
	// Alive session without zellij session (ghost — should be marked dead)
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:e/f:s3", Agent: "claude", Repository: "e/f", SessionID: "s3",
		Status: "active", Alive: true,
		LastActive: time.Now(),
	})

	// Mock listMuxSessions to return only "a-b"
	orig := listMuxSessions
	listMuxSessions = func() []string {
		return []string{"a-b"}
	}
	defer func() { listMuxSessions = orig }()

	markOrphanedSessionsDead(db)

	// s1 should still be alive (zellij session exists)
	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if !s1.Alive {
		t.Error("s1 should still be alive")
	}

	// s2 should be dead (zellij session not found)
	s2, _ := store.GetSession(db, "claude:c/d:s2")
	if s2.Alive {
		t.Error("s2 should be dead (orphaned)")
	}
	if s2.Status != "dead" {
		t.Errorf("s2 status = %q, want %q", s2.Status, "dead")
	}

	// s3 should be dead (ghost: no zellij session at all)
	s3, _ := store.GetSession(db, "claude:e/f:s3")
	if s3.Alive {
		t.Error("s3 should be dead (ghost: no zellij session)")
	}
	if s3.Status != "dead" {
		t.Errorf("s3 status = %q, want %q", s3.Status, "dead")
	}
}

func TestMarkOrphanedSessionsDead_NoMux(t *testing.T) {
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

	// Mock listMuxSessions to return nil (no mux available)
	orig := listMuxSessions
	listMuxSessions = func() []string {
		return nil
	}
	defer func() { listMuxSessions = orig }()

	markOrphanedSessionsDead(db)

	// Should not change anything when mux is unavailable
	s1, _ := store.GetSession(db, "claude:a/b:s1")
	if !s1.Alive {
		t.Error("s1 should still be alive when mux is unavailable")
	}
}

func TestValidateAliveWithMux(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Session with zellij_session set
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a/b:s1", Agent: "claude", Repository: "a/b", SessionID: "s1",
		Status: "active", Alive: true, ZellijSession: "a-b",
		LastActive: time.Now(),
	})
	// Session without zellij_session
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:c/d:s2", Agent: "claude", Repository: "c/d", SessionID: "s2",
		Status: "active", Alive: true,
		LastActive: time.Now(),
	})

	muxSet := map[string]bool{"a-b": true}

	// Session with zellij_session that exists in mux -> alive
	s1 := provider.SessionInfo{Agent: "claude", Repository: "a/b", SessionID: "s1"}
	if !validateAliveWithMux(db, s1, muxSet) {
		t.Error("s1 should be alive (zellij session exists in mux)")
	}

	// Session without zellij_session -> not alive
	s2 := provider.SessionInfo{Agent: "claude", Repository: "c/d", SessionID: "s2"}
	if validateAliveWithMux(db, s2, muxSet) {
		t.Error("s2 should not be alive (no zellij_session)")
	}

	// Session not in DB -> not alive
	s3 := provider.SessionInfo{Agent: "claude", Repository: "x/y", SessionID: "s99"}
	if validateAliveWithMux(db, s3, muxSet) {
		t.Error("s3 should not be alive (not in DB)")
	}

	// nil muxSet (no mux available) -> fall back to true
	if !validateAliveWithMux(db, s1, nil) {
		t.Error("should return true when muxSet is nil")
	}
}

func TestCheckPRConflicts_SkipsDeadSessions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a dead session with PR URL
	_ = store.UpsertSession(db, &store.Session{
		ID:         "claude:owner/repo:s1",
		Agent:      "claude",
		Repository: "owner/repo",
		SessionID:  "s1",
		GitBranch:  "feat/test",
		Status:     "dead",
		Alive:      false,
		PRURL:      "https://github.com/owner/repo/pull/1",
		LastActive: time.Now(),
	})

	// ListAliveSessionsWithPR should return nothing for dead sessions
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

	// Create an alive session with PR URL
	_ = store.UpsertSession(db, &store.Session{
		ID:         "claude:owner/repo:s1",
		Agent:      "claude",
		Repository: "owner/repo",
		SessionID:  "s1",
		GitBranch:  "feat/test",
		Status:     "active",
		Alive:      true,
		PRURL:      "https://github.com/owner/repo/pull/1",
		LastActive: time.Now(),
	})

	// Mark as recently sent
	prURL := "https://github.com/owner/repo/pull/1"
	_ = store.SetState(db, "rebase_sent:"+prURL, time.Now().Format(time.RFC3339))

	// Override checkPRMergeable to track if it's called
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
		ID:         "claude:owner/repo:s1",
		Agent:      "claude",
		Repository: "owner/repo",
		SessionID:  "s1",
		GitBranch:  "feat/test",
		Status:     "active",
		Alive:      true,
		PRURL:      "https://github.com/owner/repo/pull/1",
		LastActive: time.Now(),
	})

	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string {
		return "MERGEABLE"
	}
	defer func() { checkPRMergeable = origCheck }()

	checkPRConflicts(db)

	// Should not record any rebase_sent state
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
		ID:             "claude:owner/repo:s1",
		Agent:          "claude",
		Repository:     "owner/repo",
		SessionID:      "s1",
		GitBranch:      "feat/test",
		Status:         "active",
		Alive:          true,
		ZellijSession:  "owner-repo",
		PRURL:          "https://github.com/owner/repo/pull/42",
		LastActive:     time.Now(),
	})

	origCheck := checkPRMergeable
	checkPRMergeable = func(repo, prNumber string) string {
		if repo != "owner/repo" || prNumber != "42" {
			t.Errorf("unexpected args: repo=%q prNumber=%q", repo, prNumber)
		}
		return "CONFLICTING"
	}
	defer func() { checkPRMergeable = origCheck }()

	// checkPRConflicts will fail to resolve mux adapter in test environment,
	// but we can verify the state is not set (since send fails)
	checkPRConflicts(db)

	// Verify that rebase_sent was NOT set (because mux resolution fails in test)
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
		ID:         "claude:owner/repo:s1",
		Agent:      "claude",
		Repository: "owner/repo",
		SessionID:  "s1",
		GitBranch:  "feat/test",
		Status:     "active",
		Alive:      true,
		PRURL:      "https://github.com/owner/repo/pull/1",
		LastActive: time.Now(),
	})

	// Set rebase_sent to 2 hours ago (past the 1-hour cooldown)
	prURL := "https://github.com/owner/repo/pull/1"
	_ = store.SetState(db, "rebase_sent:"+prURL, time.Now().Add(-2*time.Hour).Format(time.RFC3339))

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

	// alive + PR
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:a:s1", Agent: "claude", Repository: "a", SessionID: "s1",
		Status: "active", Alive: true, PRURL: "https://github.com/a/pull/1",
		LastActive: time.Now(),
	})
	// alive + no PR
	_ = store.UpsertSession(db, &store.Session{
		ID: "claude:b:s2", Agent: "claude", Repository: "b", SessionID: "s2",
		Status: "active", Alive: true,
		LastActive: time.Now(),
	})
	// dead + PR
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
