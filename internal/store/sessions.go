package store

import (
	"database/sql"
	"time"
)

// Session represents a tracked agent session.
type Session struct {
	ID            string
	Agent         string
	Repository    string
	SessionID     string
	CWD           string
	GitBranch     string
	ZellijSession string
	Status        string
	BlockedReason string
	Alive         bool
	LastMessage   string
	LastRole      string
	LastActive    time.Time
	PRNumber      int
	PRURL         string
	PRState       string
	TaskSummary   string
	Role          string
	Archived      bool
	IsLoop        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// UpsertSession inserts or updates a session record.
func UpsertSession(db *sql.DB, s *Session) error {
	role := s.Role
	if role == "" {
		role = "worker"
	}
	_, err := db.Exec(`
		INSERT INTO sessions (id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			agent=excluded.agent, repository=excluded.repository,
			session_id=excluded.session_id, cwd=excluded.cwd,
			git_branch=excluded.git_branch,
			zellij_session=CASE WHEN excluded.zellij_session != '' THEN excluded.zellij_session ELSE sessions.zellij_session END,
			status=excluded.status, blocked_reason=excluded.blocked_reason,
			alive=excluded.alive,
			last_message=excluded.last_message, last_role=excluded.last_role,
			last_active=excluded.last_active,
			pr_number=CASE WHEN excluded.pr_number IS NOT NULL AND excluded.pr_number != 0 THEN excluded.pr_number ELSE sessions.pr_number END,
			pr_url=CASE WHEN excluded.pr_url != '' THEN excluded.pr_url ELSE sessions.pr_url END,
			pr_state=CASE WHEN excluded.pr_state != '' THEN excluded.pr_state ELSE sessions.pr_state END,
			task_summary=CASE WHEN (sessions.task_summary IS NULL OR sessions.task_summary = '') AND excluded.task_summary != '' THEN excluded.task_summary ELSE sessions.task_summary END,
			role=CASE WHEN excluded.role != 'worker' THEN excluded.role ELSE sessions.role END,
			archived=sessions.archived,
			is_loop=CASE WHEN sessions.is_loop=1 THEN 1 ELSE excluded.is_loop END,
			updated_at=CURRENT_TIMESTAMP`,
		s.ID, s.Agent, s.Repository, s.SessionID, s.CWD, s.GitBranch,
		s.ZellijSession, s.Status, s.BlockedReason, s.Alive, s.LastMessage, s.LastRole, s.LastActive,
		s.PRNumber, s.PRURL, s.PRState, s.TaskSummary, role, s.Archived, s.IsLoop)
	return err
}

// GetSession retrieves a session by ID.
func GetSession(db *sql.DB, id string) (*Session, error) {
	s := &Session{}
	var alive, archived, isLoop int
	var prNumber sql.NullInt64
	var lastActive sql.NullTime
	err := db.QueryRow(`SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE id = ?`, id).Scan(
		&s.ID, &s.Agent, &s.Repository, &s.SessionID, &s.CWD, &s.GitBranch,
		&s.ZellijSession, &s.Status, &s.BlockedReason, &alive, &s.LastMessage, &s.LastRole, &lastActive,
		&prNumber, &s.PRURL, &s.PRState, &s.TaskSummary, &s.Role, &archived, &isLoop, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Alive = alive != 0
	s.Archived = archived != 0
	s.IsLoop = isLoop != 0
	if prNumber.Valid {
		s.PRNumber = int(prNumber.Int64)
	}
	if lastActive.Valid {
		s.LastActive = lastActive.Time
	}
	return s, nil
}

// ListSessions returns all sessions ordered by last_active descending.
func ListSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions ORDER BY last_active DESC`)
}

// ListActiveSessions returns non-archived sessions ordered by last_active descending.
func ListActiveSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE archived = 0 ORDER BY last_active DESC`)
}

// ListSessionsByStatus returns sessions with the given status.
func ListSessionsByStatus(db *sql.DB, status string) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE status = ? ORDER BY last_active DESC`, status)
}

// ListSessionsByAlive returns sessions filtered by alive status.
func ListSessionsByAlive(db *sql.DB, alive bool) ([]Session, error) {
	aliveInt := 0
	if alive {
		aliveInt = 1
	}
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE alive = ? ORDER BY last_active DESC`, aliveInt)
}

