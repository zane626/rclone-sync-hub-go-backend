package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RemoteRouteRepository interface {
	Create(ctx context.Context, route *model.RemoteRoute) error
	GetByID(ctx context.Context, id uint) (*model.RemoteRoute, error)
	List(ctx context.Context, keyword, status string, offset, limit int) ([]model.RemoteRoute, int64, error)
	Update(ctx context.Context, route *model.RemoteRoute, destinationChanged bool) error
	Delete(ctx context.Context, id uint) error
	ListEnabledForScan(ctx context.Context, now time.Time) ([]model.RemoteRoute, error)
	ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error)
	RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error
	ScheduleScan(ctx context.Context, id uint) error
	ScheduleAllScans(ctx context.Context) (int64, error)
	UpsertFileRecords(ctx context.Context, records []*model.RemoteFileRecord, batchSize int) error
	DeleteUnseenFileRecords(ctx context.Context, routeID uint, scanStarted time.Time) (int64, error)
	ListFileRecords(ctx context.Context, routeID uint, parentPath string, offset, limit int) ([]model.RemoteFileRecord, int64, error)
}

type remoteRouteRepository struct{ db *gorm.DB }

func NewRemoteRouteRepository(db *gorm.DB) RemoteRouteRepository {
	return &remoteRouteRepository{db: db}
}

func (r *remoteRouteRepository) Create(ctx context.Context, route *model.RemoteRoute) error {
	if err := r.db.WithContext(ctx).Create(route).Error; err != nil {
		return fmt.Errorf("remote route create: %w", classifyConstraintError(err))
	}
	return nil
}

func (r *remoteRouteRepository) routeQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.RemoteRoute{}).
		Select("remote_routes.*, (SELECT COUNT(*) FROM watch_folders WHERE watch_folders.remote_route_id = remote_routes.id) AS watch_folder_count")
}

func (r *remoteRouteRepository) GetByID(ctx context.Context, id uint) (*model.RemoteRoute, error) {
	var route model.RemoteRoute
	if err := r.routeQuery(ctx).Where("remote_routes.id = ?", id).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("remote route %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("remote route get: %w", err)
	}
	return &route, nil
}

