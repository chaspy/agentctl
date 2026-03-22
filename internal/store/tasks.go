package store

import (
	"database/sql"
	"time"
)

// Task represents a task assigned to a session.
type Task struct {
	ID          int64
	SessionID   string
	Description string
	Status      string // pending, in_progress, completed, cancelled
	AssignedAt  time.Time
	CompletedAt *time.Time
	Result      string
	PRURL       string
}

// CreateTask inserts a new task.
func CreateTask(db *sql.DB, t *Task) error {
	res, err := db.Exec(`INSERT INTO tasks (session_id, description, status, result, pr_url)
		VALUES (?, ?, ?, ?, ?)`,
		t.SessionID, t.Description, t.Status, t.Result, t.PRURL)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

// UpdateTaskStatus updates a task's status and optionally its result.
func UpdateTaskStatus(db *sql.DB, id int64, status string, result string) error {
	if status == "completed" || status == "cancelled" {
		_, err := db.Exec(`UPDATE tasks SET status = ?, result = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?`,
			status, result, id)
		return err
	}
	_, err := db.Exec(`UPDATE tasks SET status = ?, result = ? WHERE id = ?`, status, result, id)
	return err
}

// GetTasksForSession returns all tasks for a session.
func GetTasksForSession(db *sql.DB, sessionID string) ([]Task, error) {
	return queryTasks(db, `SELECT id, session_id, description, status, assigned_at, completed_at, result, pr_url
		FROM tasks WHERE session_id = ? ORDER BY assigned_at DESC`, sessionID)
}

// GetActiveTasks returns all non-completed/non-cancelled tasks.
func GetActiveTasks(db *sql.DB) ([]Task, error) {
	return queryTasks(db, `SELECT id, session_id, description, status, assigned_at, completed_at, result, pr_url
		FROM tasks WHERE status IN ('pending', 'in_progress') ORDER BY assigned_at DESC`)
}

func queryTasks(db *sql.DB, query string, args ...any) ([]Task, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var completedAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Description, &t.Status,
			&t.AssignedAt, &completedAt, &t.Result, &t.PRURL); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
