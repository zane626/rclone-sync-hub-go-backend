package database

import (
	"context"
	"os"
	"testing"

	"rclone-sync-hub/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestCurrentSchemaTablesContainsOnlyBusinessTables(t *testing.T) {
	expected := []string{
		"watch_folders",
		"watch_folder_path_pipelines",
		"file_records",
		"remote_routes",
		"remote_file_records",
		"upload_tasks",
		"upload_logs",
		"scan_runs",
		"audit_logs",
	}
	tables := currentSchemaTables()
	if len(tables) != len(expected) {
		t.Fatalf("expected %d schema tables, got %d", len(expected), len(tables))
	}
	seen := make(map[string]struct{}, len(tables))
	for i, table := range tables {
		if table.name != expected[i] {
			t.Fatalf("table %d: expected %q, got %q", i, expected[i], table.name)
		}
		if table.model == nil {
			t.Fatalf("table %q has no model", table.name)
		}
		if _, duplicate := seen[table.name]; duplicate {
			t.Fatalf("duplicate table %q", table.name)
		}
		seen[table.name] = struct{}{}
	}
	if _, versioned := seen["schema_migrations"]; versioned {
		t.Fatal("versioned migration table must not be part of schema initialization")
	}
}

func TestCreateMissingTablesMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_SCHEMA_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_SCHEMA_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	dropCurrentSchema(t, db)
	t.Cleanup(func() { dropCurrentSchema(t, db) })

	expected := tableNames(currentSchemaTables())
	var announced []string
	created, err := CreateMissingTables(context.Background(), db, func(tableName string) {
		announced = append(announced, tableName)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTableNames(t, created, expected)
	assertTableNames(t, announced, expected)
	for _, table := range currentSchemaTables() {
		if !db.Migrator().HasTable(table.model) {
			t.Fatalf("table %q was not created", table.name)
		}
	}
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("schema_migrations must not be created")
	}

	created, err = CreateMissingTables(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Fatalf("second initialization unexpectedly created tables: %v", created)
	}

	dropCurrentSchema(t, db)
	if err := db.Exec(`CREATE TABLE watch_folders (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		PRIMARY KEY (id)
	) ENGINE=InnoDB`).Error; err != nil {
		t.Fatal(err)
	}
	created, err = CreateMissingTables(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTableNames(t, created, expected[1:])
	if db.Migrator().HasColumn(&model.WatchFolder{}, "Name") {
		t.Fatal("existing watch_folders table was altered")
	}
}

func dropCurrentSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	models := make([]interface{}, 0, len(currentSchemaTables()))
	for _, table := range currentSchemaTables() {
		models = append(models, table.model)
	}
	if err := db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS schema_migrations").Error; err != nil {
		t.Fatal(err)
	}
}

func tableNames(tables []schemaTable) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.name)
	}
	return names
}

func assertTableNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected tables %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected tables %v, got %v", want, got)
		}
	}
}
