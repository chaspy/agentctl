package store

import "database/sql"

// SetState upserts a key-value pair in manager_state.
func SetState(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO manager_state (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		key, value)
	return err
}

// GetState retrieves a value by key. Returns empty string if not found.
func GetState(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM manager_state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// DeleteState removes a key from manager_state.
func DeleteState(db *sql.DB, key string) error {
	_, err := db.Exec("DELETE FROM manager_state WHERE key = ?", key)
	return err
}

// AllState returns all key-value pairs.
func AllState(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query("SELECT key, value FROM manager_state ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}