func (r *remoteRouteRepository) List(ctx context.Context, keyword, status string, offset, limit int) ([]model.RemoteRoute, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.RemoteRoute{})
	if keyword != "" {
		like := "%" + keyword + "%"
		base = base.Where("name LIKE ? OR remote_name LIKE ? OR remote_path LIKE ?", like, like, like)
	}
	if status != "" {
		base = base.Where("status = ?", status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remote route count: %w", err)
	}
	var routes []model.RemoteRoute
	query := base.Select("remote_routes.*, (SELECT COUNT(*) FROM watch_folders WHERE watch_folders.remote_route_id = remote_routes.id) AS watch_folder_count").
		Order("created_at DESC, id DESC")
	if limit > 0 {
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Find(&routes).Error; err != nil {
		return nil, 0, fmt.Errorf("remote route list: %w", err)
	}
	return routes, total, nil
}

func (r *remoteRouteRepository) Update(ctx context.Context, route *model.RemoteRoute, destinationChanged bool) error {
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
	var current model.RemoteRoute
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, route.ID).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("remote route %d: %w", route.ID, ErrNotFound)
		}
		return fmt.Errorf("remote route lock: %w", err)
	}
	if current.Status == model.RemoteRouteStatusScanning && (current.ScanLeaseExpiresAt == nil || current.ScanLeaseExpiresAt.After(time.Now())) {
		tx.Rollback()
		return fmt.Errorf("remote route %d is being scanned: %w", route.ID, ErrConflict)
	}
	if !route.UpdatedAt.IsZero() && !current.UpdatedAt.Equal(route.UpdatedAt) {
		tx.Rollback()
		return fmt.Errorf("remote route %d changed concurrently: %w", route.ID, ErrConflict)
	}
	updates := map[string]interface{}{
		"name": route.Name, "route_key": route.RouteKey, "remote_name": route.RemoteName,
		"remote_path": route.RemotePath, "enabled": route.Enabled,
		"scan_interval_seconds": route.ScanIntervalSeconds,
	}
	if route.Enabled {
		updates["status"] = route.Status
		updates["next_scan_at"] = route.NextScanAt
	} else {
		updates["status"] = model.RemoteRouteStatusDisabled
		updates["next_scan_at"] = nil
	}
	if err := tx.Model(&model.RemoteRoute{}).Where("id = ?", route.ID).Updates(updates).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("remote route update: %w", classifyConstraintError(err))
	}
	if destinationChanged {
		now := time.Now()
		var linkedFolders []model.WatchFolder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "scan_lease_expires_at").
			Where("remote_route_id = ?", route.ID).
			Find(&linkedFolders).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("remote route lock linked watch folders: %w", err)
		}
		linkedFolderIDs := make([]uint, 0, len(linkedFolders))
		for i := range linkedFolders {
			if linkedFolders[i].Status == model.WatchFolderStatusDetecting &&
				(linkedFolders[i].ScanLeaseExpiresAt == nil || linkedFolders[i].ScanLeaseExpiresAt.After(now)) {
				tx.Rollback()
				return fmt.Errorf("remote route %d has a watch folder being scanned: %w", route.ID, ErrConflict)
			}
			linkedFolderIDs = append(linkedFolderIDs, linkedFolders[i].ID)
		}
		if err := tx.Model(&model.RemoteFileRecord{}).Where("remote_route_id = ? AND missing_at IS NULL", route.ID).Update("missing_at", now).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("remote route mark previous index missing: %w", err)
		}
		if len(linkedFolderIDs) > 0 {
			if err := tx.Model(&model.WatchFolder{}).Where("id IN ?", linkedFolderIDs).Updates(map[string]interface{}{
				"remote_name": route.RemoteName, "remote_path": route.RemotePath, "next_scan_at": now,
			}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("remote route update linked watch folders: %w", err)
			}
			if err := tx.Model(&model.FileRecord{}).Where("watch_folder_id IN ?", linkedFolderIDs).Update("uploaded_at", nil).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("remote route reset linked file targets: %w", err)
			}
			if err := tx.Model(&model.UploadTask{}).Where("watch_folder_id IN ? AND status = ?", linkedFolderIDs, model.TaskStatusRunning).
				Updates(map[string]interface{}{"cancel_requested_at": now, "last_status_at": now}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("remote route cancel running tasks: %w", err)
			}
			if err := tx.Model(&model.UploadTask{}).Where("watch_folder_id IN ? AND status IN ?", linkedFolderIDs, []string{model.TaskStatusPending, model.TaskStatusPaused, model.TaskStatusFailed}).
				Updates(map[string]interface{}{
					"status": model.TaskStatusCanceled, "error_message": "remote route changed", "finished_at": now,
					"canceled_at": now, "last_status_at": now, "next_retry_at": nil, "cancel_requested_at": nil,
				}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("remote route cancel queued tasks: %w", err)
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("remote route update commit: %w", err)
	}
	return nil
}

