package cmd

import (
	"database/sql"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	adoptAgent             string
	adoptRepository        string
	adoptBranch            string
	adoptCWD               string
	adoptExternalSessionID string
	adoptPermission        string
	adoptNote              string
	adoptDryRun            bool
)

var stateAdoptCmd = &cobra.Command{
	Use:   "adopt <zellij-session>",
	Short: "Queue a protected adoption plan for an unmanaged Codex session",
	Long: `Inspects a live zellij session and stores a protected adoption plan in SQLite.
This command does not import the live session into the sessions table yet.

Use --dry-run to preview the detected metadata without writing anything.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateAdopt,
}

func init() {
	stateCmd.AddCommand(stateAdoptCmd)
	stateAdoptCmd.Flags().StringVar(&adoptAgent, "agent", "", `Agent override. Currently only "codex" is supported.`)
	stateAdoptCmd.Flags().StringVar(&adoptRepository, "repo", "", "Repository override (owner/repo)")
	stateAdoptCmd.Flags().StringVar(&adoptBranch, "branch", "", "Git branch override")
	stateAdoptCmd.Flags().StringVar(&adoptCWD, "cwd", "", "Working directory override")
	stateAdoptCmd.Flags().StringVar(&adoptExternalSessionID, "session-id", "", "External agent session ID override")
	stateAdoptCmd.Flags().StringVar(&adoptPermission, "permission", "suggest", `Target permission after apply: "suggest", "auto-read", "auto-edit", "auto-run", or "full-auto"`)
	stateAdoptCmd.Flags().StringVar(&adoptNote, "note", "", "Operator note stored with the queued adoption")
	stateAdoptCmd.Flags().BoolVar(&adoptDryRun, "dry-run", false, "Preview the adoption plan without writing to SQLite")
}

func runStateAdopt(cmd *cobra.Command, args []string) error {
	targetPermission, err := parsePermissionLevel(adoptPermission)
	if err != nil {
		return err
	}

	plan, err := buildProtectedAdoptionPlan(args[0], targetPermission)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if adoptDryRun {
		writeAdoptionPlan(out, plan, true)
		fmt.Fprintln(out, "No database changes were made.")
		return nil
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if existing, err := store.GetQueuedSessionAdoptionByZellijSession(db, plan.Mux, plan.ZellijSession); err == nil {
		return fmt.Errorf("a queued adoption already exists for %q (#%d)", plan.ZellijSession, existing.ID)
	} else if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("checking queued adoptions: %w", err)
	}

	if err := store.CreateSessionAdoption(db, plan); err != nil {
		return fmt.Errorf("queueing adoption: %w", err)
	}

	writeAdoptionPlan(out, plan, false)
	fmt.Fprintf(out, "Queued protected adoption #%d. No live session was imported.\n", plan.ID)
	return nil
}

func buildProtectedAdoptionPlan(query string, targetPermission int) (*store.SessionAdoption, error) {
	zs, err := resolveAdoptionZellijSession(query)
	if err != nil {
		return nil, err
	}

	plan := &store.SessionAdoption{
		Mux:                   "zellij",
		ZellijSession:         zs.Name,
		ExternalSessionID:     adoptExternalSessionID,
		Strategy:              store.AdoptionStrategyProtected,
		TargetPermissionLevel: targetPermission,
		Status:                store.AdoptionStatusQueued,
		Note:                  adoptNote,
	}

	if discovered, ok := discoverSessionFromZellij(zs); ok {
		plan.Agent = discovered.Session.Agent
		plan.Repository = discovered.Session.Repository
		plan.CWD = discovered.Session.CWD
		plan.GitBranch = discovered.Session.GitBranch
	}

	if adoptAgent != "" {
		plan.Agent = adoptAgent
	}
	if adoptRepository != "" {
		plan.Repository = adoptRepository
	}
	if adoptBranch != "" {
		plan.GitBranch = adoptBranch
	}
	if adoptCWD != "" {
		plan.CWD = adoptCWD
	}

	if plan.Agent == "" {
		plan.Agent = string(provider.AgentCodex)
	}
	if plan.Agent != string(provider.AgentCodex) {
		return nil, fmt.Errorf("state adopt currently supports only codex sessions, got %q", plan.Agent)
	}
	if plan.CWD == "" {
		return nil, fmt.Errorf("could not determine cwd for zellij session %q; pass --cwd to queue manually", plan.ZellijSession)
	}

	return plan, nil
}

func resolveAdoptionZellijSession(query string) (mux.ZellijSessionState, error) {
	zellijSessions, err := listZellijDetailed()
	if err != nil {
		return mux.ZellijSessionState{}, fmt.Errorf("listing zellij sessions: %w", err)
	}

	lowerQuery := strings.ToLower(query)
	var partial []mux.ZellijSessionState
	for _, zs := range zellijSessions {
		lowerName := strings.ToLower(zs.Name)
		if lowerName == lowerQuery {
			return zs, nil
		}
		if strings.Contains(lowerName, lowerQuery) {
			partial = append(partial, zs)
		}
	}

	switch len(partial) {
	case 0:
		return mux.ZellijSessionState{}, fmt.Errorf("no zellij session matching %q", query)
	case 1:
		return partial[0], nil
	default:
		names := make([]string, 0, len(partial))
		for _, zs := range partial {
			names = append(names, zs.Name)
		}
		return mux.ZellijSessionState{}, fmt.Errorf("ambiguous zellij session %q: %s", query, strings.Join(names, ", "))
	}
}

func parsePermissionLevel(value string) (int, error) {
	switch value {
	case "1", "suggest":
		return store.PermissionSuggest, nil
	case "2", "auto-read":
		return store.PermissionAutoRead, nil
	case "3", "auto-edit":
		return store.PermissionAutoEdit, nil
	case "4", "auto-run":
		return store.PermissionAutoRun, nil
	case "5", "full-auto":
		return store.PermissionFullAuto, nil
	default:
		return 0, fmt.Errorf("invalid permission %q: must be suggest, auto-read, auto-edit, auto-run, full-auto, or 1-5", value)
	}
}

func writeAdoptionPlan(w io.Writer, plan *store.SessionAdoption, dryRun bool) {
	title := "Protected adoption plan"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "agent\t%s\n", dashIfEmpty(plan.Agent))
	fmt.Fprintf(tw, "mux\t%s\n", dashIfEmpty(plan.Mux))
	fmt.Fprintf(tw, "zellij_session\t%s\n", dashIfEmpty(plan.ZellijSession))
	fmt.Fprintf(tw, "external_session_id\t%s\n", dashIfEmpty(plan.ExternalSessionID))
	fmt.Fprintf(tw, "repository\t%s\n", dashIfEmpty(plan.Repository))
	fmt.Fprintf(tw, "branch\t%s\n", dashIfEmpty(plan.GitBranch))
	fmt.Fprintf(tw, "cwd\t%s\n", dashIfEmpty(plan.CWD))
	fmt.Fprintf(tw, "strategy\t%s\n", dashIfEmpty(plan.Strategy))
	fmt.Fprintf(tw, "target_permission\t%d (%s)\n", plan.TargetPermissionLevel, store.PermissionLabel(plan.TargetPermissionLevel))
	fmt.Fprintf(tw, "note\t%s\n", dashIfEmpty(plan.Note))
	_ = tw.Flush()
}

func dashIfEmpty(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
