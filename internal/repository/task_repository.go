// Package repository 负责所有数据库操作，每实体单独文件，不写业务逻辑。
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskRepository 上传任务数据访问接口。
type TaskRepository interface {
	GetByID(id uint) (*model.UploadTask, error)
	Delete(ctx context.Context, id uint) error
	// List 按状态分页列表，status 为空表示全部；keyword 非空时对 watch_folder_name/file_name/local_path/remote_name/remote_path 模糊查询。
	List(status, keyword string, offset, limit int) ([]model.UploadTask, error)
	// CountForList 与 List 同条件的总数，用于分页。
	CountForList(status, keyword string) (int64, error)
	CountByStatuses(ctx context.Context) (map[string]int64, error)
	ListOpenByWatchFolder(ctx context.Context, watchFolderID uint) ([]model.UploadTask, error)
	CreateIfAbsent(ctx context.Context, task *model.UploadTask) (bool, error)
	CreateManyIfAbsent(ctx context.Context, tasks []*model.UploadTask, batchSize int) (int64, error)
	ClaimNext(ctx context.Context, owner string, leaseDuration time.Duration, maxAttempts int) (*model.UploadTask, error)
	RenewLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	UpdateProgress(ctx context.Context, id uint, owner string, percent float64, speed int64, at time.Time) error
	FinishSuccess(ctx context.Context, id uint, owner string, fileRecordID uint, fingerprint, remoteName, remotePath string, finishedAt time.Time, durationSeconds int64) (bool, bool, error)
	FinishCanceled(ctx context.Context, id uint, owner string, finishedAt time.Time, message string) (bool, error)
	FailOrRetry(ctx context.Context, id uint, owner, message string, nextRetryAt time.Time, maxAttempts int) (string, error)
	ReleaseLease(ctx context.Context, id uint, owner string) error
	IsCancellationRequested(ctx context.Context, id uint) (bool, error)
	RequestCancel(ctx context.Context, id uint, at time.Time) (string, error)
	ResetForRetry(ctx context.Context, id uint, at time.Time) (bool, error)
	PauseIfNotRunning(ctx context.Context, id uint, at time.Time) (bool, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*model.UploadTask, error)
}

type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 构造 TaskRepository。
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) GetByID(id uint) (*model.UploadTask, error) {
	var t model.UploadTask
	if err := r.db.Preload("FileRecord").First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("task %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("task get by id: %w", err)
	}
	return &t, nil
}

func (r *taskRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Where("id = ? AND status <> ?", id, model.TaskStatusRunning).Delete(&model.UploadTask{})
	if result.Error != nil {
		return fmt.Errorf("task delete: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var task model.UploadTask
		if err := r.db.WithContext(ctx).Select("id", "status").First(&task, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("task %d: %w", id, ErrNotFound)
			}
			return fmt.Errorf("task verify delete: %w", err)
		}
		return fmt.Errorf("running task %d cannot be deleted: %w", id, ErrConflict)
	}
	return nil
}

func (r *taskRepository) applyListFilters(q *gorm.DB, status, keyword string) *gorm.DB {
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("watch_folder_name LIKE ? OR file_name LIKE ? OR local_path LIKE ? OR remote_name LIKE ? OR remote_path LIKE ?", like, like, like, like, like)
	}
	return q
}

func (r *taskRepository) List(status, keyword string, offset, limit int) ([]model.UploadTask, error) {
	var list []model.UploadTask
	q := r.db.Model(&model.UploadTask{})
	q = r.applyListFilters(q, status, keyword)
	q = q.Order("created_at DESC").Offset(offset).Limit(limit)
	err := q.Preload("FileRecord").Find(&list).Error
	return list, err
}

func (r *taskRepository) CountForList(status, keyword string) (int64, error) {
	var n int64
	q := r.db.Model(&model.UploadTask{})
	q = r.applyListFilters(q, status, keyword)
	err := q.Count(&n).Error
	return n, err
}

