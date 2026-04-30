package store

import "database/sql"

// ControlPlaneResource is a persisted desired-state snapshot for non-ManagedRepo control-plane resources.
type ControlPlaneResource struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	APIVersion   string `json:"apiVersion,omitempty"`
	SourcePath   string `json:"sourcePath,omitempty"`
	SourceCommit string `json:"sourceCommit,omitempty"`
	SpecHash     string `json:"specHash,omitempty"`
	RawSpecJSON  string `json:"rawSpecJSON,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

func UpsertControlPlaneResource(db *sql.DB, resource *ControlPlaneResource) error {
	_, err := db.Exec(`
		INSERT INTO control_plane_resources (
			kind,
			name,
			api_version,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(kind, name) DO UPDATE SET
			api_version = excluded.api_version,
			source_path = excluded.source_path,
			source_commit = excluded.source_commit,
			spec_hash = excluded.spec_hash,
			raw_spec_json = excluded.raw_spec_json,
			updated_at = CURRENT_TIMESTAMP
	`,
		resource.Kind,
		resource.Name,
		resource.APIVersion,
		resource.SourcePath,
		resource.SourceCommit,
		resource.SpecHash,
		resource.RawSpecJSON,
	)
	return err
}

func GetControlPlaneResource(db *sql.DB, kind, name string) (*ControlPlaneResource, error) {
	var resource ControlPlaneResource
	err := db.QueryRow(`
		SELECT
			kind,
			name,
			api_version,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM control_plane_resources
		WHERE kind = ? AND name = ?
	`, kind, name).Scan(
		&resource.Kind,
		&resource.Name,
		&resource.APIVersion,
		&resource.SourcePath,
		&resource.SourceCommit,
		&resource.SpecHash,
		&resource.RawSpecJSON,
		&resource.CreatedAt,
		&resource.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

func ListControlPlaneResources(db *sql.DB, kind string) ([]ControlPlaneResource, error) {
	query := `
		SELECT
			kind,
			name,
			api_version,
			source_path,
			source_commit,
			spec_hash,
			raw_spec_json,
			created_at,
			updated_at
		FROM control_plane_resources
	`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY kind, name`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []ControlPlaneResource
	for rows.Next() {
		var resource ControlPlaneResource
		if err := rows.Scan(
			&resource.Kind,
			&resource.Name,
			&resource.APIVersion,
			&resource.SourcePath,
			&resource.SourceCommit,
			&resource.SpecHash,
			&resource.RawSpecJSON,
			&resource.CreatedAt,
			&resource.UpdatedAt,
		); err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func CountControlPlaneResources(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM control_plane_resources`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
