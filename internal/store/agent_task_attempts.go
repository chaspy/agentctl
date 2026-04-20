package store

import "database/sql"

type AgentTaskAttempt struct {
	ID               int64  `json:"id"`
	DecisionID       int64  `json:"decisionId"`
	AgentTaskName    string `json:"agentTaskName"`
	RepoRef          string `json:"repoRef"`
	Repository       string `json:"repository"`
	TaskType         string `json:"taskType"`
	Risk             string `json:"risk"`
	Agent            string `json:"agent"`
	RepoMode         string `json:"repoMode"`
	Branch           string `json:"branch"`
	SessionName      string `json:"sessionName"`
	ManagedSessionID string `json:"managedSessionId"`
	WorkDir          string `json:"workDir"`
	LaunchCommand    string `json:"launchCommand"`
	InitialMessage   string `json:"initialMessage"`
	Summary          string `json:"summary"`
	Status           string `json:"status"`
	FailureReason    string `json:"failureReason"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

func CreateAgentTaskAttempt(db *sql.DB, attempt *AgentTaskAttempt) error {
	status := attempt.Status
	if status == "" {
		status = "starting"
	}

	res, err := db.Exec(`
		INSERT INTO agent_task_attempts (
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			repo_mode,
			branch,
			session_name,
			managed_session_id,
			work_dir,
			launch_command,
			initial_message,
			summary,
			status,
			failure_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.DecisionID,
		attempt.AgentTaskName,
		attempt.RepoRef,
		attempt.Repository,
		attempt.TaskType,
		attempt.Risk,
		attempt.Agent,
		attempt.RepoMode,
		attempt.Branch,
		attempt.SessionName,
		attempt.ManagedSessionID,
		attempt.WorkDir,
		attempt.LaunchCommand,
		attempt.InitialMessage,
		attempt.Summary,
		status,
		attempt.FailureReason,
	)
	if err != nil {
		return err
	}
	attempt.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	attempt.Status = status
	return nil
}

func UpdateAgentTaskAttempt(db *sql.DB, attempt *AgentTaskAttempt) error {
	_, err := db.Exec(`
		UPDATE agent_task_attempts
		SET
			decision_id = ?,
			agent_task_name = ?,
			repo_ref = ?,
			repository = ?,
			task_type = ?,
			risk = ?,
			agent = ?,
			repo_mode = ?,
			branch = ?,
			session_name = ?,
			managed_session_id = ?,
			work_dir = ?,
			launch_command = ?,
			initial_message = ?,
			summary = ?,
			status = ?,
			failure_reason = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		attempt.DecisionID,
		attempt.AgentTaskName,
		attempt.RepoRef,
		attempt.Repository,
		attempt.TaskType,
		attempt.Risk,
		attempt.Agent,
		attempt.RepoMode,
		attempt.Branch,
		attempt.SessionName,
		attempt.ManagedSessionID,
		attempt.WorkDir,
		attempt.LaunchCommand,
		attempt.InitialMessage,
		attempt.Summary,
		attempt.Status,
		attempt.FailureReason,
		attempt.ID,
	)
	return err
}

func ListAgentTaskAttempts(db *sql.DB) ([]AgentTaskAttempt, error) {
	rows, err := db.Query(`
		SELECT
			id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			repo_mode,
			branch,
			session_name,
			managed_session_id,
			work_dir,
			launch_command,
			initial_message,
			summary,
			status,
			failure_reason,
			created_at,
			updated_at
		FROM agent_task_attempts
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []AgentTaskAttempt
	for rows.Next() {
		attempt, err := scanAgentTaskAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func GetAgentTaskAttempt(db *sql.DB, id int64) (*AgentTaskAttempt, error) {
	rows, err := db.Query(`
		SELECT
			id,
			decision_id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			agent,
			repo_mode,
			branch,
			session_name,
			managed_session_id,
			work_dir,
			launch_command,
			initial_message,
			summary,
			status,
			failure_reason,
			created_at,
			updated_at
		FROM agent_task_attempts
		WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	attempt, err := scanAgentTaskAttempt(rows)
	if err != nil {
		return nil, err
	}
	return &attempt, rows.Err()
}

func CountAgentTaskAttempts(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_task_attempts`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanAgentTaskAttempt(scanner taskProposalRowScanner) (AgentTaskAttempt, error) {
	var attempt AgentTaskAttempt
	if err := scanner.Scan(
		&attempt.ID,
		&attempt.DecisionID,
		&attempt.AgentTaskName,
		&attempt.RepoRef,
		&attempt.Repository,
		&attempt.TaskType,
		&attempt.Risk,
		&attempt.Agent,
		&attempt.RepoMode,
		&attempt.Branch,
		&attempt.SessionName,
		&attempt.ManagedSessionID,
		&attempt.WorkDir,
		&attempt.LaunchCommand,
		&attempt.InitialMessage,
		&attempt.Summary,
		&attempt.Status,
		&attempt.FailureReason,
		&attempt.CreatedAt,
		&attempt.UpdatedAt,
	); err != nil {
		return AgentTaskAttempt{}, err
	}
	return attempt, nil
}
