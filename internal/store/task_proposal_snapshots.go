package store

import (
	"database/sql"
	"encoding/json"
)

type TaskProposalSnapshot struct {
	ID                string   `json:"id"`
	Source            string   `json:"source"`
	ReportMode        string   `json:"reportMode"`
	RepoRef           string   `json:"repoRef"`
	Repository        string   `json:"repository"`
	Tier              string   `json:"tier"`
	Category          string   `json:"category"`
	Title             string   `json:"title"`
	Objective         string   `json:"objective"`
	TaskType          string   `json:"taskType"`
	Risk              string   `json:"risk"`
	ReviewPolicyRef   string   `json:"reviewPolicyRef"`
	ApprovalPolicyRef string   `json:"approvalPolicyRef"`
	ApprovalStatus    string   `json:"approvalStatus"`
	ApprovalReason    string   `json:"approvalReason"`
	DesiredOutcome    []string `json:"desiredOutcome"`
	TriggerIssues     []string `json:"triggerIssues"`
	RawProposalJSON   string   `json:"rawProposalJSON"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

func ReplaceTaskProposalSnapshots(db *sql.DB, source string, proposals []TaskProposalSnapshot) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM task_proposal_snapshots WHERE source = ?`, source); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO task_proposal_snapshots (
			id,
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
			raw_proposal_json,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, proposal := range proposals {
		desiredOutcomeJSON, err := marshalStringSlice(proposal.DesiredOutcome)
		if err != nil {
			return err
		}
		triggerIssuesJSON, err := marshalStringSlice(proposal.TriggerIssues)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(
			proposal.ID,
			source,
			proposal.ReportMode,
			proposal.RepoRef,
			proposal.Repository,
			proposal.Tier,
			proposal.Category,
			proposal.Title,
			proposal.Objective,
			proposal.TaskType,
			proposal.Risk,
			proposal.ReviewPolicyRef,
			proposal.ApprovalPolicyRef,
			proposal.ApprovalStatus,
			proposal.ApprovalReason,
			desiredOutcomeJSON,
			triggerIssuesJSON,
			proposal.RawProposalJSON,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func ListTaskProposalSnapshots(db *sql.DB) ([]TaskProposalSnapshot, error) {
	rows, err := db.Query(`
		SELECT
			id,
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
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_snapshots
		ORDER BY source ASC, repo_ref ASC, category ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []TaskProposalSnapshot
	for rows.Next() {
		proposal, err := scanTaskProposalSnapshot(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func GetTaskProposalSnapshot(db *sql.DB, id string) (*TaskProposalSnapshot, error) {
	rows, err := db.Query(`
		SELECT
			id,
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
			raw_proposal_json,
			created_at,
			updated_at
		FROM task_proposal_snapshots
		WHERE id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	proposal, err := scanTaskProposalSnapshot(rows)
	if err != nil {
		return nil, err
	}
	return &proposal, rows.Err()
}

func CountTaskProposalSnapshots(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_proposal_snapshots`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type taskProposalRowScanner interface {
	Scan(dest ...any) error
}

func scanTaskProposalSnapshot(scanner taskProposalRowScanner) (TaskProposalSnapshot, error) {
	var proposal TaskProposalSnapshot
	var desiredOutcomeJSON string
	var triggerIssuesJSON string
	if err := scanner.Scan(
		&proposal.ID,
		&proposal.Source,
		&proposal.ReportMode,
		&proposal.RepoRef,
		&proposal.Repository,
		&proposal.Tier,
		&proposal.Category,
		&proposal.Title,
		&proposal.Objective,
		&proposal.TaskType,
		&proposal.Risk,
		&proposal.ReviewPolicyRef,
		&proposal.ApprovalPolicyRef,
		&proposal.ApprovalStatus,
		&proposal.ApprovalReason,
		&desiredOutcomeJSON,
		&triggerIssuesJSON,
		&proposal.RawProposalJSON,
		&proposal.CreatedAt,
		&proposal.UpdatedAt,
	); err != nil {
		return TaskProposalSnapshot{}, err
	}
	var err error
	proposal.DesiredOutcome, err = unmarshalStringSlice(desiredOutcomeJSON)
	if err != nil {
		return TaskProposalSnapshot{}, err
	}
	proposal.TriggerIssues, err = unmarshalStringSlice(triggerIssuesJSON)
	if err != nil {
		return TaskProposalSnapshot{}, err
	}
	return proposal, nil
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	out, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func unmarshalStringSlice(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return nil, err
	}
	return items, nil
}
