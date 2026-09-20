package sqlite

import "testing"

func TestFleetModelParametersUpgradeFromOldVersion140(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 139)
	if _, err := db.Exec(`
ALTER TABLE conversations ADD COLUMN service_tier TEXT;
ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';
INSERT INTO goose_db_version (version_id, is_applied) VALUES (140, 1);
INSERT INTO projects (id, path, registered_at) VALUES ('fleet', '/fleet', CURRENT_TIMESTAMP);
INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at, reasoning_effort, service_tier)
VALUES ('fleet-1', 'fleet', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'high', 'priority');
`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var effort, tier string
	if err := db.QueryRow(`SELECT reasoning_effort, service_tier FROM sessions WHERE id = 'fleet-1'`).Scan(&effort, &tier); err != nil {
		t.Fatal(err)
	}
	if effort != "high" || tier != "priority" {
		t.Fatalf("saved parameters changed: %q %q", effort, tier)
	}
	var required int
	if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info('sessions') WHERE name = 'project_id'`).Scan(&required); err != nil || required != 0 {
		t.Fatalf("upstream standalone migration was skipped: required=%d err=%v", required, err)
	}
}
