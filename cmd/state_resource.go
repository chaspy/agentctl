package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

type controlPlaneResourceReport struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	APIVersion   string `json:"apiVersion,omitempty"`
	SourcePath   string `json:"sourcePath,omitempty"`
	SourceCommit string `json:"sourceCommit,omitempty"`
	SpecHash     string `json:"specHash,omitempty"`
	RawSpecJSON  string `json:"rawSpecJSON,omitempty"`
	Spec         any    `json:"spec,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

var (
	stateResourceJSON bool
	stateResourceKind string
)

var stateResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Inspect applied control-plane resources from the local DB",
}

var stateResourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applied control-plane resources",
	Args:  cobra.NoArgs,
	RunE:  runStateResourceList,
}

var stateResourceGetCmd = &cobra.Command{
	Use:   "get <kind> <name>",
	Short: "Show one applied control-plane resource",
	Args:  cobra.ExactArgs(2),
	RunE:  runStateResourceGet,
}

func init() {
	stateCmd.AddCommand(stateResourceCmd)
	stateResourceCmd.AddCommand(stateResourceListCmd)
	stateResourceCmd.AddCommand(stateResourceGetCmd)
	stateResourceListCmd.Flags().BoolVar(&stateResourceJSON, "json", false, "Output machine-readable JSON")
	stateResourceListCmd.Flags().StringVar(&stateResourceKind, "kind", "", "Filter by resource kind")
	stateResourceGetCmd.Flags().BoolVar(&stateResourceJSON, "json", false, "Output machine-readable JSON")
}

func runStateResourceList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	resources, err := store.ListControlPlaneResources(db, strings.TrimSpace(stateResourceKind))
	if err != nil {
		return fmt.Errorf("listing control plane resources: %w", err)
	}
	reports := buildControlPlaneResourceReports(resources)

	if stateResourceJSON {
		return writeControlPlaneResourceJSON(cmd.OutOrStdout(), reports)
	}
	if len(reports) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No control-plane resources found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tAPI_VERSION\tUPDATED\tSOURCE_PATH")
	for _, resource := range reports {
		updated := resource.UpdatedAt
		if updated == "" {
			updated = "-"
		}
		sourcePath := resource.SourcePath
		if sourcePath == "" {
			sourcePath = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			resource.Kind,
			resource.Name,
			dashIfEmpty(resource.APIVersion),
			updated,
			sourcePath,
		)
	}
	return w.Flush()
}

func runStateResourceGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	resource, err := store.GetControlPlaneResource(db, args[0], args[1])
	if err != nil {
		return fmt.Errorf("getting control plane resource %s/%s: %w", args[0], args[1], err)
	}
	if resource == nil {
		return fmt.Errorf("control plane resource %q/%q not found", args[0], args[1])
	}
	report := buildControlPlaneResourceReport(*resource)

	if stateResourceJSON {
		return writeControlPlaneResourceJSON(cmd.OutOrStdout(), report)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Kind:        %s\n", report.Kind)
	fmt.Fprintf(cmd.OutOrStdout(), "Name:        %s\n", report.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "APIVersion:  %s\n", emptyFallback(report.APIVersion))
	fmt.Fprintf(cmd.OutOrStdout(), "SourcePath:  %s\n", emptyFallback(report.SourcePath))
	fmt.Fprintf(cmd.OutOrStdout(), "SourceCommit:%s\n", " "+emptyFallback(report.SourceCommit))
	fmt.Fprintf(cmd.OutOrStdout(), "SpecHash:    %s\n", emptyFallback(report.SpecHash))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:     %s\n", emptyFallback(report.UpdatedAt))
	if report.RawSpecJSON != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Spec:        %s\n", prettyJSON(report.RawSpecJSON))
	}
	return nil
}

func buildControlPlaneResourceReports(resources []store.ControlPlaneResource) []controlPlaneResourceReport {
	reports := make([]controlPlaneResourceReport, 0, len(resources))
	for _, resource := range resources {
		reports = append(reports, buildControlPlaneResourceReport(resource))
	}
	return reports
}

func buildControlPlaneResourceReport(resource store.ControlPlaneResource) controlPlaneResourceReport {
	report := controlPlaneResourceReport{
		Kind:         resource.Kind,
		Name:         resource.Name,
		APIVersion:   resource.APIVersion,
		SourcePath:   resource.SourcePath,
		SourceCommit: resource.SourceCommit,
		SpecHash:     resource.SpecHash,
		RawSpecJSON:  resource.RawSpecJSON,
		CreatedAt:    resource.CreatedAt,
		UpdatedAt:    resource.UpdatedAt,
	}
	if resource.RawSpecJSON != "" {
		var spec any
		if err := json.Unmarshal([]byte(resource.RawSpecJSON), &spec); err == nil {
			report.Spec = spec
		}
	}
	return report
}

func writeControlPlaneResourceJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func prettyJSON(raw string) string {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return raw
	}
	return string(out)
}
