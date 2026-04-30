package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/controlplane"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

type managedRepoReport = controlplane.ManagedRepoView

var (
	stateManagedRepoJSON bool
)

var stateManagedRepoCmd = &cobra.Command{
	Use:   "managed-repo",
	Short: "Inspect applied ManagedRepo desired state",
}

var stateManagedRepoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applied ManagedRepo resources from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateManagedRepoList,
}

var stateManagedRepoGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show one applied ManagedRepo resource from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateManagedRepoGet,
}

func init() {
	stateCmd.AddCommand(stateManagedRepoCmd)
	stateManagedRepoCmd.AddCommand(stateManagedRepoListCmd)
	stateManagedRepoCmd.AddCommand(stateManagedRepoGetCmd)
	stateManagedRepoListCmd.Flags().BoolVar(&stateManagedRepoJSON, "json", false, "Output machine-readable JSON")
	stateManagedRepoGetCmd.Flags().BoolVar(&stateManagedRepoJSON, "json", false, "Output machine-readable JSON")
}

func runStateManagedRepoList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	repos, err := controlplane.ListManagedRepoViews(db)
	if err != nil {
		return fmt.Errorf("listing managed repos: %w", err)
	}
	if stateManagedRepoJSON {
		return writeManagedRepoJSON(cmd.OutOrStdout(), repos)
	}

	if len(repos) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed repos found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPOSITORY\tTIER\tROLE\tVISIBILITY\tROUTING\tREVIEW\tAPPROVAL\tRELEASE\tUPDATED")
	for _, repo := range repos {
		tier := repo.Tier
		if tier == "" {
			tier = "-"
		}
		routing := repo.DefaultRoutingPolicyRef
		if routing == "" {
			routing = "-"
		}
		review := repo.DefaultReviewPolicyRef
		if review == "" {
			review = "-"
		}
		approval := repo.DefaultApprovalPolicyRef
		if approval == "" {
			approval = "-"
		}
		release := repo.DefaultReleaseGateRef
		if release == "" {
			release = "-"
		}
		updated := repo.UpdatedAt
		if updated == "" {
			updated = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			repo.Name, repo.Repository, tier, repo.Role, repo.Visibility, routing, review, approval, release, updated)
	}
	return w.Flush()
}

func runStateManagedRepoGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	repo, err := store.GetManagedRepo(db, args[0])
	if err != nil {
		return fmt.Errorf("getting managed repo %s: %w", args[0], err)
	}
	if repo == nil {
		return fmt.Errorf("managed repo %q not found", args[0])
	}
	report := controlplane.BuildManagedRepoView(*repo)

	if stateManagedRepoJSON {
		return writeManagedRepoJSON(cmd.OutOrStdout(), report)
	}

	tier := report.Tier
	if tier == "" {
		tier = "-"
	}
	release := report.DefaultReleaseGateRef
	if release == "" {
		release = "-"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Name:                    %s\n", report.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:              %s\n", report.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "Tier:                    %s\n", tier)
	fmt.Fprintf(cmd.OutOrStdout(), "Role:                    %s\n", report.Role)
	fmt.Fprintf(cmd.OutOrStdout(), "Visibility:              %s\n", report.Visibility)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoContractPath:        %s\n", emptyFallback(report.RepoContractPath))
	fmt.Fprintf(cmd.OutOrStdout(), "DefaultRoutingPolicyRef: %s\n", emptyFallback(report.DefaultRoutingPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "DefaultReviewPolicyRef:  %s\n", emptyFallback(report.DefaultReviewPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "DefaultApprovalPolicyRef: %s\n", emptyFallback(report.DefaultApprovalPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "DefaultBenchmarkPolicyRef:%s\n", " "+emptyFallback(report.DefaultBenchmarkPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "DefaultReleaseGateRef:   %s\n", release)
	fmt.Fprintf(cmd.OutOrStdout(), "SourcePath:              %s\n", emptyFallback(report.SourcePath))
	fmt.Fprintf(cmd.OutOrStdout(), "SourceCommit:            %s\n", emptyFallback(report.SourceCommit))
	fmt.Fprintf(cmd.OutOrStdout(), "SpecHash:                %s\n", emptyFallback(report.SpecHash))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:                 %s\n", emptyFallback(report.UpdatedAt))
	if report.SpecParseError != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "SpecParseError:          %s\n", report.SpecParseError)
	}
	if hasSelfHostingSettings(report.SelfHosting) {
		fmt.Fprintln(cmd.OutOrStdout(), "SelfHosting:")
		fmt.Fprintf(cmd.OutOrStdout(), "  canModifyDocs: %t\n", report.SelfHosting.CanModifyDocs)
		fmt.Fprintf(cmd.OutOrStdout(), "  canModifyOpsSpecs: %t\n", report.SelfHosting.CanModifyOpsSpecs)
		fmt.Fprintf(cmd.OutOrStdout(), "  canModifyRuntimeCode: %t\n", report.SelfHosting.CanModifyRuntimeCode)
		fmt.Fprintf(cmd.OutOrStdout(), "  canModifyApprovalPolicy: %t\n", report.SelfHosting.CanModifyApprovalPolicy)
		fmt.Fprintf(cmd.OutOrStdout(), "  requiresHumanApprovalBeforeMerge: %t\n", report.SelfHosting.RequiresHumanApprovalBeforeMerge)
		if len(report.SelfHosting.RequiresExtraReviewFor) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  requiresExtraReviewFor: %s\n", strings.Join(report.SelfHosting.RequiresExtraReviewFor, ", "))
		}
	}
	return nil
}

func writeManagedRepoJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func hasSelfHostingSettings(spec controlplane.ManagedRepoSelfHostingSpec) bool {
	return spec.CanModifyDocs ||
		spec.CanModifyOpsSpecs ||
		spec.CanModifyRuntimeCode ||
		spec.CanModifyApprovalPolicy ||
		spec.RequiresHumanApprovalBeforeMerge ||
		len(spec.RequiresExtraReviewFor) > 0
}
