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
	Owner       string
	AssignedAt  time.Time
	CompletedAt *time.Time
	Result      string
	PRURL       string
	Blocks      []int64 // task IDs that this task blocks (populated by GetTaskWithDeps)
	BlockedBy   []int64 // task IDs that block this task (populated by GetTaskWithDeps)
}

// CreateTask inserts a new task.
func CreateTask(db *sql.DB, t *Task) error {
	res, err := db.Exec(`INSERT INTO tasks (session_id, description, status, result, pr_url, owner)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.SessionID, t.Description, t.Status, t.Result, t.PRURL, t.Owner)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

// UpdateTaskOwner sets the owner of a task.
func UpdateTaskOwner(db *sql.DB, id int64, owner string) error {
	_, err := db.Exec(`UPDATE tasks SET owner = ? WHERE id = ?`, owner, id)
	return err
}

// AddTaskDependency records that taskID is blocked by dependsOn.
func AddTaskDependency(db *sql.DB, taskID, dependsOn int64) error {
	_, err := db.Exec(`INSERT OR IGNORE INTO task_dependencies (task_id, depends_on) VALUES (?, ?)`,
		taskID, dependsOn)
	return err
}

// RemoveTaskDependency removes a dependency.
func RemoveTaskDependency(db *sql.DB, taskID, dependsOn int64) error {
	_, err := db.Exec(`DELETE FROM task_dependencies WHERE task_id = ? AND depends_on = ?`,
		taskID, dependsOn)
	return err
}

// GetBlockedBy returns the IDs of tasks that block the given task.
func GetBlockedBy(db *sql.DB, taskID int64) ([]int64, error) {
	rows, err := db.Query(`SELECT depends_on FROM task_dependencies WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetBlocks returns the IDs of tasks that the given task blocks.
func GetBlocks(db *sql.DB, taskID int64) ([]int64, error) {
	rows, err := db.Query(`SELECT task_id FROM task_dependencies WHERE depends_on = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// IsTaskBlocked returns true if the task has any incomplete blockers.
func IsTaskBlocked(db *sql.DB, taskID int64) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM task_dependencies td
		JOIN tasks t ON t.id = td.depends_on
		WHERE td.task_id = ? AND t.status NOT IN ('completed', 'cancelled')`, taskID).Scan(&count)
	return count > 0, err
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
	return queryTasks(db, `SELECT id, session_id, description, status, assigned_at, completed_at, result, pr_url, owner
		FROM tasks WHERE session_id = ? ORDER BY assigned_at DESC`, sessionID)
}

// GetActiveTasks returns all non-completed/non-cancelled tasks.
func GetActiveTasks(db *sql.DB) ([]Task, error) {
	return queryTasks(db, `SELECT id, session_id, description, status, assigned_at, completed_at, result, pr_url, owner
		FROM tasks WHERE status IN ('pending', 'in_progress') ORDER BY assigned_at DESC`)
}

// GetTaskByID returns a single task by ID.
func GetTaskByID(db *sql.DB, id int64) (*Task, error) {
	tasks, err := queryTasks(db, `SELECT id, session_id, description, status, assigned_at, completed_at, result, pr_url, owner
		FROM tasks WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, sql.ErrNoRows
	}
	return &tasks[0], nil
}

// GetReadyTasks returns active tasks that have no incomplete blockers.
func GetReadyTasks(db *sql.DB) ([]Task, error) {
	return queryTasks(db, `SELECT t.id, t.session_id, t.description, t.status, t.assigned_at, t.completed_at, t.result, t.pr_url, t.owner
		FROM tasks t
		WHERE t.status IN ('pending', 'in_progress')
		AND NOT EXISTS (
			SELECT 1 FROM task_dependencies td
			JOIN tasks blocker ON blocker.id = td.depends_on
			WHERE td.task_id = t.id AND blocker.status NOT IN ('completed', 'cancelled')
		)
		ORDER BY t.assigned_at DESC`)
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
			&t.AssignedAt, &completedAt, &t.Result, &t.PRURL, &t.Owner); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
