package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// Permission levels for sessions (inspired by Anthropic's graduated constraint model).
//
//	1 = suggest   : agent can only suggest actions, human approves all
//	2 = auto-read : agent can read files/run queries autonomously
//	3 = auto-edit : agent can edit files and run tests
//	4 = auto-run  : agent can execute arbitrary commands
//	5 = full-auto : agent can push, create PRs, deploy without confirmation
const (
	PermissionSuggest  = 1
	PermissionAutoRead = 2
	PermissionAutoEdit = 3
	PermissionAutoRun  = 4
	PermissionFullAuto = 5
)

// PermissionLabel returns a human-readable label for a permission level.
func PermissionLabel(level int) string {
	switch level {
	case PermissionSuggest:
		return "suggest"
	case PermissionAutoRead:
		return "auto-read"
	case PermissionAutoEdit:
		return "auto-edit"
	case PermissionAutoRun:
		return "auto-run"
	case PermissionFullAuto:
		return "full-auto"
	default:
		return "unknown"
	}
}

// Session represents a tracked agent session.
type Session struct {
	ID              string
	Agent           string
	Repository      string
	SessionID       string
	CWD             string
	GitBranch       string
	ZellijSession   string
	Status          string
	BlockedReason   string
	DesiredState    string // "running", "stopped"
	Alive           bool   // deprecated alias for DesiredState == "running"
	LastMessage     string
	LastRole        string
	LastActive      time.Time
	LastSentAt      time.Time
	LastMessageAt   time.Time
	LastSeenAliveAt time.Time
	PRNumber        int
	PRURL           string
	PRState         string
	TaskSummary     string
	Role            string
	Archived        bool
	IsLoop          bool
	IsProtected     bool
	PermissionLevel int
	RuntimeStatus   string // "running", "exited", "gone"
	LifecycleState  string // "spawning", "running", "killing", "stopped"
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const (
	DesiredStateRunning = "running"
	DesiredStateStopped = "stopped"
)

const (
	LifecycleStateSpawning = "spawning"
	LifecycleStateRunning  = "running"
	LifecycleStateKilling  = "killing"
	LifecycleStateStopped  = "stopped"
)

const sessionSelectColumns = `id, agent, repository, session_id, cwd, git_branch,
	zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
	last_sent_at, last_message_at, last_seen_alive_at,
	pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at`
const sessionSelectArchiveColumns = `id, agent, repository, session_id, cwd, git_branch,
	zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
	last_sent_at, last_message_at, last_seen_alive_at,
	pr_number, pr_url, pr_state, task_summary,
	CASE WHEN role IN ('worker', 'director', 'secretary', 'lead') THEN role ELSE 'worker' END AS role,
	1 AS archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at`

const sessionSelectFromSessions = "SELECT " + sessionSelectColumns + " FROM sessions"
const sessionSelectFromArchive = "SELECT " + sessionSelectArchiveColumns + " FROM sessions_archive"

// WantsRunning reports whether the user-intent state for this session is running.
func (s Session) WantsRunning() bool {
	if s.DesiredState != "" {
		return s.DesiredState == DesiredStateRunning
	}
	return s.Alive
}

// ObservedActivityAt returns the best-effort latest timestamp that indicates recent session activity.
// It keeps legacy last_active as a fallback, but prefers explicit sent/message/alive timestamps.
func (s Session) ObservedActivityAt() time.Time {
	return maxSessionTimes(s.LastActive, s.LastSentAt, s.LastMessageAt, s.LastSeenAliveAt)
}

// UpsertSession inserts or updates a session record.
func UpsertSession(db *sql.DB, s *Session) error {
	role := s.Role
	if role == "" {
		role = "worker"
	}
	permLevel := s.PermissionLevel
	if permLevel == 0 {
		permLevel = PermissionSuggest
	}
	runtimeStatus := s.RuntimeStatus
	if runtimeStatus == "" {
		runtimeStatus = "gone"
	}
	lifecycleState := s.LifecycleState
	if lifecycleState == "" {
		lifecycleState = LifecycleStateRunning
	}
	desiredState := s.DesiredState
	if desiredState == "" {
		if s.Alive {
			desiredState = DesiredStateRunning
		} else {
			desiredState = DesiredStateStopped
		}
	}
	legacyLastActive := maxSessionTimes(s.LastActive, s.LastSentAt, s.LastMessageAt, s.LastSeenAliveAt)
	_, err := db.Exec(`
		INSERT INTO sessions (id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
			last_sent_at, last_message_at, last_seen_alive_at,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			agent=excluded.agent, repository=excluded.repository,
			session_id=excluded.session_id, cwd=excluded.cwd,
			git_branch=excluded.git_branch,
			zellij_session=CASE WHEN excluded.zellij_session != '' THEN excluded.zellij_session ELSE sessions.zellij_session END,
			status=excluded.status, blocked_reason=excluded.blocked_reason,
			desired_state=excluded.desired_state,
			last_message=excluded.last_message, last_role=excluded.last_role,
			last_active=CASE WHEN excluded.last_active IS NOT NULL THEN excluded.last_active ELSE sessions.last_active END,
			last_sent_at=CASE WHEN excluded.last_sent_at IS NOT NULL THEN excluded.last_sent_at ELSE sessions.last_sent_at END,
			last_message_at=CASE WHEN excluded.last_message_at IS NOT NULL THEN excluded.last_message_at ELSE sessions.last_message_at END,
			last_seen_alive_at=CASE WHEN excluded.last_seen_alive_at IS NOT NULL THEN excluded.last_seen_alive_at ELSE sessions.last_seen_alive_at END,
			pr_number=CASE WHEN excluded.pr_number IS NOT NULL AND excluded.pr_number != 0 THEN excluded.pr_number ELSE sessions.pr_number END,
			pr_url=CASE WHEN excluded.pr_url != '' THEN excluded.pr_url ELSE sessions.pr_url END,
			pr_state=CASE WHEN excluded.pr_state != '' THEN excluded.pr_state ELSE sessions.pr_state END,
			task_summary=CASE WHEN (sessions.task_summary IS NULL OR sessions.task_summary = '') AND excluded.task_summary != '' THEN excluded.task_summary ELSE sessions.task_summary END,
			role=CASE WHEN excluded.role != 'worker' THEN excluded.role ELSE sessions.role END,
			archived=sessions.archived,
			is_loop=CASE WHEN sessions.is_loop=1 THEN 1 ELSE excluded.is_loop END,
			is_protected=CASE WHEN sessions.is_protected=1 THEN 1 ELSE excluded.is_protected END,
			permission_level=CASE WHEN excluded.permission_level > 1 THEN excluded.permission_level ELSE sessions.permission_level END,
			runtime_status=excluded.runtime_status,
			lifecycle_state=excluded.lifecycle_state,
			updated_at=CURRENT_TIMESTAMP`,
		s.ID, s.Agent, s.Repository, s.SessionID, s.CWD, s.GitBranch,
		s.ZellijSession, s.Status, s.BlockedReason, desiredState, s.LastMessage, s.LastRole, nullableSessionTimeArg(legacyLastActive),
		nullableSessionTimeArg(s.LastSentAt), nullableSessionTimeArg(s.LastMessageAt), nullableSessionTimeArg(s.LastSeenAliveAt),
		s.PRNumber, s.PRURL, s.PRState, s.TaskSummary, role, s.Archived, s.IsLoop, s.IsProtected, permLevel, runtimeStatus, lifecycleState)
	return err
}

// GetSession retrieves a session by ID.
func GetSession(db *sql.DB, id string) (*Session, error) {
	s := &Session{}
	var archived, isLoop, isProtected int
	var prNumber nullableSessionInt
	var lastActive, lastSentAt, lastMessageAt, lastSeenAliveAt nullableSessionTime
	var createdAt, updatedAt nullableSessionTime
	err := db.QueryRow(sessionSelectFromSessions+` WHERE id = ?`, id).Scan(
		&s.ID, &s.Agent, &s.Repository, &s.SessionID, &s.CWD, &s.GitBranch,
		&s.ZellijSession, &s.Status, &s.BlockedReason, &s.DesiredState, &s.LastMessage, &s.LastRole, &lastActive,
		&lastSentAt, &lastMessageAt, &lastSeenAliveAt,
		&prNumber, &s.PRURL, &s.PRState, &s.TaskSummary, &s.Role, &archived, &isLoop, &isProtected, &s.PermissionLevel, &s.RuntimeStatus, &s.LifecycleState, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	s.Alive = s.WantsRunning()
	s.Archived = archived != 0
	s.IsLoop = isLoop != 0
	s.IsProtected = isProtected != 0
	if prNumber.Valid {
		s.PRNumber = int(prNumber.Int64)
	}
	if lastActive.Valid {
		s.LastActive = lastActive.Time
	}
	if lastSentAt.Valid {
		s.LastSentAt = lastSentAt.Time
	}
	if lastMessageAt.Valid {
		s.LastMessageAt = lastMessageAt.Time
	}
	if lastSeenAliveAt.Valid {
		s.LastSeenAliveAt = lastSeenAliveAt.Time
	}
	if createdAt.Valid {
		s.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		s.UpdatedAt = updatedAt.Time
	}
	return s, nil
}

// GetSessionAny retrieves a session by ID from either the active or archive table.
func GetSessionAny(db *sql.DB, id string) (*Session, error) {
	s, err := GetSession(db, id)
	if err == nil {
		return s, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	sessions, err := querySessions(db, sessionSelectFromArchive+` WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}

// GetActiveSessionByKey retrieves an active session by manager ID, provider session ID, or zellij session name.
func GetActiveSessionByKey(db *sql.DB, key string) (*Session, error) {
	if s, err := GetSession(db, key); err == nil {
		return s, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	if s, err := getSessionBySessionIDFromTable(db, "sessions", key); err == nil {
		return s, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return getSessionByZellijSessionFromTable(db, "sessions", key)
}

// GetSessionBySessionID retrieves the most recent session with the given provider session ID.
func GetSessionBySessionID(db *sql.DB, sessionID string) (*Session, error) {
	if s, err := getSessionBySessionIDFromTable(db, "sessions", sessionID); err == nil {
		return s, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return getSessionBySessionIDFromTable(db, "sessions_archive", sessionID)
}

// GetSessionByZellijSession retrieves the most recent session with the given zellij session name.
func GetSessionByZellijSession(db *sql.DB, zellijSession string) (*Session, error) {
	if s, err := getSessionByZellijSessionFromTable(db, "sessions", zellijSession); err == nil {
		return s, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return getSessionByZellijSessionFromTable(db, "sessions_archive", zellijSession)
}

// ListSessions returns all sessions ordered by last_active descending.
func ListSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+` ORDER BY last_active DESC`)
}

// ListActiveSessions returns non-archived sessions ordered by last_active descending.
func ListActiveSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+` WHERE archived = 0 ORDER BY last_active DESC`)
}

// ListSessionsByStatus returns sessions with the given status.
func ListSessionsByStatus(db *sql.DB, status string) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+` WHERE status = ? ORDER BY last_active DESC`, status)
}

// ListSessionsByAlive returns sessions filtered by alive status.
func ListSessionsByAlive(db *sql.DB, alive bool) ([]Session, error) {
	desiredState := DesiredStateStopped
	if alive {
		desiredState = DesiredStateRunning
	}
	return querySessions(db, sessionSelectFromSessions+` WHERE desired_state = ? ORDER BY last_active DESC`, desiredState)
}

// ListAliveSessionsWithPR returns alive sessions that have a pr_url set.
func ListAliveSessionsWithPR(db *sql.DB) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+` WHERE desired_state = 'running' AND pr_url != '' ORDER BY last_active DESC`)
}

// ArchiveSession sets archived=1 for the given session ID.
func ArchiveSession(db *sql.DB, id string) error {
	_, err := db.Exec("UPDATE sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// MarkSessionDeadByZellijSession marks a session dead by zellij session name.
func MarkSessionDeadByZellijSession(db *sql.DB, zellijSession string) (int64, error) {
	result, err := db.Exec(
		`UPDATE sessions
		 SET desired_state = 'stopped',
		     status = 'dead',
		     blocked_reason = '',
		     runtime_status = 'gone',
		     lifecycle_state = 'stopped',
		     updated_at = CURRENT_TIMESTAMP
		 WHERE zellij_session = ?`,
		zellijSession,
	)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// UnarchiveSession restores a session so it appears in the default list.
func UnarchiveSession(db *sql.DB, id string) error {
	_, err := db.Exec("UPDATE sessions SET archived = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// MarkStaleSessionsDead marks sessions not in scannedIDs as dead and not alive.
func MarkStaleSessionsDead(db *sql.DB, scannedIDs []string) error {
	if len(scannedIDs) == 0 {
		_, err := db.Exec(`UPDATE sessions SET desired_state = 'stopped', status = 'dead', blocked_reason = '', updated_at = CURRENT_TIMESTAMP
			WHERE archived = 0 AND desired_state = 'running'`)
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
	_, err := db.Exec(`UPDATE sessions SET desired_state = 'stopped', status = 'dead', blocked_reason = '', updated_at = CURRENT_TIMESTAMP
		WHERE archived = 0 AND desired_state = 'running' AND id NOT IN (`+placeholders+`)`, args...)
	return err
}

// FindSessionByCWD finds a session by exact CWD match.
// Returns the first matching alive session, or any session if no alive one exists.
func FindSessionByCWD(db *sql.DB, cwd string) (*Session, error) {
	sessions, err := querySessions(db, sessionSelectFromSessions+` WHERE cwd = ? ORDER BY CASE WHEN desired_state = 'running' THEN 1 ELSE 0 END DESC, last_active DESC LIMIT 1`, cwd)
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
	return querySessions(db, sessionSelectFromSessions+` WHERE repository LIKE '%' || ? || '%' ORDER BY last_active DESC`, query)
}

// FindSessionByZellijSession finds sessions whose zellij_session contains the query (case-insensitive).
func FindSessionByZellijSession(db *sql.DB, query string) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+` WHERE LOWER(zellij_session) LIKE '%' || LOWER(?) || '%' ORDER BY last_active DESC`, query)
}

// UpdateSessionMetadata updates JSONL-derived metadata for an existing session.
// This is UPDATE-only — it will NOT create a new record if the ID doesn't exist.
func UpdateSessionMetadata(db *sql.DB, s *Session) error {
	messageAt := maxSessionTimes(s.LastMessageAt, s.LastActive)
	_, err := db.Exec(`UPDATE sessions SET
		status = ?, git_branch = ?, last_message = ?, last_role = ?,
		last_active = COALESCE(?, last_active), last_message_at = COALESCE(?, last_message_at), role = ?, is_loop = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		s.Status, s.GitBranch, s.LastMessage, s.LastRole,
		nullableSessionTimeArg(messageAt), nullableSessionTimeArg(messageAt), s.Role, s.IsLoop, s.ID)
	return err
}

// TouchSessionLastSentByZellijSession records when an instruction was last sent to a session.
func TouchSessionLastSentByZellijSession(db *sql.DB, zellijSession string, at time.Time) (int64, error) {
	result, err := db.Exec(`UPDATE sessions
		SET last_sent_at = ?, last_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE zellij_session = ?`,
		nullableSessionTimeArg(at), nullableSessionTimeArg(at), zellijSession)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// TouchSessionLastSeenAliveByZellijSession records the latest time a session was observed alive in the mux runtime.
func TouchSessionLastSeenAliveByZellijSession(db *sql.DB, zellijSession string, at time.Time) (int64, error) {
	result, err := db.Exec(`UPDATE sessions
		SET last_seen_alive_at = ?, last_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE zellij_session = ?`,
		nullableSessionTimeArg(at), nullableSessionTimeArg(at), zellijSession)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdateTaskSummary overwrites the task_summary for the given session ID.
func UpdateTaskSummary(db *sql.DB, id, summary string) error {
	_, err := db.Exec("UPDATE sessions SET task_summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", summary, id)
	return err
}

// ClearTaskSummariesForActiveSessions clears task_summary for non-archived sessions that are not already dead.
func ClearTaskSummariesForActiveSessions(db *sql.DB) (int64, error) {
	result, err := db.Exec(`UPDATE sessions
		SET task_summary = '', updated_at = CURRENT_TIMESTAMP
		WHERE archived = 0 AND status != 'dead'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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

// SetPermissionLevel updates the permission level for a session.
func SetPermissionLevel(db *sql.DB, id string, level int) error {
	_, err := db.Exec("UPDATE sessions SET permission_level = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", level, id)
	return err
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
		zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
		last_sent_at, last_message_at, last_seen_alive_at,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at, archived_at)
		SELECT id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
			last_sent_at, last_message_at, last_seen_alive_at,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at, CURRENT_TIMESTAMP
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

// ArchiveDeadSessions moves stopped sessions that are confirmed gone to sessions_archive.
// Returns the number of sessions archived.
func ArchiveDeadSessions(db *sql.DB) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`INSERT OR REPLACE INTO sessions_archive (id, agent, repository, session_id, cwd, git_branch,
		zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
		last_sent_at, last_message_at, last_seen_alive_at,
		pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at, archived_at)
		SELECT id, agent, repository, session_id, cwd, git_branch,
			zellij_session, status, blocked_reason, desired_state, last_message, last_role, last_active,
			last_sent_at, last_message_at, last_seen_alive_at,
			pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, is_protected, permission_level, runtime_status, lifecycle_state, created_at, updated_at, CURRENT_TIMESTAMP
		FROM sessions WHERE desired_state = 'stopped' AND runtime_status = 'gone'`)
	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()

	_, err = tx.Exec("DELETE FROM sessions WHERE desired_state = 'stopped' AND runtime_status = 'gone'")
	if err != nil {
		return 0, err
	}

	return int(count), tx.Commit()
}

// ListArchivedSessions returns all sessions from the archive table.
func ListArchivedSessions(db *sql.DB) ([]Session, error) {
	return querySessions(db, sessionSelectFromArchive+` ORDER BY last_active DESC`)
}

// ListAllSessionsWithArchive returns sessions from both tables via UNION ALL.
func ListAllSessionsWithArchive(db *sql.DB) ([]Session, error) {
	return querySessions(db, sessionSelectFromSessions+`
		UNION ALL
		`+sessionSelectFromArchive+`
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
		var archived, isLoop, isProtected int
		var prNumber nullableSessionInt
		var lastActive, lastSentAt, lastMessageAt, lastSeenAliveAt nullableSessionTime
		var createdAt, updatedAt nullableSessionTime
		if err := rows.Scan(
			&s.ID, &s.Agent, &s.Repository, &s.SessionID, &s.CWD, &s.GitBranch,
			&s.ZellijSession, &s.Status, &s.BlockedReason, &s.DesiredState, &s.LastMessage, &s.LastRole, &lastActive,
			&lastSentAt, &lastMessageAt, &lastSeenAliveAt,
			&prNumber, &s.PRURL, &s.PRState, &s.TaskSummary, &s.Role, &archived, &isLoop, &isProtected, &s.PermissionLevel, &s.RuntimeStatus, &s.LifecycleState, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		s.Alive = s.WantsRunning()
		s.Archived = archived != 0
		s.IsLoop = isLoop != 0
		s.IsProtected = isProtected != 0
		if prNumber.Valid {
			s.PRNumber = int(prNumber.Int64)
		}
		if lastActive.Valid {
			s.LastActive = lastActive.Time
		}
		if lastSentAt.Valid {
			s.LastSentAt = lastSentAt.Time
		}
		if lastMessageAt.Valid {
			s.LastMessageAt = lastMessageAt.Time
		}
		if lastSeenAliveAt.Valid {
			s.LastSeenAliveAt = lastSeenAliveAt.Time
		}
		if createdAt.Valid {
			s.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			s.UpdatedAt = updatedAt.Time
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func maxSessionTimes(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if latest.IsZero() || value.After(latest) {
			latest = value
		}
	}
	return latest
}

func nullableSessionTimeArg(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

type nullableSessionTime struct {
	Time  time.Time
	Valid bool
}

type nullableSessionInt struct {
	Int64 int64
	Valid bool
}

func (i *nullableSessionInt) Scan(value any) error {
	if value == nil {
		i.Valid = false
		i.Int64 = 0
		return nil
	}
	switch v := value.(type) {
	case int64:
		i.Int64 = v
		i.Valid = true
		return nil
	case int:
		i.Int64 = int64(v)
		i.Valid = true
		return nil
	case string:
		return i.scanString(v)
	case []byte:
		return i.scanString(string(v))
	default:
		return fmt.Errorf("unsupported session integer type %T", value)
	}
}

func (i *nullableSessionInt) scanString(value string) error {
	if value == "" {
		i.Valid = false
		i.Int64 = 0
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("unsupported session integer value %q", value)
	}
	i.Int64 = parsed
	i.Valid = true
	return nil
}

func (t *nullableSessionTime) Scan(value any) error {
	if value == nil {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t.Valid = !v.IsZero()
		t.Time = v
		return nil
	case string:
		return t.scanString(v)
	case []byte:
		return t.scanString(string(v))
	case int64:
		return t.scanUnix(v)
	case int:
		return t.scanUnix(int64(v))
	default:
		return fmt.Errorf("unsupported session timestamp type %T", value)
	}
}

func (t *nullableSessionTime) scanUnix(value int64) error {
	if value == 0 {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	if value > 1_000_000_000_000 {
		t.Time = time.UnixMilli(value)
	} else {
		t.Time = time.Unix(value, 0)
	}
	t.Valid = true
	return nil
}

func (t *nullableSessionTime) scanString(value string) error {
	if value == "" {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return t.scanUnix(unix)
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			t.Time = parsed
			t.Valid = !parsed.IsZero()
			return nil
		}
	}
	t.Valid = false
	t.Time = time.Time{}
	return nil
}

func getSessionBySessionIDFromTable(db *sql.DB, table, sessionID string) (*Session, error) {
	selectFrom := sessionSelectFromSessions
	if table == "sessions_archive" {
		selectFrom = sessionSelectFromArchive
	}
	sessions, err := querySessions(db, selectFrom+" WHERE session_id = ? ORDER BY last_active DESC LIMIT 1", sessionID)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}

func getSessionByZellijSessionFromTable(db *sql.DB, table, zellijSession string) (*Session, error) {
	selectFrom := sessionSelectFromSessions
	if table == "sessions_archive" {
		selectFrom = sessionSelectFromArchive
	}
	sessions, err := querySessions(db, selectFrom+" WHERE zellij_session = ? ORDER BY last_active DESC LIMIT 1", zellijSession)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, sql.ErrNoRows
	}
	return &sessions[0], nil
}
