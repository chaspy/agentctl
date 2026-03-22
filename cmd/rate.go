package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/spf13/cobra"
)

var rateAgent string

var rateCmd = &cobra.Command{
	Use:   "rate",
	Short: "Show Claude Code / Codex CLI rate information",
	Long:  "Shows the latest locally observable rate-limit information for Claude Code and Codex CLI.",
	RunE:  runRate,
}

func init() {
	rootCmd.AddCommand(rateCmd)
	rateCmd.Flags().StringVar(&rateAgent, "agent", "all", "Filter by agent: all, claude, codex")
}

func runRate(cmd *cobra.Command, args []string) error {
	agents, err := selectedAgents(rateAgent)
	if err != nil {
		return err
	}

	var infos []provider.RateInfo
	for _, agent := range agents {
		switch agent {
		case provider.AgentClaude:
			info, err := provider.ClaudeRate()
			if err != nil {
				if len(agents) == 1 {
					return fmt.Errorf("reading claude rate info: %w", err)
				}
				fmt.Fprintf(os.Stderr, "warning: could not read claude rate info: %v\n", err)
				continue
			}
			infos = append(infos, info)
		case provider.AgentCodex:
			info, err := provider.CodexRate()
			if err != nil {
				if len(agents) == 1 {
					return fmt.Errorf("reading codex rate info: %w", err)
				}
				fmt.Fprintf(os.Stderr, "warning: could not read codex rate info: %v\n", err)
				continue
			}
			infos = append(infos, info)
		}
	}

	if len(infos) == 0 {
		return fmt.Errorf("no rate info available")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tRATE\tUPDATED\tDETAILS\tBURN")
	for _, info := range infos {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			info.Agent,
			emptyFallback(info.Summary),
			formatTimestamp(info.UpdatedAt),
			emptyFallback(info.Details),
			emptyFallback(info.BurnRate),
		)
	}

	return w.Flush()
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func emptyFallback(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
