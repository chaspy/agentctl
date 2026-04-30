package store

import "database/sql"

type AgentTaskOutcome struct {
	ID               int64  `json:"id"`
	AttemptID        int64  `json:"attemptId"`
	DecisionID       int64  `json:"decisionId"`
	AgentTaskName    string `json:"agentTaskName"`
	RepoRef          string `json:"repoRef"`
	Repository       string `json:"repository"`
	TaskType         string `json:"taskType"`
	Risk             string `json:"risk"`
	Agent            string `json:"agent"`
	Branch           string `json:"branch"`
	SessionName      string `json:"sessionName"`
	ManagedSessionID string `json:"managedSessionId"`
	Status           string `json:"status"`
	ResultSummary    string `json:"resultSummary"`
	PRNumber         int    `json:"prNumber"`
	PRURL            string `json:"prUrl"`
	PRState          string `json:"prState"`
	CommitSHA        string `json:"commitSha"`
	FailureCategory  string `json:"failureCategory"`
	FailureReason    string `json:"failureReason"`
	Source           string `json:"source"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

func CreateAgentTaskOutcome(db *sql.DB, outcome *AgentTaskOutcome) error {
	source := outcome.Source
	if source == "" {
		source = "manual"
	}

	res, err := db.Exec(`
		INSERT INTO agent_task_outcomes (
			attempt_id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			branch,
			session_name,
			managed_session_id,
			status,
			result_summary,
			pr_number,
			pr_url,
			pr_state,
			commit_sha,
			failure_category,
			failure_reason,
			source
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		outcome.AttemptID,
		outcome.DecisionID,
		outcome.AgentTaskName,
		outcome.RepoRef,
		outcome.Repository,
		outcome.TaskType,
		outcome.Risk,
		outcome.Agent,
		outcome.Branch,
		outcome.SessionName,
		outcome.ManagedSessionID,
		outcome.Status,
		outcome.ResultSummary,
		outcome.PRNumber,
		outcome.PRURL,
		outcome.PRState,
		outcome.CommitSHA,
		outcome.FailureCategory,
		outcome.FailureReason,
		source,
	)
	if err != nil {
		return err
	}
	outcome.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	outcome.Source = source
	return nil
}

func UpdateAgentTaskOutcome(db *sql.DB, outcome *AgentTaskOutcome) error {
	_, err := db.Exec(`
		UPDATE agent_task_outcomes
		SET
			attempt_id = ?,
			decision_id = ?,
			agent_task_name = ?,
			repo_ref = ?,
			repository = ?,
			task_type = ?,
			risk = ?,
			agent = ?,
			branch = ?,
			session_name = ?,
			managed_session_id = ?,
			status = ?,
			result_summary = ?,
			pr_number = ?,
			pr_url = ?,
			pr_state = ?,
			commit_sha = ?,
			failure_category = ?,
			failure_reason = ?,
			source = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		outcome.AttemptID,
		outcome.DecisionID,
		outcome.AgentTaskName,
		outcome.RepoRef,
		outcome.Repository,
		outcome.TaskType,
		outcome.Risk,
		outcome.Agent,
		outcome.Branch,
		outcome.SessionName,
		outcome.ManagedSessionID,
		outcome.Status,
		outcome.ResultSummary,
		outcome.PRNumber,
		outcome.PRURL,
		outcome.PRState,
		outcome.CommitSHA,
		outcome.FailureCategory,
		outcome.FailureReason,
		outcome.Source,
		outcome.ID,
	)
	return err
}

func ListAgentTaskOutcomes(db *sql.DB) ([]AgentTaskOutcome, error) {
	rows, err := db.Query(`
		SELECT
			id,
			attempt_id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			branch,
			session_name,
			managed_session_id,
			status,
			result_summary,
			pr_number,
			pr_url,
			pr_state,
			commit_sha,
			failure_category,
			failure_reason,
			source,
			created_at,
			updated_at
		FROM agent_task_outcomes
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []AgentTaskOutcome
	for rows.Next() {
		outcome, err := scanAgentTaskOutcome(rows)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, rows.Err()
}

func GetAgentTaskOutcome(db *sql.DB, id int64) (*AgentTaskOutcome, error) {
	rows, err := db.Query(`
		SELECT
			id,
			attempt_id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			branch,
			session_name,
			managed_session_id,
			status,
			result_summary,
			pr_number,
			pr_url,
			pr_state,
			commit_sha,
			failure_category,
			failure_reason,
			source,
			created_at,
			updated_at
		FROM agent_task_outcomes
		WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	outcome, err := scanAgentTaskOutcome(rows)
	if err != nil {
		return nil, err
	}
	return &outcome, rows.Err()
}

func GetAgentTaskOutcomeByAttemptID(db *sql.DB, attemptID int64) (*AgentTaskOutcome, error) {
	rows, err := db.Query(`
		SELECT
			id,
			attempt_id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			branch,
			session_name,
			managed_session_id,
			status,
			result_summary,
			pr_number,
			pr_url,
			pr_state,
			commit_sha,
			failure_category,
			failure_reason,
			source,
			created_at,
			updated_at
		FROM agent_task_outcomes
		WHERE attempt_id = ?`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	outcome, err := scanAgentTaskOutcome(rows)
	if err != nil {
		return nil, err
	}
	return &outcome, rows.Err()
}

func CountAgentTaskOutcomes(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_task_outcomes`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanAgentTaskOutcome(scanner taskProposalRowScanner) (AgentTaskOutcome, error) {
	var outcome AgentTaskOutcome
	if err := scanner.Scan(
		&outcome.ID,
		&outcome.AttemptID,
		&outcome.DecisionID,
		&outcome.AgentTaskName,
		&outcome.RepoRef,
		&outcome.Repository,
		&outcome.TaskType,
		&outcome.Risk,
		&outcome.Agent,
		&outcome.Branch,
		&outcome.SessionName,
		&outcome.ManagedSessionID,
		&outcome.Status,
		&outcome.ResultSummary,
		&outcome.PRNumber,
		&outcome.PRURL,
		&outcome.PRState,
		&outcome.CommitSHA,
		&outcome.FailureCategory,
		&outcome.FailureReason,
		&outcome.Source,
		&outcome.CreatedAt,
		&outcome.UpdatedAt,
	); err != nil {
		return AgentTaskOutcome{}, err
	}
	return outcome, nil
}
