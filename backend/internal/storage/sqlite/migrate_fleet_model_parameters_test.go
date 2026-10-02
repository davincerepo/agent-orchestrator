package sqlite

import (
	"fmt"
	"testing"
)

func TestFleetModelParametersUpgradeFromOldVersions(t *testing.T) {
	for _, version := range []int64{139, 147} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db := openMigratedDatabaseCopy(t, version)
			if _, err := db.Exec(`
ALTER TABLE conversations ADD COLUMN service_tier TEXT;
ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';

INSERT INTO projects (id, path, registered_at) VALUES ('fleet', '/fleet', CURRENT_TIMESTAMP);
INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at, reasoning_effort, service_tier)
VALUES ('fleet-1', 'fleet', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'high', 'priority');
`); err != nil {
				t.Fatal(err)
			}
			legacyVersion := int64(148)
			if version == 139 {
				legacyVersion = 140
			}
			if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, legacyVersion); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := migrate(db); err != nil {
					t.Fatal(err)
				}
			}
			var effort, tier string
			if err := db.QueryRow(`SELECT effort, service_tier FROM sessions WHERE id = 'fleet-1'`).Scan(&effort, &tier); err != nil {
				t.Fatal(err)
			}
			if effort != "high" || tier != "priority" {
				t.Fatalf("saved parameters changed: %q %q", effort, tier)
			}
			var required int
			if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info('sessions') WHERE name = 'project_id'`).Scan(&required); err != nil || required != 0 {
				t.Fatalf("upstream standalone migration was skipped: required=%d err=%v", required, err)
			}
			var legacy, dismissed int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'reasoning_effort'`).Scan(&legacy); err != nil || legacy != 0 {
				t.Fatalf("legacy session effort remains: %d %v", legacy, err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('notifications') WHERE name = 'dismissed_at'`).Scan(&dismissed); err != nil || dismissed != 1 {
				t.Fatalf("upstream notification migration missing: %d %v", dismissed, err)
			}
		})
	}
}

func TestFleetModelParametersPreserveUpstreamEffort(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 170)
	if _, err := db.Exec(`
 ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';
 INSERT INTO projects (id, path, registered_at) VALUES ('fleet', '/fleet', CURRENT_TIMESTAMP);
 INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at, effort, reasoning_effort)
 VALUES ('fleet-1', 'fleet', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'low', 'high');
 `); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var effort string
	if err := db.QueryRow(`SELECT effort FROM sessions WHERE id = 'fleet-1'`).Scan(&effort); err != nil || effort != "low" {
		t.Fatalf("upstream choice changed: %q %v", effort, err)
	}
}
