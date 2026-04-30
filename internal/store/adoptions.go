package store

import (
	"database/sql"
	"time"
)

const (
	AdoptionStrategyProtected = "protected"
)

const (
	AdoptionStatusQueued    = "queued"
	AdoptionStatusApplied   = "applied"
	AdoptionStatusCancelled = "cancelled"
)

// SessionAdoption tracks a planned import of an unmanaged live session.
// Queue entries are intentionally separate from sessions so we can prepare
// metadata and protection defaults before touching the live runtime state.
type SessionAdoption struct {
	ID                    int64
	Agent                 string
	Mux                   string
	ZellijSession         string
	ExternalSessionID     string
	Repository            string
	CWD                   string
	GitBranch             string
	Strategy              string
	TargetPermissionLevel int
	Status                string
	ManagedSessionID      string
	Note                  string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// CreateSessionAdoption inserts a queued adoption plan.
func CreateSessionAdoption(db *sql.DB, a *SessionAdoption) error {
	mux := a.Mux
	if mux == "" {
		mux = "zellij"
	}
	strategy := a.Strategy
	if strategy == "" {
		strategy = AdoptionStrategyProtected
	}
	status := a.Status
	if status == "" {
		status = AdoptionStatusQueued
	}
	permissionLevel := a.TargetPermissionLevel
	if permissionLevel == 0 {
		permissionLevel = PermissionSuggest
	}

	res, err := db.Exec(`INSERT INTO session_adoptions (
		agent, mux, zellij_session, external_session_id, repository, cwd, git_branch,
		strategy, target_permission_level, status, managed_session_id, note
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Agent, mux, a.ZellijSession, a.ExternalSessionID, a.Repository, a.CWD, a.GitBranch,
		strategy, permissionLevel, status, a.ManagedSessionID, a.Note,
	)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	a.Mux = mux
	a.Strategy = strategy
	a.Status = status
	a.TargetPermissionLevel = permissionLevel
	return nil
}

// ListSessionAdoptions returns all adoption plans ordered by newest first.
func ListSessionAdoptions(db *sql.DB) ([]SessionAdoption, error) {
	return querySessionAdoptions(db, `SELECT id, agent, mux, zellij_session, external_session_id,
		repository, cwd, git_branch, strategy, target_permission_level, status, managed_session_id,
		note, created_at, updated_at
		FROM session_adoptions
		ORDER BY created_at DESC, id DESC`)
}

// ListQueuedSessionAdoptions returns only queued adoption plans.
func ListQueuedSessionAdoptions(db *sql.DB) ([]SessionAdoption, error) {
	return querySessionAdoptions(db, `SELECT id, agent, mux, zellij_session, external_session_id,
		repository, cwd, git_branch, strategy, target_permission_level, status, managed_session_id,
		note, created_at, updated_at
		FROM session_adoptions
		WHERE status = ?
		ORDER BY created_at DESC, id DESC`, AdoptionStatusQueued)
}

// GetQueuedSessionAdoptionByZellijSession returns the newest queued adoption for a mux session.
func GetQueuedSessionAdoptionByZellijSession(db *sql.DB, muxName, zellijSession string) (*SessionAdoption, error) {
	items, err := querySessionAdoptions(db, `SELECT id, agent, mux, zellij_session, external_session_id,
		repository, cwd, git_branch, strategy, target_permission_level, status, managed_session_id,
		note, created_at, updated_at
		FROM session_adoptions
		WHERE mux = ? AND LOWER(zellij_session) = LOWER(?) AND status = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, muxName, zellijSession, AdoptionStatusQueued)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return &items[0], nil
}

// GetSessionAdoption returns one adoption by id.
func GetSessionAdoption(db *sql.DB, id int64) (*SessionAdoption, error) {
	items, err := querySessionAdoptions(db, `SELECT id, agent, mux, zellij_session, external_session_id,
		repository, cwd, git_branch, strategy, target_permission_level, status, managed_session_id,
		note, created_at, updated_at
		FROM session_adoptions
		WHERE id = ?
		LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return &items[0], nil
}

// UpdateSessionAdoptionStatus updates a queued adoption lifecycle state.
func UpdateSessionAdoptionStatus(db *sql.DB, id int64, status string) error {
	_, err := db.Exec(`UPDATE session_adoptions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func querySessionAdoptions(db *sql.DB, query string, args ...any) ([]SessionAdoption, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var adoptions []SessionAdoption
	for rows.Next() {
		var adoption SessionAdoption
		if err := rows.Scan(
			&adoption.ID,
			&adoption.Agent,
			&adoption.Mux,
			&adoption.ZellijSession,
			&adoption.ExternalSessionID,
			&adoption.Repository,
			&adoption.CWD,
			&adoption.GitBranch,
			&adoption.Strategy,
			&adoption.TargetPermissionLevel,
			&adoption.Status,
			&adoption.ManagedSessionID,
			&adoption.Note,
			&adoption.CreatedAt,
			&adoption.UpdatedAt,
		); err != nil {
			return nil, err
		}
		adoptions = append(adoptions, adoption)
	}

	return adoptions, rows.Err()
}
