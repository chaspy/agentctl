package store

import (
	"testing"
	"time"
)

func openTestDB(t *testing.T) *testDB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	return &testDB{db: db, t: t}
}

type testDB struct {
	db interface {
		Close() error
	}
	t *testing.T
}

func TestMigrate(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Running migrate again should be idempotent
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate again: %v", err)
	}
}

func TestSessionCRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := &Session{
		ID:          "claude:chaspy/test:abc123",
		Agent:       "claude",
		Repository:  "chaspy/test",
		SessionID:   "abc123",
		CWD:         "/home/user/test",
		GitBranch:   "main",
		Status:      "active",
		Alive:       true,
		LastMessage: "working on it",
		LastRole:    "assistant",
		LastActive:  time.Now(),
	}

	// Insert
	if err := UpsertSession(db, s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Get
	got, err := GetSession(db, s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Repository != "chaspy/test" || got.Status != "active" || !got.Alive {
		t.Errorf("GetSession mismatch: got repository=%s status=%s alive=%v",
			got.Repository, got.Status, got.Alive)
	}

	// Update (upsert)
	s.Status = "idle"
	s.Alive = false
	if err := UpsertSession(db, s); err != nil {
		t.Fatalf("UpsertSession (update): %v", err)
	}
	got, _ = GetSession(db, s.ID)
	if got.Status != "idle" || got.Alive {
		t.Errorf("after update: status=%s alive=%v", got.Status, got.Alive)
	}

	// List
	list, err := ListSessions(db)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListSessions: expected 1, got %d", len(list))
	}

	// ListByStatus
	list, err = ListSessionsByStatus(db, "idle")
	if err != nil {
		t.Fatalf("ListSessionsByStatus: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListSessionsByStatus(idle): expected 1, got %d", len(list))
	}
	list, _ = ListSessionsByStatus(db, "active")
	if len(list) != 0 {
		t.Errorf("ListSessionsByStatus(active): expected 0, got %d", len(list))
	}

	// Delete
	if err := DeleteSession(db, s.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	list, _ = ListSessions(db)
	if len(list) != 0 {
		t.Errorf("after delete: expected 0, got %d", len(list))
	}
}

func TestTaskCRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a session first (for FK)
	UpsertSession(db, &Session{
		ID: "claude:test:s1", Agent: "claude", Repository: "test", SessionID: "s1",
		Status: "active", LastActive: time.Now(),
	})

	task := &Task{
		SessionID:   "claude:test:s1",
		Description: "implement feature X",
		Status:      "pending",
	}
	if err := CreateTask(db, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == 0 {
		t.Error("CreateTask: expected non-zero ID")
	}

	// Get active tasks
	active, err := GetActiveTasks(db)
	if err != nil {
		t.Fatalf("GetActiveTasks: %v", err)
	}
	if len(active) != 1 || active[0].Description != "implement feature X" {
		t.Errorf("GetActiveTasks: unexpected result: %+v", active)
	}

	// Complete task
	if err := UpdateTaskStatus(db, task.ID, "completed", "PR #123 merged"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	active, _ = GetActiveTasks(db)
	if len(active) != 0 {
		t.Errorf("after complete: expected 0 active, got %d", len(active))
	}

	// Get tasks for session
	tasks, err := GetTasksForSession(db, "claude:test:s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != "completed" || tasks[0].Result != "PR #123 merged" {
		t.Errorf("GetTasksForSession: unexpected %+v", tasks)
	}
	if tasks[0].CompletedAt == nil {
		t.Error("completed task should have CompletedAt set")
	}
}

func TestActionLog(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a1 := &Action{ActionType: "send", SessionID: "s1", Content: "do X", Result: "ok"}
	a2 := &Action{ActionType: "note", Content: "rate limit approaching"}
	if err := LogAction(db, a1); err != nil {
		t.Fatal(err)
	}
	if err := LogAction(db, a2); err != nil {
		t.Fatal(err)
	}

	recent, err := GetRecentActions(db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(recent))
	}

	forSession, err := GetActionsForSession(db, "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(forSession) != 1 {
		t.Errorf("expected 1 action for s1, got %d", len(forSession))
	}
}

func TestListActiveSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create two sessions: one active, one archived
	_ = UpsertSession(db, &Session{
		ID: "claude:proj-a:s1", Agent: "claude", Repository: "proj-a", SessionID: "s1",
		Status: "idle", LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:proj-b:s2", Agent: "claude", Repository: "proj-b", SessionID: "s2",
		Status: "idle", LastActive: time.Now(),
	})

	// Archive one
	if err := ArchiveSession(db, "claude:proj-b:s2"); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	// ListActiveSessions should only return the non-archived one
	active, err := ListActiveSessions(db)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active session, got %d", len(active))
	}
	if active[0].ID != "claude:proj-a:s1" {
		t.Errorf("expected proj-a, got %s", active[0].Repository)
	}

	// ListSessions should return both
	all, err := ListSessions(db)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 total sessions, got %d", len(all))
	}

	// Verify archived flag
	got, _ := GetSession(db, "claude:proj-b:s2")
	if !got.Archived {
		t.Error("expected session to be archived")
	}
}