func (r *taskRepository) CountByStatuses(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	if err := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Select("status", "COUNT(*) AS count").Group("status").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("task count by statuses: %w", err)
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Status] = row.Count
	}
	return counts, nil
}

// CreateIfAbsent 依赖 idempotency_key 唯一索引原子地创建任务。
// 返回 false 表示同一文件版本的任务已经存在。
func (r *taskRepository) CreateIfAbsent(ctx context.Context, task *model.UploadTask) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(task)
	if res.Error != nil {
		return false, fmt.Errorf("task create if absent: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CreateManyIfAbsent relies on the idempotency_key unique index and reduces a
// large first scan from one database round trip per task to bounded batches.
func (r *taskRepository) CreateManyIfAbsent(ctx context.Context, tasks []*model.UploadTask, batchSize int) (int64, error) {
	if len(tasks) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(tasks, batchSize)
	if result.Error != nil {
		return 0, fmt.Errorf("batch create tasks if absent: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ListOpenByWatchFolder 批量返回会阻止同一文件版本重复建单的任务。
func (r *taskRepository) ListOpenByWatchFolder(ctx context.Context, watchFolderID uint) ([]model.UploadTask, error) {
	var list []model.UploadTask
	err := r.db.WithContext(ctx).
		Select("id", "watch_folder_id", "local_path", "file_fingerprint", "idempotency_key", "status").
		Where("watch_folder_id = ? AND status IN ?", watchFolderID, []string{
			model.TaskStatusPending,
			model.TaskStatusRunning,
			model.TaskStatusFailed,
			model.TaskStatusPaused,
		}).
		Find(&list).Error
	return list, err
}

// ClaimNext 使用 FOR UPDATE SKIP LOCKED 从 MySQL 原子领取一个到期任务。
// 已过期的 running 租约也可被重新领取；超过最大次数的过期任务会先终结为 failed。
func (r *taskRepository) ClaimNext(ctx context.Context, owner string, leaseDuration time.Duration, maxAttempts int) (*model.UploadTask, error) {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	now := time.Now()
	leaseExpiresAt := now.Add(leaseDuration)
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if recoverValue := recover(); recoverValue != nil {
			tx.Rollback()
			panic(recoverValue)
		}
	}()
	if err := tx.Model(&model.UploadTask{}).
		Where("status IN ? AND cancel_requested_at IS NOT NULL", []string{model.TaskStatusPending, model.TaskStatusPaused, model.TaskStatusFailed}).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusCanceled,
			"error_message":       "task cancellation finalized before claim",
			"finished_at":         now,
			"canceled_at":         now,
			"last_status_at":      now,
			"next_retry_at":       nil,
			"cancel_requested_at": nil,
		}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task finalize queued cancellation: %w", err)
	}
	if err := tx.Model(&model.UploadTask{}).
		Where("status = ? AND cancel_requested_at IS NOT NULL AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", model.TaskStatusRunning, now).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusCanceled,
			"error_message":       "task canceled while worker lease was inactive",
			"finished_at":         now,
			"canceled_at":         now,
			"last_status_at":      now,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"heartbeat_at":        nil,
			"cancel_requested_at": nil,
		}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task finalize abandoned cancellation: %w", err)
	}

	if err := tx.Model(&model.UploadTask{}).
		Where("status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?) AND retry_count >= ?", model.TaskStatusRunning, now, maxAttempts).
		Updates(map[string]interface{}{
			"status":           model.TaskStatusFailed,
			"error_message":    "worker lease expired after maximum attempts",
			"finished_at":      now,
			"last_status_at":   now,
			"lease_owner":      "",
			"lease_expires_at": nil,
			"heartbeat_at":     nil,
		}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task expire exhausted leases: %w", err)
	}

	var task model.UploadTask
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("cancel_requested_at IS NULL AND retry_count < ? AND ((status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)))",
			maxAttempts, model.TaskStatusPending, now, model.TaskStatusRunning, now).
		Order("priority DESC, id ASC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		tx.Rollback()
		return nil, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task claim select: %w", err)
	}

	if err := tx.Model(&model.UploadTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"status":           model.TaskStatusRunning,
		"started_at":       now,
		"finished_at":      nil,
		"last_status_at":   now,
		"next_retry_at":    nil,
		"lease_owner":      owner,
		"lease_expires_at": leaseExpiresAt,
		"heartbeat_at":     now,
		"retry_count":      gorm.Expr("retry_count + 1"),
	}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task claim update: %w", err)
	}
	if err := tx.Preload("FileRecord").First(&task, task.ID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("task claim reload: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("task claim commit: %w", err)
	}
	return &task, nil
}

