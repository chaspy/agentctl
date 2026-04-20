package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	stateProposalJSON              bool
	stateProposalAdoptionJSON      bool
	stateProposalAdoptDryRun       bool
	stateProposalAdoptNote         string
	stateProposalMaterializeDryRun bool
	stateProposalMaterializeName   string
)

var stateProposalCmd = &cobra.Command{
	Use:   "proposal",
	Short: "Inspect persisted task proposal snapshots",
}

var stateProposalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persisted task proposal snapshots from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateProposalList,
}

var stateProposalGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one persisted task proposal snapshot from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateProposalGet,
}

var stateProposalAdoptCmd = &cobra.Command{
	Use:   "adopt <id>",
	Short: "Queue an explicit adoption of a persisted task proposal snapshot",
	Long: `Queues a protected task proposal adoption in SQLite.
This records that an operator carried a proposal snapshot forward, but it does
not create an execution task yet.

Use --dry-run to preview the adoption record without writing anything.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateProposalAdopt,
}

var stateProposalAdoptionCmd = &cobra.Command{
	Use:   "adoption",
	Short: "Inspect queued task proposal adoptions",
}

var stateProposalAdoptionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List task proposal adoptions from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateProposalAdoptionList,
}

var stateProposalAdoptionGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one task proposal adoption from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateProposalAdoptionGet,
}

var stateProposalAdoptionMaterializeCmd = &cobra.Command{
	Use:   "materialize <id>",
	Short: "Materialize a queued proposal adoption into an AgentTask record",
	Long: `Creates a planned AgentTask record from a queued proposal adoption.
This does not spawn a session or create an execution task yet.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateProposalAdoptionMaterialize,
}

func init() {
	stateCmd.AddCommand(stateProposalCmd)
	stateProposalCmd.AddCommand(stateProposalListCmd)
	stateProposalCmd.AddCommand(stateProposalGetCmd)
	stateProposalCmd.AddCommand(stateProposalAdoptCmd)
	stateProposalCmd.AddCommand(stateProposalAdoptionCmd)
	stateProposalAdoptionCmd.AddCommand(stateProposalAdoptionListCmd)
	stateProposalAdoptionCmd.AddCommand(stateProposalAdoptionGetCmd)
	stateProposalAdoptionCmd.AddCommand(stateProposalAdoptionMaterializeCmd)
	stateProposalListCmd.Flags().BoolVar(&stateProposalJSON, "json", false, "Output machine-readable JSON")
	stateProposalGetCmd.Flags().BoolVar(&stateProposalJSON, "json", false, "Output machine-readable JSON")
	stateProposalAdoptCmd.Flags().BoolVar(&stateProposalAdoptDryRun, "dry-run", false, "Preview the adoption without writing to SQLite")
	stateProposalAdoptCmd.Flags().StringVar(&stateProposalAdoptNote, "note", "", "Operator note stored with the queued adoption")
	stateProposalAdoptionListCmd.Flags().BoolVar(&stateProposalAdoptionJSON, "json", false, "Output machine-readable JSON")
	stateProposalAdoptionGetCmd.Flags().BoolVar(&stateProposalAdoptionJSON, "json", false, "Output machine-readable JSON")
	stateProposalAdoptionMaterializeCmd.Flags().BoolVar(&stateProposalMaterializeDryRun, "dry-run", false, "Preview the AgentTask record without writing to SQLite")
	stateProposalAdoptionMaterializeCmd.Flags().StringVar(&stateProposalMaterializeName, "name", "", "Override the AgentTask name")
}

func runStateProposalList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	proposals, err := store.ListTaskProposalSnapshots(db)
	if err != nil {
		return fmt.Errorf("listing task proposal snapshots: %w", err)
	}
	if stateProposalJSON {
		return writeProposalJSON(cmd.OutOrStdout(), proposals)
	}
	if len(proposals) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No task proposal snapshots found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSOURCE\tREPO\tCATEGORY\tTASK_TYPE\tRISK\tAPPROVAL\tUPDATED")
	for _, proposal := range proposals {
		updated := proposal.UpdatedAt
		if updated == "" {
			updated = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			proposal.ID,
			proposal.Source,
			proposal.RepoRef,
			proposal.Category,
			proposal.TaskType,
			proposal.Risk,
			proposal.ApprovalStatus,
			updated,
		)
	}
	return w.Flush()
}

func runStateProposalGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	proposal, err := store.GetTaskProposalSnapshot(db, args[0])
	if err != nil {
		return fmt.Errorf("getting task proposal snapshot %s: %w", args[0], err)
	}
	if proposal == nil {
		return fmt.Errorf("task proposal snapshot %q not found", args[0])
	}
	if stateProposalJSON {
		return writeProposalJSON(cmd.OutOrStdout(), proposal)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:                %s\n", proposal.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Source:            %s\n", emptyFallback(proposal.Source))
	fmt.Fprintf(cmd.OutOrStdout(), "ReportMode:        %s\n", emptyFallback(proposal.ReportMode))
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:           %s\n", proposal.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:        %s\n", proposal.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "Tier:              %s\n", emptyFallback(proposal.Tier))
	fmt.Fprintf(cmd.OutOrStdout(), "Category:          %s\n", proposal.Category)
	fmt.Fprintf(cmd.OutOrStdout(), "Title:             %s\n", proposal.Title)
	fmt.Fprintf(cmd.OutOrStdout(), "Objective:         %s\n", proposal.Objective)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:          %s\n", proposal.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:              %s\n", proposal.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "ReviewPolicyRef:   %s\n", emptyFallback(proposal.ReviewPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalPolicyRef: %s\n", emptyFallback(proposal.ApprovalPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalStatus:    %s\n", proposal.ApprovalStatus)
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalReason:    %s\n", emptyFallback(proposal.ApprovalReason))
	if len(proposal.DesiredOutcome) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "DesiredOutcome:")
		for _, item := range proposal.DesiredOutcome {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	if len(proposal.TriggerIssues) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "TriggerIssues:")
		for _, item := range proposal.TriggerIssues {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:           %s\n", emptyFallback(proposal.UpdatedAt))
	return nil
}

func runStateProposalAdopt(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	snapshot, err := store.GetTaskProposalSnapshot(db, args[0])
	if err != nil {
		return fmt.Errorf("getting task proposal snapshot %s: %w", args[0], err)
	}
	if snapshot == nil {
		return fmt.Errorf("task proposal snapshot %q not found", args[0])
	}

	adoption := buildTaskProposalAdoption(snapshot, stateProposalAdoptNote)
	out := cmd.OutOrStdout()
	if stateProposalAdoptDryRun {
		writeProposalAdoption(out, adoption, true)
		fmt.Fprintln(out, "No database changes were made.")
		return nil
	}

	if existing, err := store.GetQueuedTaskProposalAdoptionBySnapshotID(db, snapshot.ID); err == nil {
		return fmt.Errorf("a queued proposal adoption already exists for %q (#%d)", snapshot.ID, existing.ID)
	} else if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("checking queued proposal adoptions: %w", err)
	}

	if err := store.CreateTaskProposalAdoption(db, adoption); err != nil {
		return fmt.Errorf("queueing task proposal adoption: %w", err)
	}

	writeProposalAdoption(out, adoption, false)
	fmt.Fprintf(out, "Queued task proposal adoption #%d. No execution task was created.\n", adoption.ID)
	return nil
}

func runStateProposalAdoptionList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	adoptions, err := store.ListTaskProposalAdoptions(db)
	if err != nil {
		return fmt.Errorf("listing task proposal adoptions: %w", err)
	}
	if stateProposalAdoptionJSON {
		return writeProposalJSON(cmd.OutOrStdout(), adoptions)
	}
	if len(adoptions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No task proposal adoptions found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSNAPSHOT\tREPO\tCATEGORY\tTASK_TYPE\tRISK\tSTATUS\tCREATED")
	for _, adoption := range adoptions {
		created := adoption.CreatedAt
		if created == "" {
			created = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			adoption.ID,
			adoption.ProposalSnapshotID,
			adoption.RepoRef,
			adoption.Category,
			adoption.TaskType,
			adoption.Risk,
			adoption.Status,
			created,
		)
	}
	return w.Flush()
}

func runStateProposalAdoptionGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid adoption ID %q: %w", args[0], err)
	}

	adoption, err := store.GetTaskProposalAdoption(db, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("task proposal adoption %q not found", args[0])
		}
		return fmt.Errorf("getting task proposal adoption %s: %w", args[0], err)
	}
	if stateProposalAdoptionJSON {
		return writeProposalJSON(cmd.OutOrStdout(), adoption)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:                %d\n", adoption.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "ProposalSnapshot:  %s\n", adoption.ProposalSnapshotID)
	fmt.Fprintf(cmd.OutOrStdout(), "Source:            %s\n", emptyFallback(adoption.Source))
	fmt.Fprintf(cmd.OutOrStdout(), "ReportMode:        %s\n", emptyFallback(adoption.ReportMode))
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:           %s\n", adoption.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:        %s\n", adoption.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "Tier:              %s\n", emptyFallback(adoption.Tier))
	fmt.Fprintf(cmd.OutOrStdout(), "Category:          %s\n", adoption.Category)
	fmt.Fprintf(cmd.OutOrStdout(), "Title:             %s\n", adoption.Title)
	fmt.Fprintf(cmd.OutOrStdout(), "Objective:         %s\n", adoption.Objective)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:          %s\n", adoption.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:              %s\n", adoption.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "ReviewPolicyRef:   %s\n", emptyFallback(adoption.ReviewPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalPolicyRef: %s\n", emptyFallback(adoption.ApprovalPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalStatus:    %s\n", adoption.ApprovalStatus)
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalReason:    %s\n", emptyFallback(adoption.ApprovalReason))
	fmt.Fprintf(cmd.OutOrStdout(), "Status:            %s\n", adoption.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "OperatorNote:      %s\n", emptyFallback(adoption.OperatorNote))
	if len(adoption.DesiredOutcome) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "DesiredOutcome:")
		for _, item := range adoption.DesiredOutcome {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	if len(adoption.TriggerIssues) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "TriggerIssues:")
		for _, item := range adoption.TriggerIssues {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created:           %s\n", emptyFallback(adoption.CreatedAt))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:           %s\n", emptyFallback(adoption.UpdatedAt))
	return nil
}

