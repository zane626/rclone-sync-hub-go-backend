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

// WatchFolderRepository 监听文件夹数据访问接口。
type WatchFolderRepository interface {
	Create(f *model.WatchFolder) error
	GetByID(id uint) (*model.WatchFolder, error)
	Update(ctx context.Context, f *model.WatchFolder, requireIdle, destinationChanged bool) error
	Delete(ctx context.Context, id uint) error
	// List 按状态分页查询，status 为空则不过滤；keyword 非空时对 name/local_path/remote_name/remote_path 模糊查询。
	List(status, keyword string, offset, limit int) ([]model.WatchFolder, int64, error)
	ListPaths(ctx context.Context, excludeID uint) ([]model.WatchFolder, error)
	ListEnabledForScan(ctx context.Context, now time.Time) ([]model.WatchFolder, error)
	ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error)
	RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error
	RecordUploadSuccess(ctx context.Context, id uint, fileSize int64, at time.Time) error
	RecordUploadFailure(ctx context.Context, id uint, at time.Time) error
	ScheduleAllEnabledNow(ctx context.Context) (int64, error)
}

// ClaimForScan prevents multiple application replicas from scanning the same
// folder concurrently. An expired lease is recoverable after a crashed process.
func (r *watchFolderRepository) ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error) {
	query := r.db.WithContext(ctx).Model(&model.WatchFolder{}).
		Where("id = ? AND enabled = ?", id, true).
		Where("status NOT IN ?", []string{model.WatchFolderStatusStopped, model.WatchFolderStatusPaused}).
		Where("next_scan_at IS NULL OR next_scan_at <= ?", startedAt).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", startedAt)
	if !expectedUpdatedAt.IsZero() {
		query = query.Where("updated_at = ?", expectedUpdatedAt)
	}
	result := query.Updates(map[string]interface{}{
		"status":                model.WatchFolderStatusDetecting,
		"last_error":            "",
		"last_scan_at":          startedAt,
		"last_scan_started_at":  startedAt,
		"scan_lease_owner":      owner,
		"scan_lease_expires_at": startedAt.Add(leaseDuration),
	})
	if result.Error != nil {
		return false, fmt.Errorf("watch_folder claim for scan: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *watchFolderRepository) RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.WatchFolder{}).
		Where("id = ? AND status = ? AND scan_lease_owner = ?", id, model.WatchFolderStatusDetecting, owner).
		Update("scan_lease_expires_at", time.Now().Add(leaseDuration))
	if result.Error != nil {
		return false, fmt.Errorf("watch_folder renew scan lease: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

type watchFolderRepository struct {
	db *gorm.DB
}

// NewWatchFolderRepository 构造 WatchFolderRepository。
func NewWatchFolderRepository(db *gorm.DB) WatchFolderRepository {
	return &watchFolderRepository{db: db}
}

func (r *watchFolderRepository) Create(f *model.WatchFolder) error {
	if err := r.db.Create(f).Error; err != nil {
		return fmt.Errorf("watch_folder create: %w", classifyConstraintError(err))
	}
	return nil
}

func (r *watchFolderRepository) GetByID(id uint) (*model.WatchFolder, error) {
	var f model.WatchFolder
	if err := r.db.First(&f, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("watch folder %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("watch_folder get by id: %w", err)
	}
	return &f, nil
}

func (r *watchFolderRepository) Update(ctx context.Context, f *model.WatchFolder, requireIdle, destinationChanged bool) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	query := tx.Model(&model.WatchFolder{}).Where("id = ?", f.ID)
	if !f.UpdatedAt.IsZero() {
		query = query.Where("updated_at = ?", f.UpdatedAt)
	}
	if requireIdle {
		query = query.Where("status <> ?", model.WatchFolderStatusDetecting)
	}
	result := query.Updates(map[string]interface{}{
		"name":                  f.Name,
		"local_path":            f.LocalPath,
		"remote_route_id":       f.RemoteRouteID,
		"remote_name":           f.RemoteName,
		"remote_path":           f.RemotePath,
		"sync_type":             f.SyncType,
		"max_depth":             f.MaxDepth,
		"filter_keywords":       f.FilterKeywords,
		"scan_interval_seconds": f.ScanIntervalSeconds,
		"status":                f.Status,
		"enabled":               f.Enabled,
		"next_scan_at":          nil,
	})
	if result.Error != nil {
		tx.Rollback()
		return fmt.Errorf("watch_folder update: %w", classifyConstraintError(result.Error))
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := tx.Model(&model.WatchFolder{}).Where("id = ?", f.ID).Count(&count).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("watch_folder verify update: %w", err)
		}
		tx.Rollback()
		if count == 0 {
			return fmt.Errorf("watch folder %d: %w", f.ID, ErrNotFound)
		}
		return fmt.Errorf("watch folder %d changed concurrently or is being scanned: %w", f.ID, ErrConflict)
	}
	if destinationChanged {
		now := time.Now()
		if err := tx.Model(&model.FileRecord{}).Where("watch_folder_id = ?", f.ID).Update("uploaded_at", nil).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("watch_folder reset file destinations: %w", err)
		}
		if err := tx.Model(&model.UploadTask{}).Where("watch_folder_id = ? AND status = ?", f.ID, model.TaskStatusRunning).
			Updates(map[string]interface{}{"cancel_requested_at": now, "last_status_at": now}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("watch_folder cancel running tasks after destination change: %w", err)
		}
		if err := tx.Model(&model.UploadTask{}).Where("watch_folder_id = ? AND status IN ?", f.ID, []string{model.TaskStatusPending, model.TaskStatusPaused, model.TaskStatusFailed}).
			Updates(map[string]interface{}{
				"status": model.TaskStatusCanceled, "error_message": "watch folder remote route changed", "finished_at": now,
				"canceled_at": now, "last_status_at": now, "next_retry_at": nil, "cancel_requested_at": nil,
			}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("watch_folder cancel queued tasks after destination change: %w", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("watch_folder update commit: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) Delete(ctx context.Context, id uint) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			tx.Rollback()
			panic(recovered)
		}
	}()
	var folder model.WatchFolder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "scan_lease_expires_at").First(&folder, id).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("watch folder %d: %w", id, ErrNotFound)
		}
		return fmt.Errorf("watch_folder lock for delete: %w", err)
	}
	if folder.Status == model.WatchFolderStatusDetecting && (folder.ScanLeaseExpiresAt == nil || folder.ScanLeaseExpiresAt.After(time.Now())) {
		tx.Rollback()
		return fmt.Errorf("watch folder %d is being scanned: %w", id, ErrConflict)
	}
	now := time.Now()
	if err := tx.Model(&model.UploadTask{}).
		Where("watch_folder_id = ? AND status = ?", id, model.TaskStatusRunning).
		Updates(map[string]interface{}{"cancel_requested_at": now, "last_status_at": now}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("watch_folder cancel running tasks: %w", err)
	}
	if err := tx.Model(&model.UploadTask{}).
		Where("watch_folder_id = ? AND status IN ?", id, []string{model.TaskStatusPending, model.TaskStatusPaused, model.TaskStatusFailed}).
		Updates(map[string]interface{}{
			"status":              model.TaskStatusCanceled,
			"error_message":       "watch folder deleted",
			"finished_at":         now,
			"canceled_at":         now,
			"last_status_at":      now,
			"next_retry_at":       nil,
			"cancel_requested_at": nil,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("watch_folder cancel queued tasks: %w", err)
	}
	if err := tx.Model(&model.FileRecord{}).Where("watch_folder_id = ?", id).Update("watch_folder_id", 0).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("watch_folder detach file snapshots: %w", err)
	}
	if err := tx.Delete(&model.WatchFolder{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("watch_folder delete: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("watch_folder delete commit: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) List(status, keyword string, offset, limit int) ([]model.WatchFolder, int64, error) {
	var list []model.WatchFolder
	var total int64

	q := r.db.Model(&model.WatchFolder{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR local_path LIKE ? OR remote_name LIKE ? OR remote_path LIKE ?", like, like, like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("watch_folder count: %w", err)
	}

	q = q.Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("watch_folder list: %w", err)
	}
	return list, total, nil
}

func (r *watchFolderRepository) ListPaths(ctx context.Context, excludeID uint) ([]model.WatchFolder, error) {
	var folders []model.WatchFolder
	query := r.db.WithContext(ctx).Model(&model.WatchFolder{}).Select("id", "name", "local_path")
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Find(&folders).Error; err != nil {
		return nil, fmt.Errorf("watch_folder list paths: %w", err)
	}
	return folders, nil
}

// ListEnabledForScan 返回到期且允许自动扫描的目录。
// error/detecting 状态也会被重新选择，从而避免一次异常后永久漏扫。
func (r *watchFolderRepository) ListEnabledForScan(ctx context.Context, now time.Time) ([]model.WatchFolder, error) {
	var list []model.WatchFolder
	err := r.db.WithContext(ctx).
		Where("enabled = ?", true).
		Where("status NOT IN ?", []string{model.WatchFolderStatusStopped, model.WatchFolderStatusPaused}).
		Where("next_scan_at IS NULL OR next_scan_at <= ?", now).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", now).
		Order("COALESCE(next_scan_at, created_at) ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("watch_folder list enabled for scan: %w", err)
	}
	return list, nil
}

// FinishScan updates only scanner-owned fields while holding the same lease.
// A manual pause/stop made during a scan is preserved atomically.
func (r *watchFolderRepository) FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error {
	ownedUpdates := make(map[string]interface{}, len(updates))
	for key, value := range updates {
		ownedUpdates[key] = value
	}
	if desiredStatus, ok := ownedUpdates["status"].(string); ok {
		ownedUpdates["status"] = gorm.Expr(
			"CASE WHEN enabled = ? AND status NOT IN ? THEN ? ELSE status END",
			true,
			[]string{model.WatchFolderStatusStopped, model.WatchFolderStatusPaused},
			desiredStatus,
		)
	}
	result := r.db.WithContext(ctx).Model(&model.WatchFolder{}).
		Where("id = ? AND scan_lease_owner = ?", id, owner).
		Updates(ownedUpdates)
	if result.Error != nil {
		return fmt.Errorf("watch_folder finish scan: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("watch_folder finish scan: scan lease was lost")
	}
	return nil
}

// RecordUploadSuccess 使用原子自增，避免 worker 与扫描器互相覆盖目录状态和统计。
func (r *watchFolderRepository) RecordUploadSuccess(ctx context.Context, id uint, fileSize int64, at time.Time) error {
	updates := map[string]interface{}{
		"uploaded_file_count":   gorm.Expr("uploaded_file_count + ?", 1),
		"uploaded_bytes":        gorm.Expr("uploaded_bytes + ?", fileSize),
		"window_uploaded_files": gorm.Expr("window_uploaded_files + ?", 1),
		"window_uploaded_bytes": gorm.Expr("window_uploaded_bytes + ?", fileSize),
		"last_sync_at":          at,
		"last_active_at":        at,
	}
	if err := r.db.WithContext(ctx).Model(&model.WatchFolder{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("watch_folder record upload success: %w", err)
	}
	return nil
}

// RecordUploadFailure 使用原子自增记录失败，不改写扫描状态。
func (r *watchFolderRepository) RecordUploadFailure(ctx context.Context, id uint, at time.Time) error {
	if err := r.db.WithContext(ctx).Model(&model.WatchFolder{}).Where("id = ?", id).Updates(map[string]interface{}{
		"failed_file_count": gorm.Expr("failed_file_count + ?", 1),
		"last_active_at":    at,
	}).Error; err != nil {
		return fmt.Errorf("watch_folder record upload failure: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) ScheduleAllEnabledNow(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).Model(&model.WatchFolder{}).
		Where("enabled = ? AND status NOT IN ?", true, []string{model.WatchFolderStatusStopped, model.WatchFolderStatusPaused}).
		Update("next_scan_at", time.Now())
	if result.Error != nil {
		return 0, fmt.Errorf("watch_folder schedule all now: %w", result.Error)
	}
	return result.RowsAffected, nil
}
