package store

import (
	"database/sql"
	"time"
)

// Action represents a logged action by the AI Manager.
type Action struct {
	ID         int64
	SessionID  string
	ActionType string // send, receive, spawn, kill, note, decision
	Content    string
	Result     string
	CreatedAt  time.Time
}

// LogAction inserts an action log entry.
func LogAction(db *sql.DB, a *Action) error {
	res, err := db.Exec(`INSERT INTO actions (session_id, action_type, content, result)
		VALUES (?, ?, ?, ?)`,
		a.SessionID, a.ActionType, a.Content, a.Result)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

// GetRecentActions returns the most recent actions.
func GetRecentActions(db *sql.DB, limit int) ([]Action, error) {
	return queryActions(db, `SELECT id, session_id, action_type, content, result, created_at
		FROM actions ORDER BY created_at DESC LIMIT ?`, limit)
}

// GetActionsForSession returns recent actions for a specific session.
func GetActionsForSession(db *sql.DB, sessionID string, limit int) ([]Action, error) {
	return queryActions(db, `SELECT id, session_id, action_type, content, result, created_at
		FROM actions WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
}

// GetActionsSince returns actions since the given time.
func GetActionsSince(db *sql.DB, since time.Time) ([]Action, error) {
	return queryActions(db, `SELECT id, session_id, action_type, content, result, created_at
		FROM actions WHERE created_at >= ? ORDER BY created_at DESC`, since)
}

func queryActions(db *sql.DB, query string, args ...any) ([]Action, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ActionType, &a.Content, &a.Result, &a.CreatedAt); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}
