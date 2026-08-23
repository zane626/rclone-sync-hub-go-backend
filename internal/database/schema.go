package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

const schemaInitializationLockName = "rclone_sync_hub_schema_initialization"

type schemaTable struct {
	name  string
	model interface{}
}

// currentSchemaTables is the complete schema required by this application
// version. Keep dependencies before tables that reference them.
func currentSchemaTables() []schemaTable {
	return []schemaTable{
		{name: "watch_folders", model: &model.WatchFolder{}},
		{name: "watch_folder_path_pipelines", model: &model.WatchFolderPathPipeline{}},
		{name: "file_records", model: &model.FileRecord{}},
		{name: "remote_routes", model: &model.RemoteRoute{}},
		{name: "remote_file_records", model: &model.RemoteFileRecord{}},
		{name: "upload_tasks", model: &model.UploadTask{}},
		{name: "upload_logs", model: &model.UploadLog{}},
		{name: "scan_runs", model: &model.ScanRun{}},
		{name: "audit_logs", model: &model.AuditLog{}},
	}
}

// CreateMissingTables bootstraps an empty database for the current application
// version. It intentionally never alters existing tables, tracks schema
// versions, or backfills legacy data. Existing installations therefore need to
// provide a schema that already matches the running application version.
func CreateMissingTables(ctx context.Context, db *gorm.DB, beforeCreate func(tableName string)) ([]string, error) {
	if db == nil {
		return nil, errors.New("database is nil")
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get schema initialization database: %w", err)
	}
	lockConn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open schema initialization lock connection: %w", err)
	}
	defer lockConn.Close()

	locked, err := acquireSchemaInitializationLock(ctx, lockConn)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("timed out waiting for database table initialization lock")
	}
	defer func() {
		_, _ = lockConn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", schemaInitializationLockName)
	}()

	tx := db.WithContext(ctx)
	created := make([]string, 0, len(currentSchemaTables()))
	for _, table := range currentSchemaTables() {
		if tx.Migrator().HasTable(table.model) {
			continue
		}
		if beforeCreate != nil {
			beforeCreate(table.name)
		}
		if err := tx.Migrator().CreateTable(table.model); err != nil {
			return created, fmt.Errorf("create table %s: %w", table.name, err)
		}
		created = append(created, table.name)
	}
	return created, nil
}

func acquireSchemaInitializationLock(ctx context.Context, conn *sql.Conn) (bool, error) {
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", schemaInitializationLockName, 60).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire database table initialization lock: %w", err)
	}
	return locked.Valid && locked.Int64 == 1, nil
}
