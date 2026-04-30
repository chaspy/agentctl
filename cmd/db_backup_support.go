package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
)

const defaultBackupKeepCount = 56

func defaultBackupDirForDB(dbPath string) string {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = store.DefaultDBPath()
	}
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

func runQuickCheck(db *sql.DB) error {
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("running PRAGMA quick_check: %w", err)
	}
	defer rows.Close()

	results := make([]string, 0, 4)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return fmt.Errorf("scanning PRAGMA quick_check: %w", err)
		}
		results = append(results, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading PRAGMA quick_check rows: %w", err)
	}
	if len(results) == 1 && strings.EqualFold(strings.TrimSpace(results[0]), "ok") {
		return nil
	}
	if len(results) == 0 {
		return fmt.Errorf("PRAGMA quick_check returned no rows")
	}
	return fmt.Errorf("PRAGMA quick_check failed: %s", strings.Join(results, "; "))
}

func createDatabaseSnapshot(db *sql.DB, dbPath, backupDir string, keep int) (string, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = store.DefaultDBPath()
	}
	if dbPath == "" || dbPath == ":memory:" {
		return "", fmt.Errorf("backup requires a file-backed database")
	}
	if keep < 0 {
		return "", fmt.Errorf("keep must be >= 0")
	}

	if err := backupDatabaseFile(db, dbPath); err != nil {
		return "", err
	}

	backupDir = strings.TrimSpace(backupDir)
	if backupDir == "" {
		backupDir = defaultBackupDirForDB(dbPath)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}

	snapshotName := fmt.Sprintf("%s.snapshot-%s", filepath.Base(dbPath), nowFunc().UTC().Format("20060102T150405"))
	snapshotPath := filepath.Join(backupDir, snapshotName)
	if err := copyDatabaseFile(dbPath, snapshotPath); err != nil {
		return "", err
	}
	if keep > 0 {
		if err := pruneDatabaseSnapshots(backupDir, filepath.Base(dbPath)+".snapshot-", keep); err != nil {
			return snapshotPath, err
		}
	}
	return snapshotPath, nil
}

func copyDatabaseFile(srcPath, dstPath string) error {
	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("stating database for backup: %w", err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening database for backup: %w", err)
	}
	defer src.Close()

	tmpPath := dstPath + ".tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating backup file: %w", err)
	}
	if _, err := dst.ReadFrom(src); err != nil {
		dst.Close()
		return fmt.Errorf("copying database backup: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("closing backup file: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("installing backup file: %w", err)
	}
	return nil
}

func pruneDatabaseSnapshots(dir, prefix string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading backup directory: %w", err)
	}
	type candidate struct {
		path string
		name string
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasSuffix(name, ".tmp") {
			continue
		}
		candidates = append(candidates, candidate{
			path: filepath.Join(dir, name),
			name: name,
		})
	}
	if len(candidates) <= keep {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].name > candidates[j].name
	})
	for _, candidate := range candidates[keep:] {
		if err := os.Remove(candidate.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing old backup %s: %w", candidate.path, err)
		}
	}
	return nil
}
