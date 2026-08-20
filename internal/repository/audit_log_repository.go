package repository

import (
	"context"
	"fmt"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

type AuditLogRepository interface {
	Create(ctx context.Context, entry *model.AuditLog) error
	List(ctx context.Context, offset, limit int) ([]model.AuditLog, int64, error)
}

type auditLogRepository struct {
	db *gorm.DB
}

func NewAuditLogRepository(db *gorm.DB) AuditLogRepository {
	return &auditLogRepository{db: db}
}

func (r *auditLogRepository) Create(ctx context.Context, entry *model.AuditLog) error {
	if err := r.db.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("audit log create: %w", err)
	}
	return nil
}

func (r *auditLogRepository) List(ctx context.Context, offset, limit int) ([]model.AuditLog, int64, error) {
	var entries []model.AuditLog
	var total int64
	query := r.db.WithContext(ctx).Model(&model.AuditLog{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
