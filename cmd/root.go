package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "agentctl",
	Short: "Orchestrate Claude Code and Codex CLI sessions",
	Long:  "A CLI tool to inspect Claude Code / Codex CLI sessions, observe rate status, and send instructions via tmux or zellij.",
}

func Execute() {
	rootCmd.Version = Version
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
