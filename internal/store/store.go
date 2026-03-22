package store

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DefaultDBPath returns the default database file path.
func DefaultDBPath() string {
	return filepath.Join(".claude", "manager.db")
}

// Open opens or creates the SQLite database at the given path.
// If path is empty, DefaultDBPath() is used.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultDBPath()
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