func (r *remoteRouteRepository) Delete(ctx context.Context, id uint) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	var route model.RemoteRoute
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&route, id).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("remote route %d: %w", id, ErrNotFound)
		}
		return fmt.Errorf("remote route lock for delete: %w", err)
	}
	if route.Status == model.RemoteRouteStatusScanning && (route.ScanLeaseExpiresAt == nil || route.ScanLeaseExpiresAt.After(time.Now())) {
		tx.Rollback()
		return fmt.Errorf("remote route %d is being scanned: %w", id, ErrConflict)
	}
	var linked int64
	if err := tx.Model(&model.WatchFolder{}).Where("remote_route_id = ?", id).Count(&linked).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("remote route count linked folders: %w", err)
	}
	if linked > 0 {
		tx.Rollback()
		return fmt.Errorf("remote route %d is used by %d watch folders: %w", id, linked, ErrConflict)
	}
	if err := tx.Where("remote_route_id = ?", id).Delete(&model.RemoteFileRecord{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("remote route delete file index: %w", err)
	}
	if err := tx.Delete(&model.RemoteRoute{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("remote route delete: %w", err)
	}
	return tx.Commit().Error
}

func (r *remoteRouteRepository) ListEnabledForScan(ctx context.Context, now time.Time) ([]model.RemoteRoute, error) {
	var routes []model.RemoteRoute
	err := r.db.WithContext(ctx).Where("enabled = ?", true).
		Where("next_scan_at IS NULL OR next_scan_at <= ?", now).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", now).
		Order("COALESCE(next_scan_at, created_at) ASC, id ASC").Find(&routes).Error
	if err != nil {
		return nil, fmt.Errorf("remote route list due: %w", err)
	}
	return routes, nil
}

func (r *remoteRouteRepository) ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error) {
	query := r.db.WithContext(ctx).Model(&model.RemoteRoute{}).Where("id = ? AND enabled = ?", id, true).
		Where("next_scan_at IS NULL OR next_scan_at <= ?", startedAt).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", startedAt)
	if !expectedUpdatedAt.IsZero() {
		query = query.Where("updated_at = ?", expectedUpdatedAt)
	}
	result := query.Updates(map[string]interface{}{
		"status": model.RemoteRouteStatusScanning, "last_error": "", "last_scan_started_at": startedAt,
		"scan_lease_owner": owner, "scan_lease_expires_at": startedAt.Add(leaseDuration),
	})
	if result.Error != nil {
		return false, fmt.Errorf("remote route claim scan: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *remoteRouteRepository) RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.RemoteRoute{}).
		Where("id = ? AND status = ? AND scan_lease_owner = ?", id, model.RemoteRouteStatusScanning, owner).
		Update("scan_lease_expires_at", time.Now().Add(leaseDuration))
	if result.Error != nil {
		return false, fmt.Errorf("remote route renew scan lease: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *remoteRouteRepository) FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error {
	owned := make(map[string]interface{}, len(updates))
	for key, value := range updates {
		owned[key] = value
	}
	if desired, ok := owned["status"].(string); ok {
		owned["status"] = gorm.Expr("CASE WHEN enabled = ? THEN ? ELSE ? END", true, desired, model.RemoteRouteStatusDisabled)
	}
	result := r.db.WithContext(ctx).Model(&model.RemoteRoute{}).Where("id = ? AND scan_lease_owner = ?", id, owner).Updates(owned)
	if result.Error != nil {
		return fmt.Errorf("remote route finish scan: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("remote route finish scan: scan lease was lost")
	}
	return nil
}

func (r *remoteRouteRepository) ScheduleScan(ctx context.Context, id uint) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&model.RemoteRoute{}).Where("id = ? AND enabled = ?", id, true).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", now).
		Updates(map[string]interface{}{"next_scan_at": now, "status": model.RemoteRouteStatusPending})
	if result.Error != nil {
		return fmt.Errorf("remote route schedule scan: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var route model.RemoteRoute
		if err := r.db.WithContext(ctx).Select("id", "enabled", "status", "scan_lease_expires_at").First(&route, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("remote route %d: %w", id, ErrNotFound)
			}
			return err
		}
		if !route.Enabled {
			return fmt.Errorf("remote route %d is disabled: %w", id, ErrConflict)
		}
		return fmt.Errorf("remote route %d is already scanning: %w", id, ErrConflict)
	}
	return nil
}

func (r *remoteRouteRepository) ScheduleAllScans(ctx context.Context) (int64, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&model.RemoteRoute{}).Where("enabled = ?", true).
		Where("scan_lease_expires_at IS NULL OR scan_lease_expires_at <= ?", now).
		Updates(map[string]interface{}{"next_scan_at": now, "status": model.RemoteRouteStatusPending})
	if result.Error != nil {
		return 0, fmt.Errorf("remote route schedule all scans: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r *remoteRouteRepository) UpsertFileRecords(ctx context.Context, records []*model.RemoteFileRecord, batchSize int) error {
	if len(records) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	columns := []string{"path", "parent_hash", "parent_path", "name", "is_dir", "size", "mod_time", "mime_type", "last_seen_at", "missing_at", "updated_at"}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "remote_route_id"}, {Name: "path_hash"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).CreateInBatches(records, batchSize).Error; err != nil {
		return fmt.Errorf("remote file index upsert: %w", err)
	}
	return nil
}

func (r *remoteRouteRepository) DeleteUnseenFileRecords(ctx context.Context, routeID uint, scanStarted time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("remote_route_id = ? AND (missing_at IS NOT NULL OR last_seen_at IS NULL OR last_seen_at < ?)", routeID, scanStarted).
		Delete(&model.RemoteFileRecord{})
	if result.Error != nil {
		return 0, fmt.Errorf("remote file index delete unseen: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r *remoteRouteRepository) ListFileRecords(ctx context.Context, routeID uint, parentPath string, offset, limit int) ([]model.RemoteFileRecord, int64, error) {
	parentDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(parentPath)))
	base := r.db.WithContext(ctx).Model(&model.RemoteFileRecord{}).
		Where("remote_route_id = ? AND parent_hash = ? AND parent_path = ? AND missing_at IS NULL", routeID, parentDigest, parentPath)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("remote file index count: %w", err)
	}
	var entries []model.RemoteFileRecord
	query := base.Order("is_dir DESC, name ASC, id ASC")
	if limit > 0 {
		query = query.Offset(offset).Limit(limit)
	}
	if err := query.Find(&entries).Error; err != nil {
		return nil, 0, fmt.Errorf("remote file index list: %w", err)
	}
	return entries, total, nil
}
