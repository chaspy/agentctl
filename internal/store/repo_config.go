package store

import "database/sql"

// RepoConfig represents a repository operation mode configuration.
type RepoConfig struct {
	Repo        string
	Mode        string // "main" or "branch"
	Description string
	CreatedAt   string
	UpdatedAt   string
}

// SetRepoConfig upserts a repository configuration (mode only).
func SetRepoConfig(db *sql.DB, repo, mode string) error {
	_, err := db.Exec(`INSERT INTO repo_config (repo, mode, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(repo) DO UPDATE SET mode=excluded.mode, updated_at=CURRENT_TIMESTAMP`,
		repo, mode)
	return err
}

// SetRepoDescription upserts a repository description.
// If the repo doesn't exist yet, it creates a row with mode="branch" (default).
func SetRepoDescription(db *sql.DB, repo, description string) error {
	_, err := db.Exec(`INSERT INTO repo_config (repo, mode, description, created_at, updated_at)
		VALUES (?, 'branch', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(repo) DO UPDATE SET description=excluded.description, updated_at=CURRENT_TIMESTAMP`,
		repo, description)
	return err
}

// GetRepoConfig retrieves the mode for a repository. Returns empty string if not found.
func GetRepoConfig(db *sql.DB, repo string) (string, error) {
	var mode string
	err := db.QueryRow("SELECT mode FROM repo_config WHERE repo = ?", repo).Scan(&mode)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return mode, err
}

// GetRepoDescription retrieves the description for a repository. Returns empty string if not found.
func GetRepoDescription(db *sql.DB, repo string) (string, error) {
	var desc string
	err := db.QueryRow("SELECT description FROM repo_config WHERE repo = ?", repo).Scan(&desc)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return desc, err
}

// GetRepoFullConfig retrieves the full configuration for a repository. Returns nil if not found.
func GetRepoFullConfig(db *sql.DB, repo string) (*RepoConfig, error) {
	var c RepoConfig
	err := db.QueryRow("SELECT repo, mode, description, created_at, updated_at FROM repo_config WHERE repo = ?", repo).
		Scan(&c.Repo, &c.Mode, &c.Description, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListRepoConfigs returns all repository configurations.
func ListRepoConfigs(db *sql.DB) ([]RepoConfig, error) {
	rows, err := db.Query("SELECT repo, mode, description, created_at, updated_at FROM repo_config ORDER BY repo")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []RepoConfig
	for rows.Next() {
		var c RepoConfig
		if err := rows.Scan(&c.Repo, &c.Mode, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// DeleteRepoConfig removes a repository configuration.
func DeleteRepoConfig(db *sql.DB, repo string) error {
	_, err := db.Exec("DELETE FROM repo_config WHERE repo = ?", repo)
	return err
}
