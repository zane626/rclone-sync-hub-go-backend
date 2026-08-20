package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

const migrationLockName = "rclone_sync_hub_schema_migrations"

type Migration struct {
	Version int64
	Name    string
	Up      func(*gorm.DB) error
}

type schemaMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"size:255;not null"`
	Dirty     bool      `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

// DefaultMigrations is append-only. Never edit an applied migration; add a new version instead.
func DefaultMigrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "baseline production schema",
			Up: func(db *gorm.DB) error {
				return db.AutoMigrate(
					&model.UploadTask{},
					&model.FileRecord{},
					&model.UploadLog{},
					&model.WatchFolder{},
					&model.ScanRun{},
					&model.AuditLog{},
				)
			},
		},
		{
			Version: 2,
			Name:    "production query indexes",
			Up: func(db *gorm.DB) error {
				return ensureIndexes(db, []modelIndexes{
					{model: &model.UploadTask{}, names: []string{"idx_task_created_at", "idx_task_finished_at", "idx_task_status_created", "idx_task_status_finished", "idx_task_status_updated"}},
					{model: &model.WatchFolder{}, names: []string{"idx_watch_scan_due"}},
					{model: &model.ScanRun{}, names: []string{"idx_scan_started_at", "idx_scan_created_at"}},
				})
			},
		},
		{
			Version: 3,
			Name:    "reconcile orphaned watch folder work",
			Up: func(db *gorm.DB) error {
				return db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Exec(`UPDATE upload_tasks AS task
						LEFT JOIN watch_folders AS folder ON folder.id = task.watch_folder_id
						SET task.status = ?, task.error_message = ?, task.finished_at = UTC_TIMESTAMP(3),
							task.canceled_at = UTC_TIMESTAMP(3), task.last_status_at = UTC_TIMESTAMP(3),
							task.next_retry_at = NULL, task.cancel_requested_at = NULL
						WHERE task.watch_folder_id <> 0 AND folder.id IS NULL AND task.status IN ?`,
						model.TaskStatusCanceled, "watch folder no longer exists",
						[]string{model.TaskStatusPending, model.TaskStatusPaused, model.TaskStatusFailed}).Error; err != nil {
						return fmt.Errorf("cancel orphaned queued tasks: %w", err)
					}
					if err := tx.Exec(`UPDATE upload_tasks AS task
						LEFT JOIN watch_folders AS folder ON folder.id = task.watch_folder_id
						SET task.cancel_requested_at = COALESCE(task.cancel_requested_at, UTC_TIMESTAMP(3)),
							task.last_status_at = UTC_TIMESTAMP(3)
						WHERE task.watch_folder_id <> 0 AND folder.id IS NULL AND task.status = ?`,
						model.TaskStatusRunning).Error; err != nil {
						return fmt.Errorf("request cancellation for orphaned running tasks: %w", err)
					}
					if err := tx.Exec(`UPDATE file_records AS file
						LEFT JOIN watch_folders AS folder ON folder.id = file.watch_folder_id
						SET file.watch_folder_id = 0
						WHERE file.watch_folder_id <> 0 AND folder.id IS NULL`).Error; err != nil {
						return fmt.Errorf("detach orphaned file snapshots: %w", err)
					}
					return nil
				})
			},
		},
	}
}

type modelIndexes struct {
	model interface{}
	names []string
}

func ensureIndexes(db *gorm.DB, groups []modelIndexes) error {
	for _, group := range groups {
		for _, name := range group.names {
			if db.Migrator().HasIndex(group.model, name) {
				continue
			}
			if err := db.Migrator().CreateIndex(group.model, name); err != nil {
				return fmt.Errorf("create index %s: %w", name, err)
			}
		}
	}
	return nil
}

// RunMigrations serializes schema changes with a MySQL advisory lock and records dirty failures.
func RunMigrations(ctx context.Context, db *gorm.DB, migrations []Migration) error {
	if len(migrations) == 0 {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get migration sql db: %w", err)
	}
	lockConn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration lock connection: %w", err)
	}
	defer lockConn.Close()
	locked, err := acquireMigrationLock(ctx, lockConn)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("timed out waiting for database migration lock")
	}
	defer func() { _, _ = lockConn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", migrationLockName) }()

	if err := db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT NOT NULL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		dirty BOOLEAN NOT NULL DEFAULT TRUE,
		applied_at DATETIME(3) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var dirty schemaMigration
	if err := db.WithContext(ctx).Where("dirty = ?", true).First(&dirty).Error; err == nil {
		return fmt.Errorf("database migration %d (%s) is dirty; manual inspection is required", dirty.Version, dirty.Name)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check dirty migrations: %w", err)
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	seen := make(map[int64]struct{}, len(migrations))
	for _, migration := range migrations {
		if migration.Version <= 0 || migration.Name == "" || migration.Up == nil {
			return fmt.Errorf("invalid migration definition: version=%d name=%q", migration.Version, migration.Name)
		}
		if _, duplicate := seen[migration.Version]; duplicate {
			return fmt.Errorf("duplicate migration version %d", migration.Version)
		}
		seen[migration.Version] = struct{}{}

		var applied schemaMigration
		err := db.WithContext(ctx).First(&applied, "version = ?", migration.Version).Error
		if err == nil {
			if applied.Name != migration.Name {
				return fmt.Errorf("migration %d name changed from %q to %q", migration.Version, applied.Name, migration.Name)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read migration %d: %w", migration.Version, err)
		}

		record := schemaMigration{Version: migration.Version, Name: migration.Name, Dirty: true, AppliedAt: time.Now()}
		if err := db.WithContext(ctx).Create(&record).Error; err != nil {
			return fmt.Errorf("mark migration %d dirty: %w", migration.Version, err)
		}
		if err := migration.Up(db.WithContext(ctx)); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.Version, migration.Name, err)
		}
		if err := db.WithContext(ctx).Model(&schemaMigration{}).Where("version = ?", migration.Version).
			Updates(map[string]interface{}{"dirty": false, "applied_at": time.Now()}).Error; err != nil {
			return fmt.Errorf("mark migration %d complete: %w", migration.Version, err)
		}
	}
	return nil
}

func acquireMigrationLock(ctx context.Context, conn *sql.Conn) (bool, error) {
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", migrationLockName, 60).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire migration lock: %w", err)
	}
	return locked.Valid && locked.Int64 == 1, nil
}
