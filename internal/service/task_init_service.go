package service

import (
	"context"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TaskInitService 负责任务与监听目录的启动修复逻辑。
// 例如：将上一次异常退出残留的 running 任务重置为 pending。
type TaskInitService struct {
	db *gorm.DB
}

// NewTaskInitService 创建 TaskInitService。
func NewTaskInitService(db *gorm.DB) *TaskInitService {
	return &TaskInitService{db: db}
}

// FixStatusesOnStartup 在系统启动时修复异常状态：
// 1. watch_folders 中 status = detecting 改为 watching
// 2. upload_tasks 中 status = running 改为 pending
func (s *TaskInitService) FixStatusesOnStartup(ctx context.Context) error {
	// 1. watch_folders
	if err := s.db.WithContext(ctx).
		Model(&model.WatchFolder{}).
		Where("status = ?", model.WatchFolderStatusDetecting).
		Update("status", model.WatchFolderStatusWatching).Error; err != nil {
		logger.L.Error("startup fix: watch_folders", zap.Error(err))
		return err
	}

	// 2. upload_tasks
	if err := s.db.WithContext(ctx).
		Model(&model.UploadTask{}).
		Where("status = ?", model.TaskStatusRunning).
		Updates(map[string]interface{}{
			"status":      model.TaskStatusPending,
			"started_at":  nil,
			"finished_at": nil,
		}).Error; err != nil {
		logger.L.Error("startup fix: upload_tasks", zap.Error(err))
		return err
	}

	logger.L.Info("startup status fix done")
	return nil
}

