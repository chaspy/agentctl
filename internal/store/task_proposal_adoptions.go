package store

import "database/sql"

const (
	TaskProposalAdoptionStatusQueued       = "queued"
	TaskProposalAdoptionStatusMaterialized = "materialized"
	TaskProposalAdoptionStatusCancelled    = "cancelled"
)

// TaskProposalAdoption tracks an explicit operator decision to carry a persisted
// proposal snapshot forward, without creating an execution task yet.
type TaskProposalAdoption struct {
	ID                 int64    `json:"id"`
	ProposalSnapshotID string   `json:"proposalSnapshotId"`
	Source             string   `json:"source"`
	ReportMode         string   `json:"reportMode"`
	RepoRef            string   `json:"repoRef"`
	Repository         string   `json:"repository"`
	Tier               string   `json:"tier"`
	Category           string   `json:"category"`
	Title              string   `json:"title"`
	Objective          string   `json:"objective"`
	TaskType           string   `json:"taskType"`
	Risk               string   `json:"risk"`
	ReviewPolicyRef    string   `json:"reviewPolicyRef"`
	ApprovalPolicyRef  string   `json:"approvalPolicyRef"`
	ApprovalStatus     string   `json:"approvalStatus"`
	ApprovalReason     string   `json:"approvalReason"`
	DesiredOutcome     []string `json:"desiredOutcome"`
	TriggerIssues      []string `json:"triggerIssues"`
	Status             string   `json:"status"`
	OperatorNote       string   `json:"operatorNote"`
	RawProposalJSON    string   `json:"rawProposalJSON"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

func CreateTaskProposalAdoption(db *sql.DB, adoption *TaskProposalAdoption) error {
	status := adoption.Status
	if status == "" {
		status = TaskProposalAdoptionStatusQueued
	}

	desiredOutcomeJSON, err := marshalStringSlice(adoption.DesiredOutcome)
	if err != nil {
		return err
	}
	triggerIssuesJSON, err := marshalStringSlice(adoption.TriggerIssues)
	if err != nil {
		return err
	}

	res, err := db.Exec(`
		INSERT INTO task_proposal_adoptions (
			proposal_snapshot_id,
			source,
			report_mode,
			repo_ref,
			repository,
			tier,
			category,
			title,
			objective,
			task_type,
			risk,
			review_policy_ref,
			approval_policy_ref,
			approval_status,
			approval_reason,
			desired_outcome_json,
			trigger_issues_json,
			status,
			operator_note,
			raw_proposal_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		adoption.ProposalSnapshotID,
		adoption.Source,
		adoption.ReportMode,
		adoption.RepoRef,
		adoption.Repository,
		adoption.Tier,
		adoption.Category,
		adoption.Title,
		adoption.Objective,
		adoption.TaskType,
		adoption.Risk,
		adoption.ReviewPolicyRef,
		adoption.ApprovalPolicyRef,
		adoption.ApprovalStatus,
		adoption.ApprovalReason,
		desiredOutcomeJSON,
		triggerIssuesJSON,
		status,
		adoption.OperatorNote,
		adoption.RawProposalJSON,
	)
	if err != nil {
		return err
	}
	adoption.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	adoption.Status = status
	return nil
}

func ListTaskProposalAdoptions(db *sql.DB) ([]TaskProposalAdoption, error) {
	return queryTaskProposalAdoptions(db, `
		SELECT
			id,
			proposal_snapshot_id,
			source,
			report_mode,
			repo_ref,
			repository,
			tier,
			category,
			title,
			objective,
			task_type,
			risk,
			review_policy_ref,
			approval_policy_ref,
			approval_status,
			approval_reason,
			desired_outcome_json,
			trigger_issues_json,
			status,
			operator_note,
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_adoptions
		ORDER BY created_at DESC, id DESC`)
}

