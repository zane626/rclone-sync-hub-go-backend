package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/pathpipeline"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"
)

// WatchFolderService 监听文件夹业务接口。
type WatchFolderService interface {
	Create(ctx context.Context, in CreateWatchFolderInput) (*model.WatchFolder, error)
	Update(ctx context.Context, id uint, in UpdateWatchFolderInput) (*model.WatchFolder, error)
	Delete(ctx context.Context, id uint) error
	Get(ctx context.Context, id uint) (*model.WatchFolder, error)
	List(ctx context.Context, status, keyword string, page, pageSize int) ([]model.WatchFolder, int64, error)
}

type watchFolderService struct {
	repo       repository.WatchFolderRepository
	remoteRepo repository.RemoteRouteRepository
	policy     *security.ResourcePolicy
}

// NewWatchFolderService 创建 WatchFolderService。
func NewWatchFolderService(repo repository.WatchFolderRepository, remoteRepo repository.RemoteRouteRepository, policy *security.ResourcePolicy) WatchFolderService {
	return &watchFolderService{repo: repo, remoteRepo: remoteRepo, policy: policy}
}

// CreateWatchFolderInput 创建监听文件夹的入参。
type CreateWatchFolderInput struct {
	Name               string
	LocalPath          string
	RemoteRouteID      uint
	RemoteName         string
	RemotePath         string
	SyncType           string
	MaxDepth           int
	FilterKeywords     string // 多行关键字，换行分隔，校验时去除每行首尾空格
	ScanIntervalSecond int
	PathPipeline       []model.UploadPathPipelineStep
}

// UpdateWatchFolderInput 更新监听文件夹的入参。
type UpdateWatchFolderInput struct {
	Name               *string
	LocalPath          *string
	RemoteRouteID      *uint
	RemoteName         *string
	RemotePath         *string
	SyncType           *string
	MaxDepth           *int
	FilterKeywords     *string
	ScanIntervalSecond *int
	PathPipeline       *[]model.UploadPathPipelineStep
	Status             *string
	Enabled            *bool
}

func (s *watchFolderService) Create(ctx context.Context, in CreateWatchFolderInput) (*model.WatchFolder, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 255 {
		return nil, apperror.Validation("name is required and must not exceed 255 characters", nil)
	}
	localPath, err := s.policy.ValidateLocalDirectory(in.LocalPath)
	if err != nil {
		return nil, apperror.Validation("local directory is invalid, inaccessible, or outside the allowlist", err)
	}
	remoteRouteID, remoteName, remotePath := in.RemoteRouteID, strings.TrimSpace(in.RemoteName), in.RemotePath
	if remoteRouteID > 0 {
		route, routeErr := s.remoteRepo.GetByID(ctx, remoteRouteID)
		if routeErr != nil {
			return nil, remoteRouteRepositoryError(routeErr)
		}
		remoteName, remotePath = route.RemoteName, route.RemotePath
	} else if remoteName == "" || strings.TrimSpace(remotePath) == "" {
		return nil, apperror.Validation("remote_route_id is required", nil)
	}
	remotePath, err = s.policy.ValidateRemote(remoteName, remotePath)
	if err != nil {
		return nil, apperror.Validation("remote destination is invalid or outside the allowlist", err)
	}
	if in.MaxDepth < 0 || in.MaxDepth > 1000 {
		return nil, apperror.Validation("max_depth must be between 0 and 1000", nil)
	}
	if len(in.FilterKeywords) > 16*1024 {
		return nil, apperror.Validation("filter_keywords is too large", nil)
	}
	pathPipeline, err := pathpipeline.Normalize(in.PathPipeline)
	if err != nil {
		return nil, apperror.Validation("path_pipeline is invalid", err)
	}
	now := time.Now()
	syncType := in.SyncType
	if syncType == "" {
		syncType = model.WatchFolderSyncTypeLocalToRemote
	}
	interval := in.ScanIntervalSecond
	if interval <= 0 {
		interval = 300
	}
	if interval < 10 || interval > 7*24*60*60 {
		return nil, apperror.Validation("scan_interval_seconds must be between 10 and 604800", nil)
	}
	if syncType != model.WatchFolderSyncTypeLocalToRemote {
		return nil, apperror.Validation("unsupported sync_type", nil)
	}
	if err := s.ensurePathDoesNotOverlap(ctx, 0, localPath); err != nil {
		return nil, err
	}
	f := &model.WatchFolder{
		Name:                in.Name,
		LocalPath:           localPath,
		RemoteRouteID:       remoteRouteID,
		RemoteName:          remoteName,
		RemotePath:          remotePath,
		SyncType:            syncType,
		MaxDepth:            in.MaxDepth,
		FilterKeywords:      in.FilterKeywords,
		ScanIntervalSeconds: interval,
		PathPipeline:        pathPipeline,
		Status:              model.WatchFolderStatusWatching,
		Enabled:             true,
		LastActiveAt:        &now,
	}
	if err := s.repo.Create(f); err != nil {
		return nil, watchFolderRepositoryError(err)
	}
	return f, nil
}

