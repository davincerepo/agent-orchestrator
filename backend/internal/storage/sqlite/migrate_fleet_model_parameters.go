package sqlite

import (
	"database/sql"
	"fmt"
)

// The previous Fleet build used 0140 for these columns. Upstream now owns 0140
// for standalone sessions. Recognize the physical schema before moving its
// ledger entry so neither existing settings nor the upstream migration is lost.
func prepareFleetModelParametersMigration(db *sql.DB) error {
	var columns int
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name IN ('reasoning_effort', 'service_tier')) +
		(SELECT COUNT(*) FROM pragma_table_info('conversations') WHERE name = 'service_tier')`).Scan(&columns); err != nil {
		return err
	}
	if columns == 0 {
		return nil
	}
	if columns != 3 {
		return fmt.Errorf("incomplete Fleet model parameter schema: found %d of 3 columns", columns)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var applied int
	if err := tx.QueryRow(`SELECT COALESCE((SELECT is_applied FROM goose_db_version
		WHERE version_id = 148 ORDER BY id DESC LIMIT 1), 0)`).Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		if _, err := tx.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (148, 1)`); err != nil {
			return err
		}
	}
	var projectRequired int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions')
		WHERE name = 'project_id' AND "notnull" = 1`).Scan(&projectRequired); err != nil {
		return err
	}
	if projectRequired != 0 {
		if _, err := tx.Exec(`DELETE FROM goose_db_version WHERE version_id = 140`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