func (r *taskRepository) RenewLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NULL", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{"lease_expires_at": now.Add(leaseDuration), "heartbeat_at": now})
	return res.RowsAffected > 0, res.Error
}

func (r *taskRepository) UpdateProgress(ctx context.Context, id uint, owner string, percent float64, speed int64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{"progress": percent, "speed": speed, "last_progress_at": at}).Error
}

func (r *taskRepository) FinishSuccess(ctx context.Context, id uint, owner string, fileRecordID uint, fingerprint, remoteName, remotePath string, finishedAt time.Time, durationSeconds int64) (bool, bool, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return false, false, tx.Error
	}
	res := tx.Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NULL", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusSuccess,
			"progress":            100,
			"speed":               0,
			"error_message":       "",
			"finished_at":         finishedAt,
			"last_status_at":      finishedAt,
			"duration_seconds":    durationSeconds,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"heartbeat_at":        nil,
			"cancel_requested_at": nil,
			"next_retry_at":       nil,
		})
	if res.Error != nil {
		tx.Rollback()
		return false, false, res.Error
	}
	if res.RowsAffected == 0 {
		tx.Rollback()
		return false, false, nil
	}
	fileQuery := tx.Model(&model.FileRecord{}).Where("id = ?", fileRecordID)
	if fingerprint != "" {
		fileQuery = fileQuery.Where("fingerprint = ?", fingerprint)
	}
	fileQuery = fileQuery.Where("remote_name = ? AND remote_path = ?", remoteName, remotePath)
	fileResult := fileQuery.Update("uploaded_at", finishedAt)
	if fileResult.Error != nil {
		tx.Rollback()
		return false, false, fileResult.Error
	}
	if err := tx.Commit().Error; err != nil {
		return false, false, err
	}
	return true, fileResult.RowsAffected > 0, nil
}

func (r *taskRepository) FinishCanceled(ctx context.Context, id uint, owner string, finishedAt time.Time, message string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusCanceled,
			"error_message":       message,
			"finished_at":         finishedAt,
			"canceled_at":         finishedAt,
			"last_status_at":      finishedAt,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"heartbeat_at":        nil,
			"cancel_requested_at": nil,
			"next_retry_at":       nil,
		})
	return res.RowsAffected > 0, res.Error
}

