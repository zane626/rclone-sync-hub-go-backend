package service

import (
	"context"
	"fmt"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type MaintenanceConfig struct {
	Interval           time.Duration
	UploadLogRetention time.Duration
	TaskRetention      time.Duration
	ScanRunRetention   time.Duration
	AuditLogRetention  time.Duration
	DeleteBatchSize    int
}

type MaintenanceService struct {
	db  *gorm.DB
	cfg MaintenanceConfig
}

func NewMaintenanceService(db *gorm.DB, cfg MaintenanceConfig) *MaintenanceService {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.DeleteBatchSize <= 0 {
		cfg.DeleteBatchSize = 5000
	}
	return &MaintenanceService{db: db, cfg: cfg}
}

func (s *MaintenanceService) Run(ctx context.Context) {
	if err := s.Cleanup(ctx, time.Now()); err != nil && ctx.Err() == nil {
		logger.L.Warn("maintenance cleanup failed", zap.Error(err))
	}
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.Cleanup(ctx, now); err != nil && ctx.Err() == nil {
				logger.L.Warn("maintenance cleanup failed", zap.Error(err))
			}
		}
	}
}

func (s *MaintenanceService) Cleanup(ctx context.Context, now time.Time) error {
	tables := []struct {
		name      string
		retention time.Duration
	}{
		{name: "upload_logs", retention: s.cfg.UploadLogRetention},
		{name: "scan_runs", retention: s.cfg.ScanRunRetention},
		{name: "audit_logs", retention: s.cfg.AuditLogRetention},
	}
	for _, table := range tables {
		if table.retention <= 0 {
			continue
		}
		cutoff := now.Add(-table.retention)
		for {
			result := s.db.WithContext(ctx).Exec(fmt.Sprintf("DELETE FROM %s WHERE created_at < ? LIMIT ?", table.name), cutoff, s.cfg.DeleteBatchSize)
			if result.Error != nil {
				return fmt.Errorf("cleanup %s: %w", table.name, result.Error)
			}
			if result.RowsAffected < int64(s.cfg.DeleteBatchSize) {
				break
			}
		}
	}
	if s.cfg.TaskRetention > 0 {
		cutoff := now.Add(-s.cfg.TaskRetention)
		for {
			result := s.db.WithContext(ctx).Exec(
				"DELETE FROM upload_tasks WHERE status IN ? AND finished_at IS NOT NULL AND finished_at < ? LIMIT ?",
				[]string{model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusCanceled}, cutoff, s.cfg.DeleteBatchSize,
			)
			if result.Error != nil {
				return fmt.Errorf("cleanup upload_tasks: %w", result.Error)
			}
			if result.RowsAffected < int64(s.cfg.DeleteBatchSize) {
				break
			}
		}
	}
	return nil
}
