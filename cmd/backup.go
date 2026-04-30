package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	backupDBPath       string
	backupDBDir        string
	backupDBKeep       int
	backupDBQuickCheck bool
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage persistent state backups",
}

var backupDBCmd = &cobra.Command{
	Use:   "db",
	Short: "Create a timestamped snapshot of the SQLite state DB",
	RunE:  runBackupDB,
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.AddCommand(backupDBCmd)

	backupDBCmd.Flags().StringVar(&backupDBPath, "db-path", "", "Database file to snapshot (default: agentctl state DB)")
	backupDBCmd.Flags().StringVar(&backupDBDir, "dir", "", "Directory for timestamped snapshots (default: <db dir>/backups)")
	backupDBCmd.Flags().IntVar(&backupDBKeep, "keep", defaultBackupKeepCount, "How many timestamped snapshots to retain")
	backupDBCmd.Flags().BoolVar(&backupDBQuickCheck, "quick-check", true, "Run PRAGMA quick_check before creating the snapshot")
}

func runBackupDB(cmd *cobra.Command, args []string) error {
	dbPath := strings.TrimSpace(backupDBPath)
	if dbPath == "" {
		dbPath = store.DefaultDBPath()
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if backupDBQuickCheck {
		if err := runQuickCheck(db); err != nil {
			return err
		}
	}

	snapshotPath, err := createDatabaseSnapshot(db, dbPath, backupDBDir, backupDBKeep)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Snapshot: %s\n", snapshotPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Latest:   %s\n", filepath.Clean(dbPath)+".bak")
	return nil
}
