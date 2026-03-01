package repository

import (
	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

// UploadLogRepository 上传日志数据访问接口。
type UploadLogRepository interface {
	Create(l *model.UploadLog) error
	ListByTaskID(taskID uint, limit int) ([]model.UploadLog, error)
}

type uploadLogRepository struct {
	db *gorm.DB
}

// NewUploadLogRepository 构造 UploadLogRepository。
func NewUploadLogRepository(db *gorm.DB) UploadLogRepository {
	return &uploadLogRepository{db: db}
}

func (r *uploadLogRepository) Create(l *model.UploadLog) error {
	return r.db.Create(l).Error
}

func (r *uploadLogRepository) ListByTaskID(taskID uint, limit int) ([]model.UploadLog, error) {
	var list []model.UploadLog
	q := r.db.Where("task_id = ?", taskID).Order("id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&list).Error
	return list, err
}
