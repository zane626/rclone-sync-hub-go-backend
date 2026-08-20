package service

import (
	"context"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
)

type OperationsService interface {
	ListScanRuns(ctx context.Context, watchFolderID uint, page, pageSize int) ([]model.ScanRun, int64, error)
	ListAuditLogs(ctx context.Context, page, pageSize int) ([]model.AuditLog, int64, error)
}

type operationsService struct {
	scanRuns repository.ScanRunRepository
	audits   repository.AuditLogRepository
}

func NewOperationsService(scanRuns repository.ScanRunRepository, audits repository.AuditLogRepository) OperationsService {
	return &operationsService{scanRuns: scanRuns, audits: audits}
}

func (s *operationsService) ListScanRuns(ctx context.Context, watchFolderID uint, page, pageSize int) ([]model.ScanRun, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	return s.scanRuns.ListByWatchFolder(ctx, watchFolderID, (page-1)*pageSize, pageSize)
}

func (s *operationsService) ListAuditLogs(ctx context.Context, page, pageSize int) ([]model.AuditLog, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	return s.audits.List(ctx, (page-1)*pageSize, pageSize)
}

func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