// ListAliveSessionsWithPR returns alive sessions that have a pr_url set.
func ListAliveSessionsWithPR(db *sql.DB) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE alive = 1 AND pr_url != '' ORDER BY last_active DESC`)
}

// ArchiveSession sets archived=1 for the given session ID.
func ArchiveSession(db *sql.DB, id string) error {
	_, err := db.Exec("UPDATE sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// UnarchiveSession restores a session so it appears in the default list.
func UnarchiveSession(db *sql.DB, id string) error {
	_, err := db.Exec("UPDATE sessions SET archived = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// MarkStaleSessionsDead marks sessions not in scannedIDs as dead and not alive.
func MarkStaleSessionsDead(db *sql.DB, scannedIDs []string) error {
	if len(scannedIDs) == 0 {
		_, err := db.Exec(`UPDATE sessions SET alive = 0, status = 'dead', blocked_reason = '', updated_at = CURRENT_TIMESTAMP
			WHERE archived = 0 AND alive = 1`)
		return err
	}
	placeholders := ""
	args := make([]any, len(scannedIDs))
	for i, id := range scannedIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	_, err := db.Exec(`UPDATE sessions SET alive = 0, status = 'dead', blocked_reason = '', updated_at = CURRENT_TIMESTAMP
		WHERE archived = 0 AND alive = 1 AND id NOT IN (`+placeholders+`)`, args...)
	return err
}

// FindSessionByCWD finds a session by exact CWD match.
// Returns the first matching alive session, or any session if no alive one exists.
func FindSessionByCWD(db *sql.DB, cwd string) (*Session, error) {
	sessions, err := querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE cwd = ? ORDER BY alive DESC, last_active DESC LIMIT 1`, cwd)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}

// FindSessionByRepository finds sessions whose repository contains the query (case-insensitive).
func FindSessionByRepository(db *sql.DB, query string) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE repository LIKE '%' || ? || '%' ORDER BY last_active DESC`, query)
}

// FindSessionByZellijSession finds sessions whose zellij_session contains the query (case-insensitive).
func FindSessionByZellijSession(db *sql.DB, query string) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions WHERE LOWER(zellij_session) LIKE '%' || LOWER(?) || '%' ORDER BY last_active DESC`, query)
}

// UpdateTaskSummary overwrites the task_summary for the given session ID.
func UpdateTaskSummary(db *sql.DB, id, summary string) error {
	_, err := db.Exec("UPDATE sessions SET task_summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", summary, id)
	return err
}

// DeleteSession removes a session by ID.
func DeleteSession(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// GetSessionPRURL returns the cached pr_url for a session ID, or "" if not found.
func GetSessionPRURL(db *sql.DB, id string) string {
	var prURL string
	err := db.QueryRow("SELECT pr_url FROM sessions WHERE id = ?", id).Scan(&prURL)
	if err != nil {
		return ""
	}
	return prURL
}

// SetSessionRole updates the role for sessions matching the given zellij session name.
func SetSessionRole(db *sql.DB, zellijSession string, role string) error {
	_, err := db.Exec("UPDATE sessions SET role = ? WHERE zellij_session = ?", role, zellijSession)
	return err
}

// MoveToArchive moves a session from sessions to sessions_archive.
func MoveToArchive(db *sql.DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT OR REPLACE INTO sessions_archive (id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, archived_at)
		SELECT id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, CURRENT_TIMESTAMP
		FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ArchiveDeadSessions moves all sessions with alive=0 from sessions to sessions_archive.
// Returns the number of sessions archived.
func ArchiveDeadSessions(db *sql.DB) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`INSERT OR REPLACE INTO sessions_archive (id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, archived_at)
		SELECT id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, CURRENT_TIMESTAMP
		FROM sessions WHERE alive = 0`)
	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()

	_, err = tx.Exec("DELETE FROM sessions WHERE alive = 0")
	if err != nil {
		return 0, err
	}

	return int(count), tx.Commit()
}

// ListArchivedSessions returns all sessions from the archive table.
func ListArchivedSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions_archive ORDER BY last_active DESC`)
}

// ListAllSessionsWithArchive returns sessions from both tables via UNION ALL.
func ListAllSessionsWithArchive(db *sql.DB) ([]Session, error) {
	return querySessions(db, `SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions
		UNION ALL
		SELECT id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at
		FROM sessions_archive
		ORDER BY last_active DESC`)
}

// GetArchivedSessionCount returns the number of sessions in the archive table.
func GetArchivedSessionCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sessions_archive").Scan(&count)
	return count, err
}

func querySessions(db *sql.DB, query string, args ...any) ([]Session, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var alive, archived, isLoop int
		var prNumber sql.NullInt64
		var lastActive sql.NullTime
		if err := rows.Scan(
			&s.ID, &s.Agent, &s.Repository, &s.SessionID, &s.CWD, &s.GitBranch,
			&s.ZellijSession, &s.Status, &s.BlockedReason, &alive, &s.LastMessage, &s.LastRole, &lastActive,
			&prNumber, &s.PRURL, &s.PRState, &s.TaskSummary, &s.Role, &archived, &isLoop, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		s.Alive = alive != 0
		s.Archived = archived != 0
		s.IsLoop = isLoop != 0
		if prNumber.Valid {
			s.PRNumber = int(prNumber.Int64)
		}
		if lastActive.Valid {
			s.LastActive = lastActive.Time
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
