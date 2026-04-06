package cmd

import (
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/spf13/cobra"
)

var ccusageWatchCmd = &cobra.Command{
	Use:    "__ccusage-watch",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return provider.RunCCUsageWatcher()
	},
}

func init() {
	rootCmd.AddCommand(ccusageWatchCmd)
}
