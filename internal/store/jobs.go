package store

import (
	"database/sql"
	"time"
)

type Job struct {
	ID          int64
	Name        string
	Schedule    string
	Action      string
	Repo        string
	Session     string
	Branch      string
	Agent       string
	Instruction string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type JobRun struct {
	ID         int64
	JobID      int64
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string
	Output     string
}

type JobLock struct {
	JobID    int64
	LockedAt time.Time
	LockedBy string
}

func CreateJob(db *sql.DB, job *Job) error {
	enabled := 0
	if job.Enabled {
		enabled = 1
	}
	res, err := db.Exec(`INSERT INTO jobs (name, schedule, action, repo, session, branch, agent, instruction, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Name, job.Schedule, job.Action, job.Repo, job.Session, job.Branch, job.Agent, job.Instruction, enabled)
	if err != nil {
		return err
	}
	job.ID, err = res.LastInsertId()
	return err
}

func ListJobs(db *sql.DB) ([]Job, error) {
	return queryJobs(db, `SELECT id, name, schedule, action, repo, session, branch, agent, instruction, enabled, created_at, updated_at
		FROM jobs ORDER BY id ASC`)
}

func GetJobByID(db *sql.DB, id int64) (*Job, error) {
	jobs, err := queryJobs(db, `SELECT id, name, schedule, action, repo, session, branch, agent, instruction, enabled, created_at, updated_at
		FROM jobs WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, sql.ErrNoRows
	}
	return &jobs[0], nil
}

func GetJobByName(db *sql.DB, name string) (*Job, error) {
	jobs, err := queryJobs(db, `SELECT id, name, schedule, action, repo, session, branch, agent, instruction, enabled, created_at, updated_at
		FROM jobs WHERE name = ?`, name)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, sql.ErrNoRows
	}
	return &jobs[0], nil
}

func DeleteJob(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

func CreateJobRun(db *sql.DB, run *JobRun) error {
	res, err := db.Exec(`INSERT INTO job_runs (job_id, started_at, finished_at, status, output)
		VALUES (?, ?, ?, ?, ?)`,
		run.JobID, run.StartedAt, run.FinishedAt, run.Status, run.Output)
	if err != nil {
		return err
	}
	run.ID, err = res.LastInsertId()
	return err
}

func UpdateJobRun(db *sql.DB, run *JobRun) error {
	_, err := db.Exec(`UPDATE job_runs SET finished_at = ?, status = ?, output = ? WHERE id = ?`,
		run.FinishedAt, run.Status, run.Output, run.ID)
	return err
}

func ListJobRuns(db *sql.DB, jobID int64, limit int) ([]JobRun, error) {
	return queryJobRuns(db, `SELECT id, job_id, started_at, finished_at, status, output
		FROM job_runs WHERE job_id = ? ORDER BY started_at DESC LIMIT ?`, jobID, limit)
}

func GetLatestJobRun(db *sql.DB, jobID int64) (*JobRun, error) {
	runs, err := queryJobRuns(db, `SELECT id, job_id, started_at, finished_at, status, output
		FROM job_runs WHERE job_id = ? ORDER BY started_at DESC LIMIT 1`, jobID)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, sql.ErrNoRows
	}
	return &runs[0], nil
}

func AcquireJobLock(db *sql.DB, jobID int64, lockedBy string) error {
	_, err := db.Exec(`INSERT INTO job_locks (job_id, locked_by) VALUES (?, ?)`, jobID, lockedBy)
	return err
}

func ReleaseJobLock(db *sql.DB, jobID int64) error {
	_, err := db.Exec(`DELETE FROM job_locks WHERE job_id = ?`, jobID)
	return err
}

func GetJobLock(db *sql.DB, jobID int64) (*JobLock, error) {
	var lock JobLock
	err := db.QueryRow(`SELECT job_id, locked_at, locked_by FROM job_locks WHERE job_id = ?`, jobID).
		Scan(&lock.JobID, &lock.LockedAt, &lock.LockedBy)
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

func queryJobs(db *sql.DB, query string, args ...any) ([]Job, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		var enabled int
		if err := rows.Scan(&job.ID, &job.Name, &job.Schedule, &job.Action, &job.Repo, &job.Session,
			&job.Branch, &job.Agent, &job.Instruction, &enabled, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		job.Enabled = enabled != 0
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func queryJobRuns(db *sql.DB, query string, args ...any) ([]JobRun, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []JobRun
	for rows.Next() {
		var run JobRun
		var finishedAt sql.NullTime
		if err := rows.Scan(&run.ID, &run.JobID, &run.StartedAt, &finishedAt, &run.Status, &run.Output); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			run.FinishedAt = &finishedAt.Time
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