func TestUpsertPreservesArchived(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create and archive a session
	_ = UpsertSession(db, &Session{
		ID: "claude:test:s1", Agent: "claude", Repository: "test", SessionID: "s1",
		Status: "idle", LastActive: time.Now(),
	})
	_ = ArchiveSession(db, "claude:test:s1")

	// Upsert again (simulating a sync) — archived should be preserved
	_ = UpsertSession(db, &Session{
		ID: "claude:test:s1", Agent: "claude", Repository: "test", SessionID: "s1",
		Status: "active", Alive: true, LastActive: time.Now(),
	})

	got, _ := GetSession(db, "claude:test:s1")
	if !got.Archived {
		t.Error("UpsertSession should preserve archived=1")
	}
	if got.Status != "active" {
		t.Errorf("expected status=active, got %s", got.Status)
	}
}

func TestMarkStaleSessionsDead(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create three sessions, two alive
	_ = UpsertSession(db, &Session{
		ID: "claude:a:s1", Agent: "claude", Repository: "a", SessionID: "s1",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:b:s2", Agent: "claude", Repository: "b", SessionID: "s2",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:c:s3", Agent: "claude", Repository: "c", SessionID: "s3",
		Status: "dead", Alive: false, LastActive: time.Now(),
	})

	// Only s1 was found in the scan
	if err := MarkStaleSessionsDead(db, []string{"claude:a:s1"}); err != nil {
		t.Fatalf("MarkStaleSessionsDead: %v", err)
	}

	// s2 should now be dead (was alive but not scanned)
	s2, _ := GetSession(db, "claude:b:s2")
	if s2.Alive || s2.Status != "dead" {
		t.Errorf("s2: expected alive=false status=dead, got alive=%v status=%s", s2.Alive, s2.Status)
	}

	// s1 should still be alive
	s1, _ := GetSession(db, "claude:a:s1")
	if !s1.Alive {
		t.Error("s1 should still be alive")
	}

	// s3 was already dead, should remain dead
	s3, _ := GetSession(db, "claude:c:s3")
	if s3.Status != "dead" {
		t.Errorf("s3 should remain dead, got %s", s3.Status)
	}
}

func TestFindSessionByRepository(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = UpsertSession(db, &Session{
		ID: "claude:user/repo-a:s1", Agent: "claude", Repository: "user/repo-a", SessionID: "s1",
		Status: "active", LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:user/repo-b:s2", Agent: "claude", Repository: "user/repo-b", SessionID: "s2",
		Status: "idle", LastActive: time.Now(),
	})

	// Search for "repo-a"
	results, err := FindSessionByRepository(db, "repo-a")
	if err != nil {
		t.Fatalf("FindSessionByProject: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'repo-a', got %d", len(results))
	}

	// Search for "user" should return both
	results, err = FindSessionByRepository(db, "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'user', got %d", len(results))
	}

	// Search for nonexistent
	results, _ = FindSessionByRepository(db, "nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'nonexistent', got %d", len(results))
	}
}