func runStateProposalAdoptionMaterialize(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid adoption ID %q: %w", args[0], err)
	}

	adoption, err := store.GetTaskProposalAdoption(db, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("task proposal adoption %q not found", args[0])
		}
		return fmt.Errorf("getting task proposal adoption %s: %w", args[0], err)
	}
	if adoption.Status != store.TaskProposalAdoptionStatusQueued {
		return fmt.Errorf("task proposal adoption %d is %q, expected queued", adoption.ID, adoption.Status)
	}

	task := buildAgentTaskFromProposalAdoption(adoption, stateProposalMaterializeName)
	specJSON, err := json.Marshal(struct {
		RepoRef           string   `json:"repoRef"`
		Objective         string   `json:"objective"`
		TaskType          string   `json:"taskType"`
		Risk              string   `json:"risk"`
		DesiredOutcome    []string `json:"desiredOutcome,omitempty"`
		ReviewPolicyRef   string   `json:"reviewPolicyRef,omitempty"`
		ApprovalPolicyRef string   `json:"approvalPolicyRef,omitempty"`
		Approval          struct {
			RequiredBeforeMerge bool `json:"requiredBeforeMerge"`
		} `json:"approval"`
	}{
		RepoRef:           task.RepoRef,
		Objective:         task.Objective,
		TaskType:          task.TaskType,
		Risk:              task.Risk,
		DesiredOutcome:    task.DesiredOutcome,
		ReviewPolicyRef:   task.ReviewPolicyRef,
		ApprovalPolicyRef: task.ApprovalPolicyRef,
		Approval: struct {
			RequiredBeforeMerge bool `json:"requiredBeforeMerge"`
		}{
			RequiredBeforeMerge: task.ApprovalRequiredBeforeMerge,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal materialized agent task spec: %w", err)
	}
	task.RawSpecJSON = string(specJSON)
	out := cmd.OutOrStdout()
	if stateProposalMaterializeDryRun {
		writeMaterializedAgentTask(out, task, true)
		fmt.Fprintln(out, "No database changes were made.")
		return nil
	}

	if existing, err := store.GetAgentTask(db, task.Name); err != nil {
		return fmt.Errorf("checking existing agent task %s: %w", task.Name, err)
	} else if existing != nil {
		return fmt.Errorf("agent task %q already exists", task.Name)
	}

	if err := store.UpsertAgentTask(db, task); err != nil {
		return fmt.Errorf("storing agent task: %w", err)
	}
	if err := store.UpdateTaskProposalAdoptionStatus(db, adoption.ID, store.TaskProposalAdoptionStatusMaterialized); err != nil {
		return fmt.Errorf("marking proposal adoption materialized: %w", err)
	}

	writeMaterializedAgentTask(out, task, false)
	fmt.Fprintf(out, "Materialized AgentTask %s from proposal adoption #%d.\n", task.Name, adoption.ID)
	return nil
}

func buildTaskProposalAdoption(snapshot *store.TaskProposalSnapshot, note string) *store.TaskProposalAdoption {
	return &store.TaskProposalAdoption{
		ProposalSnapshotID: snapshot.ID,
		Source:             snapshot.Source,
		ReportMode:         snapshot.ReportMode,
		RepoRef:            snapshot.RepoRef,
		Repository:         snapshot.Repository,
		Tier:               snapshot.Tier,
		Category:           snapshot.Category,
		Title:              snapshot.Title,
		Objective:          snapshot.Objective,
		TaskType:           snapshot.TaskType,
		Risk:               snapshot.Risk,
		ReviewPolicyRef:    snapshot.ReviewPolicyRef,
		ApprovalPolicyRef:  snapshot.ApprovalPolicyRef,
		ApprovalStatus:     snapshot.ApprovalStatus,
		ApprovalReason:     snapshot.ApprovalReason,
		DesiredOutcome:     append([]string(nil), snapshot.DesiredOutcome...),
		TriggerIssues:      append([]string(nil), snapshot.TriggerIssues...),
		Status:             store.TaskProposalAdoptionStatusQueued,
		OperatorNote:       note,
		RawProposalJSON:    snapshot.RawProposalJSON,
	}
}

func buildAgentTaskFromProposalAdoption(adoption *store.TaskProposalAdoption, overrideName string) *store.AgentTask {
	name := overrideName
	if name == "" {
		name = fmt.Sprintf("%s-%s-adoption-%d", adoption.RepoRef, adoption.Category, adoption.ID)
	}
	return &store.AgentTask{
		Name:                        name,
		RepoRef:                     adoption.RepoRef,
		Repository:                  adoption.Repository,
		Objective:                   adoption.Objective,
		TaskType:                    adoption.TaskType,
		Risk:                        adoption.Risk,
		DesiredOutcome:              append([]string(nil), adoption.DesiredOutcome...),
		ReviewPolicyRef:             adoption.ReviewPolicyRef,
		ApprovalPolicyRef:           adoption.ApprovalPolicyRef,
		ApprovalRequiredBeforeMerge: adoption.ApprovalStatus == "required",
		SourceKind:                  "proposal_adoption",
		SourceRef:                   strconv.FormatInt(adoption.ID, 10),
		Status:                      "planned",
	}
}

func writeProposalAdoption(w io.Writer, adoption *store.TaskProposalAdoption, dryRun bool) {
	title := "Task proposal adoption"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "proposal_snapshot\t%s\n", adoption.ProposalSnapshotID)
	fmt.Fprintf(tw, "source\t%s\n", dashIfEmpty(adoption.Source))
	fmt.Fprintf(tw, "report_mode\t%s\n", dashIfEmpty(adoption.ReportMode))
	fmt.Fprintf(tw, "repo\t%s\n", dashIfEmpty(adoption.RepoRef))
	fmt.Fprintf(tw, "repository\t%s\n", dashIfEmpty(adoption.Repository))
	fmt.Fprintf(tw, "category\t%s\n", dashIfEmpty(adoption.Category))
	fmt.Fprintf(tw, "task_type\t%s\n", dashIfEmpty(adoption.TaskType))
	fmt.Fprintf(tw, "risk\t%s\n", dashIfEmpty(adoption.Risk))
	fmt.Fprintf(tw, "approval\t%s\n", dashIfEmpty(adoption.ApprovalStatus))
	fmt.Fprintf(tw, "title\t%s\n", dashIfEmpty(adoption.Title))
	fmt.Fprintf(tw, "status\t%s\n", dashIfEmpty(adoption.Status))
	fmt.Fprintf(tw, "note\t%s\n", dashIfEmpty(adoption.OperatorNote))
	_ = tw.Flush()
}

func writeMaterializedAgentTask(w io.Writer, task *store.AgentTask, dryRun bool) {
	title := "Materialized AgentTask"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "name\t%s\n", task.Name)
	fmt.Fprintf(tw, "repo\t%s\n", dashIfEmpty(task.RepoRef))
	fmt.Fprintf(tw, "repository\t%s\n", dashIfEmpty(task.Repository))
	fmt.Fprintf(tw, "task_type\t%s\n", dashIfEmpty(task.TaskType))
	fmt.Fprintf(tw, "risk\t%s\n", dashIfEmpty(task.Risk))
	fmt.Fprintf(tw, "source_kind\t%s\n", dashIfEmpty(task.SourceKind))
	fmt.Fprintf(tw, "source_ref\t%s\n", dashIfEmpty(task.SourceRef))
	fmt.Fprintf(tw, "status\t%s\n", dashIfEmpty(task.Status))
	fmt.Fprintf(tw, "approval_required_before_merge\t%s\n", yesNo(task.ApprovalRequiredBeforeMerge))
	_ = tw.Flush()
}

func writeProposalJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
