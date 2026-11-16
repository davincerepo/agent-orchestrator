package sqlite

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

// Fleet extensions never consume upstream goose version numbers.
// sqlc consumes these same schema additions.
//
//go:embed fleet_schema.sql
var fleetModelSchema string

// Release old Fleet versions only when the upstream schema is absent.
func prepareFleetModelParametersMigration(db *sql.DB) error {
	var legacy int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'reasoning_effort'`).Scan(&legacy); err != nil || legacy == 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var dismissed, projectRequired int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('notifications') WHERE name = 'dismissed_at'`).Scan(&dismissed); err != nil {
		return err
	}
	if dismissed == 0 {
		if _, err := tx.Exec(`DELETE FROM goose_db_version WHERE version_id = 148`); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'project_id' AND "notnull" = 1`).Scan(&projectRequired); err != nil {
		return err
	}
	if projectRequired != 0 {
		if _, err := tx.Exec(`DELETE FROM goose_db_version WHERE version_id = 140`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Upgrade legacy session effort once, then remove its column. The copy and
// removal are atomic; an interrupted upgrade can safely be retried.
func migrateFleetModelParameters(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statements := strings.Split(strings.TrimSpace(fleetModelSchema), ";")
	for i, table := range []string{"sessions", "conversations"} {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'service_tier'`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec(statements[i]); err != nil {
				return fmt.Errorf("add %s service tier: %w", table, err)
			}
		}
	}
	var legacy int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'reasoning_effort'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy != 0 {
		if _, err := tx.Exec(`UPDATE sessions SET effort = reasoning_effort WHERE effort = '' AND reasoning_effort <> ''`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE sessions DROP COLUMN reasoning_effort`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
