package repository

import (
	"context"
	"fmt"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

type ScanRunRepository interface {
	Create(ctx context.Context, run *model.ScanRun) error
	Finish(ctx context.Context, run *model.ScanRun) error
	ListByWatchFolder(ctx context.Context, watchFolderID uint, offset, limit int) ([]model.ScanRun, int64, error)
}

type scanRunRepository struct {
	db *gorm.DB
}

func NewScanRunRepository(db *gorm.DB) ScanRunRepository {
	return &scanRunRepository{db: db}
}

func (r *scanRunRepository) Create(ctx context.Context, run *model.ScanRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("scan_run create: %w", err)
	}
	return nil
}

func (r *scanRunRepository) Finish(ctx context.Context, run *model.ScanRun) error {
	if err := r.db.WithContext(ctx).Save(run).Error; err != nil {
		return fmt.Errorf("scan_run finish: %w", err)
	}
	return nil
}

func (r *scanRunRepository) ListByWatchFolder(ctx context.Context, watchFolderID uint, offset, limit int) ([]model.ScanRun, int64, error) {
	var list []model.ScanRun
	var total int64
	q := r.db.WithContext(ctx).Model(&model.ScanRun{})
	if watchFolderID > 0 {
		q = q.Where("watch_folder_id = ?", watchFolderID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("scan_run count: %w", err)
	}
	q = q.Order("started_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("scan_run list: %w", err)
	}
	return list, total, nil
}