func (s *watchFolderService) Update(ctx context.Context, id uint, in UpdateWatchFolderInput) (*model.WatchFolder, error) {
	if in.ScanIntervalSecond != nil && (*in.ScanIntervalSecond < 10 || *in.ScanIntervalSecond > 7*24*60*60) {
		return nil, apperror.Validation("scan_interval_seconds must be between 10 and 604800", nil)
	}
	if in.Status != nil && *in.Status != "" && *in.Status != model.WatchFolderStatusWatching && *in.Status != model.WatchFolderStatusStopped && *in.Status != model.WatchFolderStatusPaused {
		return nil, apperror.Validation("status may only be watching, stopped, or paused", nil)
	}
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	previousRemoteRouteID, previousRemoteName, previousRemotePath := f.RemoteRouteID, f.RemoteName, f.RemotePath
	previousPathPipeline := append([]model.UploadPathPipelineStep(nil), f.PathPipeline...)
	configurationChanged := in.Name != nil || in.LocalPath != nil || in.RemoteRouteID != nil || in.RemoteName != nil || in.RemotePath != nil || in.SyncType != nil || in.MaxDepth != nil || in.FilterKeywords != nil || in.ScanIntervalSecond != nil || in.PathPipeline != nil
	if f.Status == model.WatchFolderStatusDetecting && configurationChanged {
		return nil, apperror.Conflict("watch folder configuration cannot change during an active scan", nil)
	}
	if in.Name != nil {
		f.Name = *in.Name
	}
	if in.LocalPath != nil {
		f.LocalPath = *in.LocalPath
	}
	if in.RemoteRouteID != nil {
		if *in.RemoteRouteID == 0 {
			return nil, apperror.Validation("remote_route_id is required", nil)
		}
		route, routeErr := s.remoteRepo.GetByID(ctx, *in.RemoteRouteID)
		if routeErr != nil {
			return nil, remoteRouteRepositoryError(routeErr)
		}
		f.RemoteRouteID = route.ID
		f.RemoteName = route.RemoteName
		f.RemotePath = route.RemotePath
	} else if f.RemoteRouteID > 0 && (in.RemoteName != nil || in.RemotePath != nil) {
		return nil, apperror.Validation("select a remote route instead of editing its destination", nil)
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
	if in.FilterKeywords != nil {
		f.FilterKeywords = *in.FilterKeywords
	}
	if in.ScanIntervalSecond != nil && *in.ScanIntervalSecond > 0 {
		f.ScanIntervalSeconds = *in.ScanIntervalSecond
	}
	if in.PathPipeline != nil {
		normalized, normalizeErr := pathpipeline.Normalize(*in.PathPipeline)
		if normalizeErr != nil {
			return nil, apperror.Validation("path_pipeline is invalid", normalizeErr)
		}
		f.PathPipeline = normalized
	}
	if in.Status != nil && *in.Status != "" {
		f.Status = *in.Status
	}
	if in.Enabled != nil {
		f.Enabled = *in.Enabled
		if !f.Enabled && in.Status == nil {
			f.Status = model.WatchFolderStatusStopped
		} else if f.Enabled && in.Status == nil && f.Status == model.WatchFolderStatusStopped {
			f.Status = model.WatchFolderStatusWatching
		}
	}
	if strings.TrimSpace(f.Name) == "" || len(f.Name) > 255 {
		return nil, apperror.Validation("name is required and must not exceed 255 characters", nil)
	}
	localPath, err := s.policy.ValidateLocalDirectory(f.LocalPath)
	if err != nil {
		return nil, apperror.Validation("local directory is invalid, inaccessible, or outside the allowlist", err)
	}
	remotePath, err := s.policy.ValidateRemote(f.RemoteName, f.RemotePath)
	if err != nil {
		return nil, apperror.Validation("remote destination is invalid or outside the allowlist", err)
	}
	if f.MaxDepth < 0 || f.MaxDepth > 1000 {
		return nil, apperror.Validation("max_depth must be between 0 and 1000", nil)
	}
	if len(f.FilterKeywords) > 16*1024 {
		return nil, apperror.Validation("filter_keywords is too large", nil)
	}
	if f.ScanIntervalSeconds < 10 || f.ScanIntervalSeconds > 7*24*60*60 {
		return nil, apperror.Validation("scan_interval_seconds must be between 10 and 604800", nil)
	}
	if !validWatchFolderStatus(f.Status) {
		return nil, apperror.Validation("invalid watch folder status", nil)
	}
	if f.SyncType != model.WatchFolderSyncTypeLocalToRemote {
		return nil, apperror.Validation("unsupported sync_type", nil)
	}
	f.LocalPath = localPath
	f.RemotePath = remotePath
	pipelineChanged := !slices.Equal(previousPathPipeline, f.PathPipeline)
	destinationChanged := previousRemoteRouteID != f.RemoteRouteID || previousRemoteName != f.RemoteName || previousRemotePath != f.RemotePath || pipelineChanged
	if err := s.ensurePathDoesNotOverlap(ctx, id, localPath); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, f, configurationChanged, destinationChanged); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, apperror.Conflict("watch folder changed concurrently, is being scanned, overlaps another path, or already exists", err)
		}
		return nil, watchFolderRepositoryError(err)
	}
	return f, nil
}