func TestManagerState(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Get non-existent key
	val, err := GetState(db, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}

	// Set
	if err := SetState(db, "status_last_run", "1234567890"); err != nil {
		t.Fatal(err)
	}
	val, _ = GetState(db, "status_last_run")
	if val != "1234567890" {
		t.Errorf("expected 1234567890, got %q", val)
	}

	// Update
	if err := SetState(db, "status_last_run", "9999999999"); err != nil {
		t.Fatal(err)
	}
	val, _ = GetState(db, "status_last_run")
	if val != "9999999999" {
		t.Errorf("expected 9999999999, got %q", val)
	}

	// AllState
	SetState(db, "foo", "bar")
	all, err := AllState(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 keys, got %d", len(all))
	}

	// Delete
	if err := DeleteState(db, "foo"); err != nil {
		t.Fatal(err)
	}
	all, _ = AllState(db)
	if len(all) != 1 {
		t.Errorf("expected 1 key after delete, got %d", len(all))
	}
}

func TestMoveToArchive(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a session
	_ = UpsertSession(db, &Session{
		ID: "claude:test:s1", Agent: "claude", Repository: "test", SessionID: "s1",
		Status: "dead", Alive: false, LastActive: time.Now(),
		GitBranch: "feat/x", TaskSummary: "do X",
	})

	// Move to archive
	if err := MoveToArchive(db, "claude:test:s1"); err != nil {
		t.Fatalf("MoveToArchive: %v", err)
	}

	// Should not be in sessions anymore
	_, err = GetSession(db, "claude:test:s1")
	if err == nil {
		t.Error("expected session to be gone from sessions table")
	}

	// Should be in archive
	archived, err := ListArchivedSessions(db)
	if err != nil {
		t.Fatalf("ListArchivedSessions: %v", err)
	}
	if len(archived) != 1 {
		t.Fatalf("expected 1 archived session, got %d", len(archived))
	}
	if archived[0].ID != "claude:test:s1" || archived[0].TaskSummary != "do X" {
		t.Errorf("archived session mismatch: %+v", archived[0])
	}
}

func TestArchiveDeadSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create sessions: active, idle, dead, error
	_ = UpsertSession(db, &Session{
		ID: "claude:a:s1", Agent: "claude", Repository: "a", SessionID: "s1",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:b:s2", Agent: "claude", Repository: "b", SessionID: "s2",
		Status: "idle", Alive: true, LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:c:s3", Agent: "claude", Repository: "c", SessionID: "s3",
		Status: "dead", Alive: false, LastActive: time.Now(),
	})
	_ = UpsertSession(db, &Session{
		ID: "claude:d:s4", Agent: "claude", Repository: "d", SessionID: "s4",
		Status: "error", Alive: false, LastActive: time.Now(),
	})

	// Archive dead/error sessions
	count, err := ArchiveDeadSessions(db)
	if err != nil {
		t.Fatalf("ArchiveDeadSessions: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 archived, got %d", count)
	}

	// Active sessions should remain
	sessions, _ := ListSessions(db)
	if len(sessions) != 2 {
		t.Errorf("expected 2 active sessions, got %d", len(sessions))
	}

	// Archive should have 2
	archived, _ := ListArchivedSessions(db)
	if len(archived) != 2 {
		t.Errorf("expected 2 archived sessions, got %d", len(archived))
	}

	// GetArchivedSessionCount
	archCount, err := GetArchivedSessionCount(db)
	if err != nil {
		t.Fatal(err)
	}
	if archCount != 2 {
		t.Errorf("expected archive count 2, got %d", archCount)
	}
}

func TestListAllSessionsWithArchive(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create active session
	_ = UpsertSession(db, &Session{
		ID: "claude:a:s1", Agent: "claude", Repository: "a", SessionID: "s1",
		Status: "active", Alive: true, LastActive: time.Now(),
	})
	// Create and archive a dead session
	_ = UpsertSession(db, &Session{
		ID: "claude:b:s2", Agent: "claude", Repository: "b", SessionID: "s2",
		Status: "dead", Alive: false, LastActive: time.Now().Add(-time.Hour),
	})
	_ = MoveToArchive(db, "claude:b:s2")

	// ListAllSessionsWithArchive should return both
	all, err := ListAllSessionsWithArchive(db)
	if err != nil {
		t.Fatalf("ListAllSessionsWithArchive: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 total sessions, got %d", len(all))
	}

	// ListActiveSessions should return only 1
	active, _ := ListActiveSessions(db)
	if len(active) != 1 {
		t.Errorf("expected 1 active session, got %d", len(active))
	}
}

