// Package repository 负责所有数据库操作，每实体单独文件，不写业务逻辑。
package repository

import (
	"context"
	"fmt"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

// TaskRepository 上传任务数据访问接口。
type TaskRepository interface {
	Create(t *model.UploadTask) error
	GetByID(id uint) (*model.UploadTask, error)
	Update(t *model.UploadTask) error
	Delete(id uint) error
	ListPending(limit int) ([]model.UploadTask, error)
	// List 按状态分页列表，status 为空表示全部。
	List(status string, offset, limit int) ([]model.UploadTask, error)
	CountByStatus(status string) (int64, error)
	CountAll() (int64, error)
	CountByLocalPath(localPath string) (int64, error)
	ListPendingIDs(limit int) ([]uint, error)
	MarkRunningIfPending(ctx context.Context, id uint) (bool, error)
}

type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 构造 TaskRepository。
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(t *model.UploadTask) error {
	if err := r.db.Create(t).Error; err != nil {
		return fmt.Errorf("task create: %w", err)
	}
	return nil
}

func (r *taskRepository) GetByID(id uint) (*model.UploadTask, error) {
	var t model.UploadTask
	if err := r.db.Preload("FileRecord").First(&t, id).Error; err != nil {
		return nil, fmt.Errorf("task get by id: %w", err)
	}
	return &t, nil
}

func (r *taskRepository) Update(t *model.UploadTask) error {
	if err := r.db.Save(t).Error; err != nil {
		return fmt.Errorf("task update: %w", err)
	}
	return nil
}

func (r *taskRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.UploadTask{}, id).Error; err != nil {
		return fmt.Errorf("task delete: %w", err)
	}
	return nil
}

func (r *taskRepository) ListPending(limit int) ([]model.UploadTask, error) {
	var list []model.UploadTask
	q := r.db.Where("status = ?", model.TaskStatusPending).Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Preload("FileRecord").Find(&list).Error
	return list, err
}

func (r *taskRepository) List(status string, offset, limit int) ([]model.UploadTask, error) {
	var list []model.UploadTask
	q := r.db.Model(&model.UploadTask{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	q = q.Offset(offset).Limit(limit).Order("id DESC")
	err := q.Preload("FileRecord").Find(&list).Error
	return list, err
}

func (r *taskRepository) CountByStatus(status string) (int64, error) {
	var n int64
	q := r.db.Model(&model.UploadTask{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Count(&n).Error
	return n, err
}

func (r *taskRepository) CountAll() (int64, error) {
	var n int64
	err := r.db.Model(&model.UploadTask{}).Count(&n).Error
	return n, err
}

func (r *taskRepository) CountByLocalPath(localPath string) (int64, error) {
	var n int64
	err := r.db.Model(&model.UploadTask{}).Where("local_path = ?", localPath).Count(&n).Error
	return n, err
}

// ListPendingIDs 返回部分 pending 任务的 ID，用于调度器分发。
func (r *taskRepository) ListPendingIDs(limit int) ([]uint, error) {
	var ids []uint
	q := r.db.Model(&model.UploadTask{}).
		Where("status = ?", model.TaskStatusPending).
		Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// MarkRunningIfPending 尝试将指定任务从 pending 原子性更新为 running。
// 返回 true 表示本次调用成功抢占到了该任务。
func (r *taskRepository) MarkRunningIfPending(ctx context.Context, id uint) (bool, error) {
	now := time.Now()
	tx := r.db.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return false, err
	}

	res := tx.Model(&model.UploadTask{}).
		Where("id = ? AND status = ?", id, model.TaskStatusPending).
		Updates(map[string]interface{}{
			"status":         model.TaskStatusRunning,
			"started_at":     now,
			"last_status_at": now,
		})

	if res.Error != nil {
		tx.Rollback()
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		tx.Rollback()
		return false, nil
	}
	return true, tx.Commit().Error
}