func (r *taskRepository) FailOrRetry(ctx context.Context, id uint, owner, message string, nextRetryAt time.Time, maxAttempts int) (string, error) {
	now := time.Now()
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return "", tx.Error
	}
	var task model.UploadTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, model.TaskStatusRunning, owner).
		First(&task).Error; err != nil {
		tx.Rollback()
		return "", fmt.Errorf("task failure transition select: %w", err)
	}

	status := model.TaskStatusFailed
	var retryAt interface{}
	var finishedAt interface{} = now
	if task.CancelRequestedAt != nil {
		status = model.TaskStatusCanceled
	} else if task.RetryCount < maxAttempts {
		status = model.TaskStatusPending
		retryAt = nextRetryAt
		finishedAt = nil
	}
	updates := map[string]interface{}{
		"status":               status,
		"error_message":        message,
		"finished_at":          finishedAt,
		"last_status_at":       now,
		"next_retry_at":        retryAt,
		"lease_owner":          "",
		"lease_expires_at":     nil,
		"heartbeat_at":         nil,
		"accumulated_failures": gorm.Expr("accumulated_failures + 1"),
	}
	if status == model.TaskStatusCanceled {
		updates["canceled_at"] = now
		updates["cancel_requested_at"] = nil
	}
	if err := tx.Model(&model.UploadTask{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		tx.Rollback()
		return "", fmt.Errorf("task failure transition update: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return "", err
	}
	return status, nil
}

func (r *taskRepository) ReleaseLease(ctx context.Context, id uint, owner string) error {
	now := time.Now()
	canceled := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NOT NULL", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusCanceled,
			"error_message":       "task canceled while worker lease was being released",
			"finished_at":         now,
			"canceled_at":         now,
			"last_status_at":      now,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"heartbeat_at":        nil,
			"next_retry_at":       nil,
			"cancel_requested_at": nil,
		})
	if canceled.Error != nil {
		return canceled.Error
	}
	if canceled.RowsAffected > 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND cancel_requested_at IS NULL", id, model.TaskStatusRunning, owner).
		Updates(map[string]interface{}{
			"status":           model.TaskStatusPending,
			"lease_owner":      "",
			"lease_expires_at": nil,
			"heartbeat_at":     nil,
			"next_retry_at":    nil,
			"retry_count":      gorm.Expr("CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END"),
		}).Error
}

func (r *taskRepository) IsCancellationRequested(ctx context.Context, id uint) (bool, error) {
	var task struct {
		Status            string
		CancelRequestedAt *time.Time
	}
	if err := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Select("status", "cancel_requested_at").First(&task, id).Error; err != nil {
		return false, err
	}
	return task.Status == model.TaskStatusCanceled || task.CancelRequestedAt != nil, nil
}

func (r *taskRepository) RequestCancel(ctx context.Context, id uint, at time.Time) (string, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return "", tx.Error
	}
	var task model.UploadTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
		tx.Rollback()
		return "", err
	}
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusCanceled {
		tx.Rollback()
		return task.Status, nil
	}
	updates := map[string]interface{}{"cancel_requested_at": at, "last_status_at": at}
	status := task.Status
	if task.Status != model.TaskStatusRunning {
		status = model.TaskStatusCanceled
		updates["status"] = status
		updates["canceled_at"] = at
		updates["finished_at"] = at
		updates["next_retry_at"] = nil
		updates["cancel_requested_at"] = nil
	}
	if err := tx.Model(&model.UploadTask{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		tx.Rollback()
		return "", err
	}
	return status, tx.Commit().Error
}

func (r *taskRepository) ResetForRetry(ctx context.Context, id uint, at time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status IN ?", id, []string{model.TaskStatusFailed, model.TaskStatusCanceled, model.TaskStatusPaused}).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusPending,
			"progress":            0,
			"speed":               0,
			"retry_count":         0,
			"error_message":       "",
			"started_at":          nil,
			"finished_at":         nil,
			"canceled_at":         nil,
			"cancel_requested_at": nil,
			"lease_owner":         "",
			"lease_expires_at":    nil,
			"heartbeat_at":        nil,
			"next_retry_at":       nil,
			"last_status_at":      at,
		})
	return res.RowsAffected > 0, res.Error
}

func (r *taskRepository) PauseIfNotRunning(ctx context.Context, id uint, at time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UploadTask{}).
		Where("id = ? AND status NOT IN ?", id, []string{model.TaskStatusRunning, model.TaskStatusSuccess}).
		Updates(map[string]interface{}{
			"status":         model.TaskStatusPaused,
			"last_status_at": at,
			"next_retry_at":  nil,
		})
	return res.RowsAffected > 0, res.Error
}

func (r *taskRepository) GetByIdempotencyKey(ctx context.Context, key string) (*model.UploadTask, error) {
	var task model.UploadTask
	if err := r.db.WithContext(ctx).Preload("FileRecord").Where("idempotency_key = ?", key).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("task idempotency key: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("task get by idempotency key: %w", err)
	}
	return &task, nil
}
