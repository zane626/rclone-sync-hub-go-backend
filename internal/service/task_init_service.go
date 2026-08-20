package service

import (
	"context"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TaskInitService 负责扫描状态的启动修复；上传 running 任务由持久化租约恢复，
// 不能在多实例启动时被某个进程批量重置。
type TaskInitService struct {
	db *gorm.DB
}

// NewTaskInitService 创建 TaskInitService。
func NewTaskInitService(db *gorm.DB) *TaskInitService {
	return &TaskInitService{db: db}
}

// FixStatusesOnStartup 在系统启动时修复异常状态：
// 1. 将异常退出遗留的 detecting 扫描标记为 error，并安排立即重试。
// 2. 终结未完成的 scan_runs。upload_tasks 使用租约恢复，不能在多实例启动时批量重置 running。
func (s *TaskInitService) FixStatusesOnStartup(ctx context.Context) error {
	now := time.Now()
	// 1. watch_folders
	if err := s.db.WithContext(ctx).
		Model(&model.WatchFolder{}).
		Where("status = ? AND (scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?)", model.WatchFolderStatusDetecting, now).
		Updates(map[string]interface{}{
			"status":                model.WatchFolderStatusError,
			"last_error":            "previous scan was interrupted by process shutdown",
			"last_scan_finished_at": now,
			"next_scan_at":          nil,
			"scan_lease_owner":      "",
			"scan_lease_expires_at": nil,
		}).Error; err != nil {
		logger.L.Error("startup fix: watch_folders", zap.Error(err))
		return err
	}

	// 2. scan_runs
	if err := s.db.WithContext(ctx).
		Model(&model.ScanRun{}).
		Where(`status = ? AND NOT EXISTS (
			SELECT 1 FROM watch_folders
			WHERE watch_folders.id = scan_runs.watch_folder_id
			AND watch_folders.scan_lease_expires_at > ?
		)`, model.ScanRunStatusRunning, now).
		Updates(map[string]interface{}{
			"status":        model.ScanRunStatusCanceled,
			"finished_at":   now,
			"error_message": "scan interrupted by process shutdown",
		}).Error; err != nil {
		logger.L.Error("startup fix: scan_runs", zap.Error(err))
		return err
	}

	logger.L.Info("startup status fix done")
	return nil
}
