package store

import "database/sql"

// AgentTask is the local status-bearing representation of an AgentTask manifest
// or a materialized proposal adoption.
type AgentTask struct {
	Name                        string   `json:"name"`
	RepoRef                     string   `json:"repoRef"`
	Repository                  string   `json:"repository"`
	Objective                   string   `json:"objective"`
	TaskType                    string   `json:"taskType"`
	Risk                        string   `json:"risk"`
	ContextRefs                 []string `json:"contextRefs"`
	DesiredOutcome              []string `json:"desiredOutcome"`
	RoutingPolicyRef            string   `json:"routingPolicyRef"`
	ReviewPolicyRef             string   `json:"reviewPolicyRef"`
	ApprovalPolicyRef           string   `json:"approvalPolicyRef"`
	ApprovalRequiredBeforeMerge bool     `json:"approvalRequiredBeforeMerge"`
	SourceKind                  string   `json:"sourceKind"`
	SourceRef                   string   `json:"sourceRef"`
	Status                      string   `json:"status"`
	SourcePath                  string   `json:"sourcePath"`
	SourceCommit                string   `json:"sourceCommit"`
	SpecHash                    string   `json:"specHash"`
	RawSpecJSON                 string   `json:"rawSpecJSON"`
	CreatedAt                   string   `json:"createdAt"`
	UpdatedAt                   string   `json:"updatedAt"`
}

func UpsertAgentTask(db *sql.DB, task *AgentTask) error {
	contextRefsJSON, err := marshalStringSlice(task.ContextRefs)
	if err != nil {
		return err
	}
	desiredOutcomeJSON, err := marshalStringSlice(task.DesiredOutcome)
	if err != nil {
		return err
	}
	approvalRequired := 0
	if task.ApprovalRequiredBeforeMerge {
		approvalRequired = 1
	}

	_, err = db.Exec(`
		INSERT INTO agent_tasks (
			name,
			repo_ref,
			repository,
			objective,
			task_type,
			risk,
			context_refs_json,
			desired_outcome_json,
			routing_policy_ref,
			review_policy_ref,
			approval_policy_ref,
			approval_required_before_merge,
			source_kind,
			source_ref,
			status,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET
			repo_ref = excluded.repo_ref,
			repository = excluded.repository,
			objective = excluded.objective,
			task_type = excluded.task_type,
			risk = excluded.risk,
			context_refs_json = excluded.context_refs_json,
			desired_outcome_json = excluded.desired_outcome_json,
			routing_policy_ref = excluded.routing_policy_ref,
			review_policy_ref = excluded.review_policy_ref,
			approval_policy_ref = excluded.approval_policy_ref,
			approval_required_before_merge = excluded.approval_required_before_merge,
			source_kind = excluded.source_kind,
			source_ref = excluded.source_ref,
			status = excluded.status,
			source_path = excluded.source_path,
			source_commit = excluded.source_commit,
			spec_hash = excluded.spec_hash,
			raw_spec_json = excluded.raw_spec_json,
			updated_at = CURRENT_TIMESTAMP
	`, task.Name, task.RepoRef, task.Repository, task.Objective, task.TaskType, task.Risk,
		contextRefsJSON, desiredOutcomeJSON, task.RoutingPolicyRef, task.ReviewPolicyRef,
		task.ApprovalPolicyRef, approvalRequired, task.SourceKind, task.SourceRef, task.Status,
		task.SourcePath, task.SourceCommit, task.SpecHash, task.RawSpecJSON)
	return err
}

func GetAgentTask(db *sql.DB, name string) (*AgentTask, error) {
	rows, err := db.Query(`
		SELECT
			name,
			repo_ref,
			repository,
			objective,
			task_type,
			risk,
			context_refs_json,
			desired_outcome_json,
			routing_policy_ref,
			review_policy_ref,
			approval_policy_ref,
			approval_required_before_merge,
			source_kind,
			source_ref,
			status,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM agent_tasks
		WHERE name = ?`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	task, err := scanAgentTask(rows)
	if err != nil {
		return nil, err
	}
	return &task, rows.Err()
}

func ListAgentTasks(db *sql.DB) ([]AgentTask, error) {
	rows, err := db.Query(`
		SELECT
			name,
			repo_ref,
			repository,
			objective,
			task_type,
			risk,
			context_refs_json,
			desired_outcome_json,
			routing_policy_ref,
			review_policy_ref,
			approval_policy_ref,
			approval_required_before_merge,
			source_kind,
			source_ref,
			status,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM agent_tasks
		ORDER BY updated_at DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []AgentTask
	for rows.Next() {
		task, err := scanAgentTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func CountAgentTasks(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_tasks`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func UpdateAgentTaskStatus(db *sql.DB, name, status string) error {
	_, err := db.Exec(`UPDATE agent_tasks SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`, status, name)
	return err
}

func scanAgentTask(scanner taskProposalRowScanner) (AgentTask, error) {
	var task AgentTask
	var contextRefsJSON string
	var desiredOutcomeJSON string
	var approvalRequired int
	if err := scanner.Scan(
		&task.Name,
		&task.RepoRef,
		&task.Repository,
		&task.Objective,
		&task.TaskType,
		&task.Risk,
		&contextRefsJSON,
		&desiredOutcomeJSON,
		&task.RoutingPolicyRef,
		&task.ReviewPolicyRef,
		&task.ApprovalPolicyRef,
		&approvalRequired,
		&task.SourceKind,
		&task.SourceRef,
		&task.Status,
		&task.SourcePath,
		&task.SourceCommit,
		&task.SpecHash,
		&task.RawSpecJSON,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return AgentTask{}, err
	}
	var err error
	task.ContextRefs, err = unmarshalStringSlice(contextRefsJSON)
	if err != nil {
		return AgentTask{}, err
	}
	task.DesiredOutcome, err = unmarshalStringSlice(desiredOutcomeJSON)
	if err != nil {
		return AgentTask{}, err
	}
	task.ApprovalRequiredBeforeMerge = approvalRequired != 0
	return task, nil
}
