package cmd

import (
	"github.com/spf13/cobra"
)

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Manage persisted state in SQLite",
	Long:  "View and manage the persistent state database used by the AI Manager.",
}

func init() {
	rootCmd.AddCommand(stateCmd)
}