func ListQueuedTaskProposalAdoptions(db *sql.DB) ([]TaskProposalAdoption, error) {
	return queryTaskProposalAdoptions(db, `
		SELECT
			id,
			proposal_snapshot_id,
			source,
			report_mode,
			repo_ref,
			repository,
			tier,
			category,
			title,
			objective,
			task_type,
			risk,
			review_policy_ref,
			approval_policy_ref,
			approval_status,
			approval_reason,
			desired_outcome_json,
			trigger_issues_json,
			status,
			operator_note,
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_adoptions
		WHERE status = ?
		ORDER BY created_at DESC, id DESC`, TaskProposalAdoptionStatusQueued)
}

func GetTaskProposalAdoption(db *sql.DB, id int64) (*TaskProposalAdoption, error) {
	items, err := queryTaskProposalAdoptions(db, `
		SELECT
			id,
			proposal_snapshot_id,
			source,
			report_mode,
			repo_ref,
			repository,
			tier,
			category,
			title,
			objective,
			task_type,
			risk,
			review_policy_ref,
			approval_policy_ref,
			approval_status,
			approval_reason,
			desired_outcome_json,
			trigger_issues_json,
			status,
			operator_note,
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_adoptions
		WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return &items[0], nil
}

func GetQueuedTaskProposalAdoptionBySnapshotID(db *sql.DB, proposalSnapshotID string) (*TaskProposalAdoption, error) {
	items, err := queryTaskProposalAdoptions(db, `
		SELECT
			id,
			proposal_snapshot_id,
			source,
			report_mode,
			repo_ref,
			repository,
			tier,
			category,
			title,
			objective,
			task_type,
			risk,
			review_policy_ref,
			approval_policy_ref,
			approval_status,
			approval_reason,
			desired_outcome_json,
			trigger_issues_json,
			status,
			operator_note,
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_adoptions
		WHERE proposal_snapshot_id = ? AND status = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, proposalSnapshotID, TaskProposalAdoptionStatusQueued)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return &items[0], nil
}

func CountQueuedTaskProposalAdoptions(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_proposal_adoptions WHERE status = ?`, TaskProposalAdoptionStatusQueued).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func queryTaskProposalAdoptions(db *sql.DB, query string, args ...any) ([]TaskProposalAdoption, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var adoptions []TaskProposalAdoption
	for rows.Next() {
		adoption, err := scanTaskProposalAdoption(rows)
		if err != nil {
			return nil, err
		}
		adoptions = append(adoptions, adoption)
	}
	return adoptions, rows.Err()
}

func scanTaskProposalAdoption(scanner taskProposalRowScanner) (TaskProposalAdoption, error) {
	var adoption TaskProposalAdoption
	var desiredOutcomeJSON string
	var triggerIssuesJSON string
	if err := scanner.Scan(
		&adoption.ID,
		&adoption.ProposalSnapshotID,
		&adoption.Source,
		&adoption.ReportMode,
		&adoption.RepoRef,
		&adoption.Repository,
		&adoption.Tier,
		&adoption.Category,
		&adoption.Title,
		&adoption.Objective,
		&adoption.TaskType,
		&adoption.Risk,
		&adoption.ReviewPolicyRef,
		&adoption.ApprovalPolicyRef,
		&adoption.ApprovalStatus,
		&adoption.ApprovalReason,
		&desiredOutcomeJSON,
		&triggerIssuesJSON,
		&adoption.Status,
		&adoption.OperatorNote,
		&adoption.RawProposalJSON,
		&adoption.CreatedAt,
		&adoption.UpdatedAt,
	); err != nil {
		return TaskProposalAdoption{}, err
	}
	var err error
	adoption.DesiredOutcome, err = unmarshalStringSlice(desiredOutcomeJSON)
	if err != nil {
		return TaskProposalAdoption{}, err
	}
	adoption.TriggerIssues, err = unmarshalStringSlice(triggerIssuesJSON)
	if err != nil {
		return TaskProposalAdoption{}, err
	}
	return adoption, nil
}
