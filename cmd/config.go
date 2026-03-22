package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	configSetMode string
	configSetDesc string
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage per-repository configuration",
	Long:  "Configure operation mode and description for repositories.",
}

var configSetCmd = &cobra.Command{
	Use:   "set <repo>",
	Short: "Set configuration for a repository",
	Long: `Set configuration for a repository.

Use --mode to set operation mode ("main" or "branch").
Use --desc to set a repository description/rules.

At least one of --mode or --desc must be specified.

Examples:
  agentctl config set owner/myrepo --mode main
  agentctl config set owner/myrepo --desc "CLI tool for managing Claude sessions"
  agentctl config set owner/myrepo --mode main --desc "CLI tool"`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <repo>",
	Short: "Get configuration for a repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all repository configurations",
	Args:  cobra.NoArgs,
	RunE:  runConfigList,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)

	configSetCmd.Flags().StringVar(&configSetMode, "mode", "", `Operation mode: "main" or "branch"`)
	configSetCmd.Flags().StringVar(&configSetDesc, "desc", "", "Repository description/rules")
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	repo := args[0]

	if configSetMode == "" && configSetDesc == "" {
		return fmt.Errorf("at least one of --mode or --desc must be specified")
	}

	if configSetMode != "" && configSetMode != "main" && configSetMode != "branch" {
		return fmt.Errorf("invalid mode %q: must be \"main\" or \"branch\"", configSetMode)
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if configSetMode != "" {
		if err := store.SetRepoConfig(db, repo, configSetMode); err != nil {
			return fmt.Errorf("set repo mode: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Set %s mode → %s\n", repo, configSetMode)
	}

	if configSetDesc != "" {
		if err := store.SetRepoDescription(db, repo, configSetDesc); err != nil {
			return fmt.Errorf("set repo description: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Set %s description → %s\n", repo, configSetDesc)
	}

	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	repo := args[0]

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	cfg, err := store.GetRepoFullConfig(db, repo)
	if err != nil {
		return fmt.Errorf("get repo config: %w", err)
	}

	if cfg == nil {
		fmt.Printf("%s: mode=branch (default), description=(none)\n", repo)
		return nil
	}

	fmt.Printf("Repository:  %s\n", cfg.Repo)
	fmt.Printf("Mode:        %s\n", cfg.Mode)
	if cfg.Description != "" {
		fmt.Printf("Description: %s\n", cfg.Description)
	} else {
		fmt.Printf("Description: (none)\n")
	}
	fmt.Printf("Updated:     %s\n", cfg.UpdatedAt)
	return nil
}

func runConfigList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	configs, err := store.ListRepoConfigs(db)
	if err != nil {
		return fmt.Errorf("list repo configs: %w", err)
	}

	if len(configs) == 0 {
		fmt.Println("No repository configurations set. Default mode is \"branch\".")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tMODE\tDESCRIPTION\tUPDATED")
	for _, c := range configs {
		desc := c.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Repo, c.Mode, desc, c.UpdatedAt)
	}
	return w.Flush()
}
