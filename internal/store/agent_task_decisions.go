package store

import (
	"database/sql"
	"encoding/json"
)

type AgentTaskDecisionCandidate struct {
	Agent     string  `json:"agent"`
	Score     float64 `json:"score"`
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
}

type AgentTaskDecision struct {
	ID                int64                        `json:"id"`
	AgentTaskName     string                       `json:"agentTaskName"`
	RepoRef           string                       `json:"repoRef"`
	Repository        string                       `json:"repository"`
	TaskType          string                       `json:"taskType"`
	Risk              string                       `json:"risk"`
	RoutingPolicyRef  string                       `json:"routingPolicyRef"`
	PolicyVersion     string                       `json:"policyVersion"`
	SelectionMode     string                       `json:"selectionMode"`
	SelectedAgent     string                       `json:"selectedAgent"`
	SelectedRepoMode  string                       `json:"selectedRepoMode"`
	RepoProfileSource string                       `json:"repoProfileSource"`
	ModeSource        string                       `json:"modeSource"`
	AgentSource       string                       `json:"agentSource"`
	EligibleAgents    []string                     `json:"eligibleAgents"`
	CandidateScores   []AgentTaskDecisionCandidate `json:"candidateScores"`
	RouteReason       string                       `json:"routeReason"`
	Status            string                       `json:"status"`
	CreatedAt         string                       `json:"createdAt"`
	UpdatedAt         string                       `json:"updatedAt"`
}

func CreateAgentTaskDecision(db *sql.DB, decision *AgentTaskDecision) error {
	eligibleAgentsJSON, err := marshalStringSlice(decision.EligibleAgents)
	if err != nil {
		return err
	}
	candidateScoresJSON, err := json.Marshal(decision.CandidateScores)
	if err != nil {
		return err
	}

	status := decision.Status
	if status == "" {
		status = "recorded"
	}

	res, err := db.Exec(`
		INSERT INTO agent_task_decisions (
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			routing_policy_ref,
			policy_version,
			selection_mode,
			selected_agent,
			selected_repo_mode,
			repo_profile_source,
			mode_source,
			agent_source,
			eligible_agents_json,
			candidate_scores_json,
			route_reason,
			status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.AgentTaskName,
		decision.RepoRef,
		decision.Repository,
		decision.TaskType,
		decision.Risk,
		decision.RoutingPolicyRef,
		decision.PolicyVersion,
		decision.SelectionMode,
		decision.SelectedAgent,
		decision.SelectedRepoMode,
		decision.RepoProfileSource,
		decision.ModeSource,
		decision.AgentSource,
		eligibleAgentsJSON,
		string(candidateScoresJSON),
		decision.RouteReason,
		status,
	)
	if err != nil {
		return err
	}
	decision.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	decision.Status = status
	return nil
}

func ListAgentTaskDecisions(db *sql.DB) ([]AgentTaskDecision, error) {
	rows, err := db.Query(`
		SELECT
			id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			routing_policy_ref,
			policy_version,
			selection_mode,
			selected_agent,
			selected_repo_mode,
			repo_profile_source,
			mode_source,
			agent_source,
			eligible_agents_json,
			candidate_scores_json,
			route_reason,
			status,
			created_at,
			updated_at
		FROM agent_task_decisions
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []AgentTaskDecision
	for rows.Next() {
		decision, err := scanAgentTaskDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func GetAgentTaskDecision(db *sql.DB, id int64) (*AgentTaskDecision, error) {
	rows, err := db.Query(`
		SELECT
			id,
			agent_task_name,
			repo_ref,
			repository,
			task_type,
			risk,
			routing_policy_ref,
			policy_version,
			selection_mode,
			selected_agent,
			selected_repo_mode,
			repo_profile_source,
			mode_source,
			agent_source,
			eligible_agents_json,
			candidate_scores_json,
			route_reason,
			status,
			created_at,
			updated_at
		FROM agent_task_decisions
		WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	decision, err := scanAgentTaskDecision(rows)
	if err != nil {
		return nil, err
	}
	return &decision, rows.Err()
}

func CountAgentTaskDecisions(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_task_decisions`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func UpdateAgentTaskDecisionStatus(db *sql.DB, id int64, status string) error {
	_, err := db.Exec(`UPDATE agent_task_decisions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func scanAgentTaskDecision(scanner taskProposalRowScanner) (AgentTaskDecision, error) {
	var decision AgentTaskDecision
	var eligibleAgentsJSON string
	var candidateScoresJSON string
	if err := scanner.Scan(
		&decision.ID,
		&decision.AgentTaskName,
		&decision.RepoRef,
		&decision.Repository,
		&decision.TaskType,
		&decision.Risk,
		&decision.RoutingPolicyRef,
		&decision.PolicyVersion,
		&decision.SelectionMode,
		&decision.SelectedAgent,
		&decision.SelectedRepoMode,
		&decision.RepoProfileSource,
		&decision.ModeSource,
		&decision.AgentSource,
		&eligibleAgentsJSON,
		&candidateScoresJSON,
		&decision.RouteReason,
		&decision.Status,
		&decision.CreatedAt,
		&decision.UpdatedAt,
	); err != nil {
		return AgentTaskDecision{}, err
	}
	var err error
	decision.EligibleAgents, err = unmarshalStringSlice(eligibleAgentsJSON)
	if err != nil {
		return AgentTaskDecision{}, err
	}
	if candidateScoresJSON != "" {
		if err := json.Unmarshal([]byte(candidateScoresJSON), &decision.CandidateScores); err != nil {
			return AgentTaskDecision{}, err
		}
	}
	return decision, nil
}
