package service

import (
	"context"
	"time"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
)

// WatchFolderService 监听文件夹业务接口。
type WatchFolderService interface {
	Create(ctx context.Context, in CreateWatchFolderInput) (*model.WatchFolder, error)
	Update(ctx context.Context, id uint, in UpdateWatchFolderInput) (*model.WatchFolder, error)
	Delete(ctx context.Context, id uint) error
	Get(ctx context.Context, id uint) (*model.WatchFolder, error)
	List(ctx context.Context, status string, page, pageSize int) ([]model.WatchFolder, int64, error)
}

type watchFolderService struct {
	repo repository.WatchFolderRepository
}

// NewWatchFolderService 创建 WatchFolderService。
func NewWatchFolderService(repo repository.WatchFolderRepository) WatchFolderService {
	return &watchFolderService{repo: repo}
}

// CreateWatchFolderInput 创建监听文件夹的入参。
type CreateWatchFolderInput struct {
	Name               string
	LocalPath          string
	RemoteName         string
	RemotePath         string
	SyncType           string
	MaxDepth           int
	ScanIntervalSecond int
}

// UpdateWatchFolderInput 更新监听文件夹的入参。
type UpdateWatchFolderInput struct {
	Name               *string
	LocalPath          *string
	RemoteName         *string
	RemotePath         *string
	SyncType           *string
	MaxDepth           *int
	ScanIntervalSecond *int
	Status             *string
	Enabled            *bool
}

func (s *watchFolderService) Create(ctx context.Context, in CreateWatchFolderInput) (*model.WatchFolder, error) {
	now := time.Now()
	syncType := in.SyncType
	if syncType == "" {
		syncType = model.WatchFolderSyncTypeLocalToRemote
	}
	interval := in.ScanIntervalSecond
	if interval <= 0 {
		interval = 300
	}
	f := &model.WatchFolder{
		Name:                in.Name,
		LocalPath:           in.LocalPath,
		RemoteName:          in.RemoteName,
		RemotePath:          in.RemotePath,
		SyncType:            syncType,
		MaxDepth:            in.MaxDepth,
		ScanIntervalSeconds: interval,
		Status:              model.WatchFolderStatusWatching,
		Enabled:             true,
		LastActiveAt:        &now,
	}
	if err := s.repo.Create(f); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *watchFolderService) Update(ctx context.Context, id uint, in UpdateWatchFolderInput) (*model.WatchFolder, error) {
	f, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		f.Name = *in.Name
	}
	if in.LocalPath != nil {
		f.LocalPath = *in.LocalPath
	}
	if in.RemoteName != nil {
		f.RemoteName = *in.RemoteName
	}
	if in.RemotePath != nil {
		f.RemotePath = *in.RemotePath
	}
	if in.SyncType != nil && *in.SyncType != "" {
		f.SyncType = *in.SyncType
	}
	if in.MaxDepth != nil {
		f.MaxDepth = *in.MaxDepth
	}
	if in.ScanIntervalSecond != nil && *in.ScanIntervalSecond > 0 {
		f.ScanIntervalSeconds = *in.ScanIntervalSecond
	}
	if in.Status != nil && *in.Status != "" {
		f.Status = *in.Status
	}
	if in.Enabled != nil {
		f.Enabled = *in.Enabled
	}
	if err := s.repo.Update(f); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *watchFolderService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(id)
}

func (s *watchFolderService) Get(ctx context.Context, id uint) (*model.WatchFolder, error) {
	return s.repo.GetByID(id)
}

func (s *watchFolderService) List(ctx context.Context, status string, page, pageSize int) ([]model.WatchFolder, int64, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	return s.repo.List(status, offset, pageSize)
}