func (s *watchFolderService) ensurePathDoesNotOverlap(ctx context.Context, excludeID uint, candidate string) error {
	folders, err := s.repo.ListPaths(ctx, excludeID)
	if err != nil {
		return err
	}
	for _, existing := range folders {
		if localPathsOverlap(candidate, existing.LocalPath) {
			return apperror.Conflict(fmt.Sprintf("local directory overlaps watch folder %q", existing.Name), nil)
		}
	}
	return nil
}

func localPathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ValidateWatchFolderPathTopology fails startup when legacy data contains
// overlapping roots that cannot safely share the absolute-path snapshot key.
func ValidateWatchFolderPathTopology(ctx context.Context, repo repository.WatchFolderRepository) error {
	folders, err := repo.ListPaths(ctx, 0)
	if err != nil {
		return err
	}
	for i := 0; i < len(folders); i++ {
		for j := i + 1; j < len(folders); j++ {
			if localPathsOverlap(folders[i].LocalPath, folders[j].LocalPath) {
				return fmt.Errorf("watch folders %d (%q) and %d (%q) have overlapping local directories; update or remove one before startup", folders[i].ID, folders[i].Name, folders[j].ID, folders[j].Name)
			}
		}
	}
	return nil
}

func validWatchFolderStatus(status string) bool {
	switch status {
	case model.WatchFolderStatusDetecting, model.WatchFolderStatusWatching, model.WatchFolderStatusStopped, model.WatchFolderStatusPaused, model.WatchFolderStatusError:
		return true
	default:
		return false
	}
}

func (s *watchFolderService) Delete(ctx context.Context, id uint) error {
	folder, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if folder.Status == model.WatchFolderStatusDetecting && (folder.ScanLeaseExpiresAt == nil || folder.ScanLeaseExpiresAt.After(time.Now())) {
		return apperror.Conflict("watch folder cannot be deleted during an active scan", nil)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return apperror.Conflict("watch folder cannot be deleted during an active scan", err)
		}
		return watchFolderRepositoryError(err)
	}
	return nil
}

func (s *watchFolderService) Get(ctx context.Context, id uint) (*model.WatchFolder, error) {
	folder, err := s.repo.GetByID(id)
	if err != nil {
		return nil, watchFolderRepositoryError(err)
	}
	return folder, nil
}

func watchFolderRepositoryError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperror.NotFound("watch folder not found", err)
	}
	if errors.Is(err, repository.ErrConflict) {
		return apperror.Conflict("watch folder local path already exists", err)
	}
	return err
}

func (s *watchFolderService) List(ctx context.Context, status, keyword string, page, pageSize int) ([]model.WatchFolder, int64, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if page <= 0 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	offset := (page - 1) * pageSize
	return s.repo.List(status, keyword, offset, pageSize)
}
