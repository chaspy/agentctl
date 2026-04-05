package cmd

import (
	"fmt"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateImportFromZellijCmd = &cobra.Command{
	Use:   "import-from-zellij",
	Short: "Rebuild DB session records from live zellij sessions",
	RunE:  runStateImportFromZellij,
}

func init() {
	stateCmd.AddCommand(stateImportFromZellijCmd)
}

func runStateImportFromZellij(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	count, err := syncRuntimeStatus(db)
	if err != nil {
		return err
	}
	if err := backupDatabaseFile(db, store.DefaultDBPath()); err != nil {
		return err
	}

	fmt.Printf("Imported %d sessions from zellij\n", count)
	return nil
}
