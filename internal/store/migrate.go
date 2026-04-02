package store

import "database/sql"

var migrations = []string{
	migrationV1,
	migrationV2,
	migrationV3,
	migrationV4,
	migrationV5,
	migrationV6,
	migrationV7,
	migrationV8,
	migrationV9,
	migrationV10,
	migrationV11,
}

// Migrate applies all pending schema migrations.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return err
	}

	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return err
	}

	for i, m := range migrations {
		ver := i + 1
		if ver <= current {
			continue
		}
		if _, err := db.Exec(m); err != nil {
			return err
		}
		if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (?)", ver); err != nil {
			return err
		}
	}
	return nil
}

const migrationV1 = `
CREATE TABLE IF NOT EXISTS sessions (
	id             TEXT PRIMARY KEY,
	agent          TEXT NOT NULL,
	project_name   TEXT NOT NULL,
	session_id     TEXT NOT NULL,
	cwd            TEXT NOT NULL DEFAULT '',
	git_branch     TEXT NOT NULL DEFAULT '',
	zellij_session TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL DEFAULT 'unknown',
	alive          INTEGER NOT NULL DEFAULT 0,
	last_message   TEXT NOT NULL DEFAULT '',
	last_role      TEXT NOT NULL DEFAULT '',
	last_active    TIMESTAMP,
	pr_number      INTEGER,
	pr_url         TEXT NOT NULL DEFAULT '',
	pr_state       TEXT NOT NULL DEFAULT '',
	task_summary   TEXT NOT NULL DEFAULT '',
	created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);

CREATE TABLE IF NOT EXISTS tasks (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id   TEXT NOT NULL,
	description  TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT 'pending',
	assigned_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	completed_at TIMESTAMP,
	result       TEXT NOT NULL DEFAULT '',
	pr_url       TEXT NOT NULL DEFAULT '',
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

CREATE TABLE IF NOT EXISTS actions (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id  TEXT NOT NULL DEFAULT '',
	action_type TEXT NOT NULL,
	content     TEXT NOT NULL,
	result      TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_actions_session ON actions(session_id);
CREATE INDEX IF NOT EXISTS idx_actions_type ON actions(action_type);
CREATE INDEX IF NOT EXISTS idx_actions_created ON actions(created_at);

CREATE TABLE IF NOT EXISTS manager_state (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const migrationV2 = `
ALTER TABLE sessions ADD COLUMN role TEXT NOT NULL DEFAULT 'worker';
`

const migrationV3 = `
ALTER TABLE sessions ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
`

const migrationV4 = `
CREATE TABLE IF NOT EXISTS repo_config (
	repo       TEXT PRIMARY KEY,
	mode       TEXT NOT NULL DEFAULT 'branch',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const migrationV5 = `
ALTER TABLE sessions RENAME COLUMN project_name TO repository;
DROP INDEX IF EXISTS idx_sessions_project;
CREATE INDEX IF NOT EXISTS idx_sessions_repository ON sessions(repository);
`

const migrationV6 = `
ALTER TABLE repo_config ADD COLUMN description TEXT NOT NULL DEFAULT '';
`

const migrationV7 = `
ALTER TABLE sessions ADD COLUMN blocked_reason TEXT NOT NULL DEFAULT '';
`

const migrationV8 = `
ALTER TABLE sessions ADD COLUMN is_loop INTEGER NOT NULL DEFAULT 0;
`

const migrationV9 = `
CREATE TABLE IF NOT EXISTS sessions_archive (
	id             TEXT PRIMARY KEY,
	agent          TEXT NOT NULL,
	repository     TEXT NOT NULL,
	session_id     TEXT NOT NULL,
	cwd            TEXT NOT NULL DEFAULT '',
	git_branch     TEXT NOT NULL DEFAULT '',
	zellij_session TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL DEFAULT 'unknown',
	blocked_reason TEXT NOT NULL DEFAULT '',
	alive          INTEGER NOT NULL DEFAULT 0,
	last_message   TEXT NOT NULL DEFAULT '',
	last_role      TEXT NOT NULL DEFAULT '',
	last_active    TIMESTAMP,
	pr_number      INTEGER,
	pr_url         TEXT NOT NULL DEFAULT '',
	pr_state       TEXT NOT NULL DEFAULT '',
	task_summary   TEXT NOT NULL DEFAULT '',
	role           TEXT NOT NULL DEFAULT 'worker',
	archived       INTEGER NOT NULL DEFAULT 0,
	is_loop        INTEGER NOT NULL DEFAULT 0,
	created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	archived_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_archive_repository ON sessions_archive(repository);
CREATE INDEX IF NOT EXISTS idx_sessions_archive_status ON sessions_archive(status);
CREATE INDEX IF NOT EXISTS idx_sessions_archive_archived_at ON sessions_archive(archived_at);

-- Migrate existing dead/error sessions to archive
INSERT INTO sessions_archive (id, agent, repository, session_id, cwd, git_branch,
	zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
	pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, archived_at)
SELECT id, agent, repository, session_id, cwd, git_branch,
	zellij_session, status, blocked_reason, alive, last_message, last_role, last_active,
	pr_number, pr_url, pr_state, task_summary, role, archived, is_loop, created_at, updated_at, CURRENT_TIMESTAMP
FROM sessions
WHERE alive = 0 AND status IN ('dead', 'error');

DELETE FROM sessions WHERE alive = 0 AND status IN ('dead', 'error');
`

const migrationV10 = `
ALTER TABLE tasks ADD COLUMN owner TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS task_dependencies (
	task_id    INTEGER NOT NULL,
	depends_on INTEGER NOT NULL,
	PRIMARY KEY (task_id, depends_on),
	FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
	FOREIGN KEY (depends_on) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_deps_task ON task_dependencies(task_id);
CREATE INDEX IF NOT EXISTS idx_task_deps_depends ON task_dependencies(depends_on);

ALTER TABLE sessions ADD COLUMN permission_level INTEGER NOT NULL DEFAULT 1;

ALTER TABLE sessions_archive ADD COLUMN permission_level INTEGER NOT NULL DEFAULT 1;
`

const migrationV11 = `
ALTER TABLE sessions ADD COLUMN runtime_status TEXT NOT NULL DEFAULT 'gone';
ALTER TABLE sessions_archive ADD COLUMN runtime_status TEXT NOT NULL DEFAULT 'gone';
`
