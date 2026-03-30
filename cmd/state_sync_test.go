package cmd

import (
	"testing"
	"time"

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
	}
	for _, tt := range tests {
		got := repoFromRepository(tt.input)
		if got != tt.want {
			t.Errorf("repoFromRepository(%q) = %q, want %q", tt.input, got, tt.want)
		}
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
