package store

import "database/sql"

// ManagedRepo is the persisted desired-state snapshot imported by `agentctl apply`.
type ManagedRepo struct {
	Name                      string
	Repository                string
	SourceRepoRef             string
	Role                      string
	Visibility                string
	RepoContractPath          string
	DefaultRoutingPolicyRef   string
	DefaultReviewPolicyRef    string
	DefaultApprovalPolicyRef  string
	DefaultBenchmarkPolicyRef string
	Notes                     string
	SourcePath                string
	SourceCommit              string
	SpecHash                  string
	RawSpecJSON               string
	CreatedAt                 string
	UpdatedAt                 string
}

// UpsertManagedRepo stores a ManagedRepo desired-state snapshot.
func UpsertManagedRepo(db *sql.DB, repo *ManagedRepo) error {
	_, err := db.Exec(`
		INSERT INTO managed_repos (
			name,
			repository,
			source_repo_ref,
			role,
			visibility,
			repo_contract_path,
			default_routing_policy_ref,
			default_review_policy_ref,
			default_approval_policy_ref,
			default_benchmark_policy_ref,
			notes,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET
			repository = excluded.repository,
			source_repo_ref = excluded.source_repo_ref,
			role = excluded.role,
			visibility = excluded.visibility,
			repo_contract_path = excluded.repo_contract_path,
			default_routing_policy_ref = excluded.default_routing_policy_ref,
			default_review_policy_ref = excluded.default_review_policy_ref,
			default_approval_policy_ref = excluded.default_approval_policy_ref,
			default_benchmark_policy_ref = excluded.default_benchmark_policy_ref,
			notes = excluded.notes,
			source_path = excluded.source_path,
			source_commit = excluded.source_commit,
			spec_hash = excluded.spec_hash,
			raw_spec_json = excluded.raw_spec_json,
			updated_at = CURRENT_TIMESTAMP
	`,
		repo.Name,
		repo.Repository,
		repo.SourceRepoRef,
		repo.Role,
		repo.Visibility,
		repo.RepoContractPath,
		repo.DefaultRoutingPolicyRef,
		repo.DefaultReviewPolicyRef,
		repo.DefaultApprovalPolicyRef,
		repo.DefaultBenchmarkPolicyRef,
		repo.Notes,
		repo.SourcePath,
		repo.SourceCommit,
		repo.SpecHash,
		repo.RawSpecJSON,
	)
	return err
}

// GetManagedRepo loads a ManagedRepo by name. Returns nil if not found.
func GetManagedRepo(db *sql.DB, name string) (*ManagedRepo, error) {
	var repo ManagedRepo
	err := db.QueryRow(`
		SELECT
			name,
			repository,
			source_repo_ref,
			role,
			visibility,
			repo_contract_path,
			default_routing_policy_ref,
			default_review_policy_ref,
			default_approval_policy_ref,
			default_benchmark_policy_ref,
			notes,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM managed_repos
		WHERE name = ?
	`, name).Scan(
		&repo.Name,
		&repo.Repository,
		&repo.SourceRepoRef,
		&repo.Role,
		&repo.Visibility,
		&repo.RepoContractPath,
		&repo.DefaultRoutingPolicyRef,
		&repo.DefaultReviewPolicyRef,
		&repo.DefaultApprovalPolicyRef,
		&repo.DefaultBenchmarkPolicyRef,
		&repo.Notes,
		&repo.SourcePath,
		&repo.SourceCommit,
		&repo.SpecHash,
		&repo.RawSpecJSON,
		&repo.CreatedAt,
		&repo.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetManagedRepoByRepository loads a ManagedRepo by repository slug. Returns nil if not found.
func GetManagedRepoByRepository(db *sql.DB, repository string) (*ManagedRepo, error) {
	var repo ManagedRepo
	err := db.QueryRow(`
		SELECT
			name,
			repository,
			source_repo_ref,
			role,
			visibility,
			repo_contract_path,
			default_routing_policy_ref,
			default_review_policy_ref,
			default_approval_policy_ref,
			default_benchmark_policy_ref,
			notes,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM managed_repos
		WHERE repository = ?
	`, repository).Scan(
		&repo.Name,
		&repo.Repository,
		&repo.SourceRepoRef,
		&repo.Role,
		&repo.Visibility,
		&repo.RepoContractPath,
		&repo.DefaultRoutingPolicyRef,
		&repo.DefaultReviewPolicyRef,
		&repo.DefaultApprovalPolicyRef,
		&repo.DefaultBenchmarkPolicyRef,
		&repo.Notes,
		&repo.SourcePath,
		&repo.SourceCommit,
		&repo.SpecHash,
		&repo.RawSpecJSON,
		&repo.CreatedAt,
		&repo.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// ListManagedRepos returns all persisted ManagedRepo desired-state snapshots.
func ListManagedRepos(db *sql.DB) ([]ManagedRepo, error) {
	rows, err := db.Query(`
		SELECT
			name,
			repository,
			source_repo_ref,
			role,
			visibility,
			repo_contract_path,
			default_routing_policy_ref,
			default_review_policy_ref,
			default_approval_policy_ref,
			default_benchmark_policy_ref,
			notes,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM managed_repos
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []ManagedRepo
	for rows.Next() {
		var repo ManagedRepo
		if err := rows.Scan(
			&repo.Name,
			&repo.Repository,
			&repo.SourceRepoRef,
			&repo.Role,
			&repo.Visibility,
			&repo.RepoContractPath,
			&repo.DefaultRoutingPolicyRef,
			&repo.DefaultReviewPolicyRef,
			&repo.DefaultApprovalPolicyRef,
			&repo.DefaultBenchmarkPolicyRef,
			&repo.Notes,
			&repo.SourcePath,
			&repo.SourceCommit,
			&repo.SpecHash,
			&repo.RawSpecJSON,
			&repo.CreatedAt,
			&repo.UpdatedAt,
		); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}