func TestMigrationArchivesExisting(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// After migration V9, existing dead/error sessions should have been moved.
	// Since we start fresh, verify the archive table exists and is queryable.
	count, err := GetArchivedSessionCount(db)
	if err != nil {
		t.Fatalf("sessions_archive table should exist: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 archived in fresh db, got %d", count)
	}
}

func TestRepoConfigCRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Get non-existent repo
	mode, err := GetRepoConfig(db, "chaspy/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		t.Errorf("expected empty, got %q", mode)
	}

	// Set mode
	if err := SetRepoConfig(db, "owner/myrepo", "main"); err != nil {
		t.Fatal(err)
	}
	mode, _ = GetRepoConfig(db, "owner/myrepo")
	if mode != "main" {
		t.Errorf("expected main, got %q", mode)
	}

	// Update mode
	if err := SetRepoConfig(db, "owner/myrepo", "branch"); err != nil {
		t.Fatal(err)
	}
	mode, _ = GetRepoConfig(db, "owner/myrepo")
	if mode != "branch" {
		t.Errorf("expected branch, got %q", mode)
	}

	// Set description
	if err := SetRepoDescription(db, "owner/myrepo", "CLI tool for managing Claude sessions"); err != nil {
		t.Fatal(err)
	}
	desc, err := GetRepoDescription(db, "owner/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "CLI tool for managing Claude sessions" {
		t.Errorf("expected description, got %q", desc)
	}

	// Verify mode was not overwritten by SetRepoDescription
	mode, _ = GetRepoConfig(db, "owner/myrepo")
	if mode != "branch" {
		t.Errorf("SetRepoDescription should not change mode, got %q", mode)
	}

	// GetRepoFullConfig
	cfg, err := GetRepoFullConfig(db, "owner/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Mode != "branch" || cfg.Description != "CLI tool for managing Claude sessions" {
		t.Errorf("GetRepoFullConfig: mode=%q desc=%q", cfg.Mode, cfg.Description)
	}

	// GetRepoFullConfig for non-existent repo
	cfg, err = GetRepoFullConfig(db, "chaspy/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("expected nil for nonexistent repo, got %+v", cfg)
	}

	// SetRepoDescription for new repo (should create with default mode)
	if err := SetRepoDescription(db, "chaspy/new-repo", "A new repo"); err != nil {
		t.Fatal(err)
	}
	mode, _ = GetRepoConfig(db, "chaspy/new-repo")
	if mode != "branch" {
		t.Errorf("new repo should have default mode 'branch', got %q", mode)
	}
	desc, _ = GetRepoDescription(db, "chaspy/new-repo")
	if desc != "A new repo" {
		t.Errorf("expected 'A new repo', got %q", desc)
	}

	// GetRepoDescription for non-existent repo
	desc, err = GetRepoDescription(db, "chaspy/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "" {
		t.Errorf("expected empty for nonexistent, got %q", desc)
	}

	// List (should include description)
	if err := SetRepoConfig(db, "org/my-project", "branch"); err != nil {
		t.Fatal(err)
	}
	configs, err := ListRepoConfigs(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(configs))
	}
	// Verify description is returned in list
	for _, c := range configs {
		if c.Repo == "owner/myrepo" && c.Description != "CLI tool for managing Claude sessions" {
			t.Errorf("list: expected description for agentctl, got %q", c.Description)
		}
	}

	// Delete
	if err := DeleteRepoConfig(db, "owner/myrepo"); err != nil {
		t.Fatal(err)
	}
	configs, _ = ListRepoConfigs(db)
	if len(configs) != 2 {
		t.Errorf("expected 2 configs after delete, got %d", len(configs))
	}
}
