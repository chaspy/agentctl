package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunBackupDBCreatesSnapshotAndLatestBackup(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertSession(db, &store.Session{
		ID: "claude:owner/repo:test", Agent: "claude", Repository: "owner/repo",
		SessionID: "test", Status: "idle", Alive: true, LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	db.Close()

	origNowFunc := nowFunc
	t.Cleanup(func() {
		nowFunc = origNowFunc
		backupDBPath = ""
		backupDBDir = ""
		backupDBKeep = defaultBackupKeepCount
		backupDBQuickCheck = true
	})
	nowFunc = func() time.Time {
		return time.Date(2026, 4, 30, 6, 0, 0, 0, time.UTC)
	}

	var out bytes.Buffer
	backupDBCmd.SetOut(&out)
	backupDBPath = dbPath
	backupDBDir = filepath.Join(filepath.Dir(dbPath), "snapshots")
	backupDBKeep = 2
	backupDBQuickCheck = true

	if err := runBackupDB(backupDBCmd, nil); err != nil {
		t.Fatalf("runBackupDB: %v", err)
	}

	snapshotPath := filepath.Join(backupDBDir, filepath.Base(dbPath)+".snapshot-20260430T060000")
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	if _, err := os.Stat(dbPath + ".bak"); err != nil {
		t.Fatalf("latest backup missing: %v", err)
	}
	if !strings.Contains(out.String(), snapshotPath) {
		t.Fatalf("output = %q, want snapshot path", out.String())
	}
}

func TestCreateDatabaseSnapshotPrunesOldSnapshots(t *testing.T) {
	dbPath := withJobTestDB(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	backupDir := filepath.Join(filepath.Dir(dbPath), "snapshots")
	origNowFunc := nowFunc
	t.Cleanup(func() {
		nowFunc = origNowFunc
	})

	times := []time.Time{
		time.Date(2026, 4, 30, 6, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 30, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC),
	}
	for _, ts := range times {
		nowFunc = func() time.Time { return ts }
		if _, err := createDatabaseSnapshot(db, dbPath, backupDir, 2); err != nil {
			t.Fatalf("createDatabaseSnapshot(%s): %v", ts.Format(time.RFC3339), err)
		}
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var snapshots []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), filepath.Base(dbPath)+".snapshot-") {
			snapshots = append(snapshots, entry.Name())
		}
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want 2 (%v)", len(snapshots), snapshots)
	}
	for _, name := range snapshots {
		if name == filepath.Base(dbPath)+".snapshot-20260430T060000" {
			t.Fatalf("old snapshot should be pruned: %v", snapshots)
		}
	}
}
