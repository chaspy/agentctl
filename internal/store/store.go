package store

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DefaultDBPath returns the default database file path.
// Priority: AGENTCTL_DB_PATH env var > ~/.agentctl/manager.db > .claude/manager.db (fallback)
func DefaultDBPath() string {
	if p := os.Getenv("AGENTCTL_DB_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "manager.db") // fallback
	}
	return filepath.Join(home, ".agentctl", "manager.db")
}

// migrateOldDB copies the old .claude/manager.db to the new location if needed.
func migrateOldDB(newPath string) {
	if _, err := os.Stat(newPath); err == nil {
		return // new DB already exists
	}
	oldPath := filepath.Join(".claude", "manager.db")
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return // old DB doesn't exist
	}
	dir := filepath.Dir(newPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	os.WriteFile(newPath, data, 0644)
}

// Open opens or creates the SQLite database at the given path.
// If path is empty, DefaultDBPath() is used.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultDBPath()
	}

	// Attempt migration from old location
	if path != ":memory:" {
		migrateOldDB(path)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
