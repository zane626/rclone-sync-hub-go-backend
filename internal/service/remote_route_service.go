package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"
)

type remoteRouteScanWaker interface{ Wake() }

type RemoteRouteService interface {
	Create(ctx context.Context, in CreateRemoteRouteInput) (*model.RemoteRoute, error)
	Update(ctx context.Context, id uint, in UpdateRemoteRouteInput) (*model.RemoteRoute, error)
	Delete(ctx context.Context, id uint) error
	Get(ctx context.Context, id uint) (*model.RemoteRoute, error)
	List(ctx context.Context, keyword, status string, page, pageSize int) ([]model.RemoteRoute, int64, error)
	ScheduleScan(ctx context.Context, id uint) error
	ScheduleAllScans(ctx context.Context) (int64, error)
	BrowseFiles(ctx context.Context, id uint, currentPath string, page, pageSize int) (FileBrowseResult, error)
}

type CreateRemoteRouteInput struct {
	Name                string
	RemoteName          string
	RemotePath          string
	ScanIntervalSeconds int
	Enabled             *bool
}

type UpdateRemoteRouteInput struct {
	Name                *string
	RemoteName          *string
	RemotePath          *string
	ScanIntervalSeconds *int
	Enabled             *bool
}

type remoteRouteService struct {
	repo   repository.RemoteRouteRepository
	policy *security.ResourcePolicy
	waker  remoteRouteScanWaker
}

func NewRemoteRouteService(repo repository.RemoteRouteRepository, policy *security.ResourcePolicy, waker remoteRouteScanWaker) RemoteRouteService {
	return &remoteRouteService{repo: repo, policy: policy, waker: waker}
}

func makeRemoteRouteKey(remoteName, remotePath string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(remoteName) + "\x00" + strings.TrimSpace(remotePath)))
	return fmt.Sprintf("%x", sum)
}

func validateRouteName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return "", apperror.Validation("name is required and must not exceed 255 characters", nil)
	}
	return value, nil
}

func validateRemoteScanInterval(value int) (int, error) {
	if value <= 0 {
		value = 3600
	}
	if value < 60 || value > 7*24*60*60 {
		return 0, apperror.Validation("scan_interval_seconds must be between 60 and 604800", nil)
	}
	return value, nil
}

func (s *remoteRouteService) Create(ctx context.Context, in CreateRemoteRouteInput) (*model.RemoteRoute, error) {
	name, err := validateRouteName(in.Name)
	if err != nil {
		return nil, err
	}
	remotePath, err := s.policy.ValidateRemote(in.RemoteName, in.RemotePath)
	if err != nil {
		return nil, apperror.Validation("remote destination is invalid or outside the allowlist", err)
	}
	interval, err := validateRemoteScanInterval(in.ScanIntervalSeconds)
	if err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := time.Now()
	route := &model.RemoteRoute{
		Name: name, RouteKey: makeRemoteRouteKey(in.RemoteName, remotePath), RemoteName: strings.TrimSpace(in.RemoteName), RemotePath: remotePath,
		Enabled: enabled, Status: model.RemoteRouteStatusPending, ScanIntervalSeconds: interval,
	}
	if enabled {
		route.NextScanAt = &now
	} else {
		route.Status = model.RemoteRouteStatusDisabled
	}
	if err := s.repo.Create(ctx, route); err != nil {
		return nil, remoteRouteRepositoryError(err)
	}
	if enabled && s.waker != nil {
		s.waker.Wake()
	}
	return route, nil
}

func (s *remoteRouteService) Update(ctx context.Context, id uint, in UpdateRemoteRouteInput) (*model.RemoteRoute, error) {
	route, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		route.Name, err = validateRouteName(*in.Name)
		if err != nil {
			return nil, err
		}
	}
	remoteName := route.RemoteName
	remotePath := route.RemotePath
	if in.RemoteName != nil {
		remoteName = strings.TrimSpace(*in.RemoteName)
	}
	if in.RemotePath != nil {
		remotePath = *in.RemotePath
	}
	remotePath, err = s.policy.ValidateRemote(remoteName, remotePath)
	if err != nil {
		return nil, apperror.Validation("remote destination is invalid or outside the allowlist", err)
	}
	destinationChanged := route.RemoteName != remoteName || route.RemotePath != remotePath
	route.RemoteName = remoteName
	route.RemotePath = remotePath
	route.RouteKey = makeRemoteRouteKey(remoteName, remotePath)
	if in.ScanIntervalSeconds != nil {
		route.ScanIntervalSeconds, err = validateRemoteScanInterval(*in.ScanIntervalSeconds)
		if err != nil {
			return nil, err
		}
	}
	if in.Enabled != nil {
		route.Enabled = *in.Enabled
	}
	if route.Enabled {
		if route.Status == model.RemoteRouteStatusDisabled || destinationChanged {
			route.Status = model.RemoteRouteStatusPending
			now := time.Now()
			route.NextScanAt = &now
		}
	} else {
		route.Status = model.RemoteRouteStatusDisabled
		route.NextScanAt = nil
	}
	if err := s.repo.Update(ctx, route, destinationChanged); err != nil {
		return nil, remoteRouteRepositoryError(err)
	}
	if route.Enabled && s.waker != nil {
		s.waker.Wake()
	}
	return s.Get(ctx, id)
}

func (s *remoteRouteService) Delete(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return remoteRouteRepositoryError(err)
	}
	return nil
}

func (s *remoteRouteService) Get(ctx context.Context, id uint) (*model.RemoteRoute, error) {
	route, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, remoteRouteRepositoryError(err)
	}
	return route, nil
}

func (s *remoteRouteService) List(ctx context.Context, keyword, status string, page, pageSize int) ([]model.RemoteRoute, int64, error) {
	page, pageSize = normalizeBrowsePage(page, pageSize)
	if page > 10000 {
		return nil, 0, apperror.Validation("page is too large", nil)
	}
	return s.repo.List(ctx, keyword, status, (page-1)*pageSize, pageSize)
}

func (s *remoteRouteService) ScheduleScan(ctx context.Context, id uint) error {
	if err := s.repo.ScheduleScan(ctx, id); err != nil {
		return remoteRouteRepositoryError(err)
	}
	if s.waker != nil {
		s.waker.Wake()
	}
	return nil
}

func (s *remoteRouteService) ScheduleAllScans(ctx context.Context) (int64, error) {
	count, err := s.repo.ScheduleAllScans(ctx)
	if err != nil {
		return 0, err
	}
	if s.waker != nil {
		s.waker.Wake()
	}
	return count, nil
}

func (s *remoteRouteService) BrowseFiles(ctx context.Context, id uint, currentPath string, page, pageSize int) (FileBrowseResult, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return FileBrowseResult{}, err
	}
	currentPath, err := normalizeIndexPath(currentPath)
	if err != nil {
		return FileBrowseResult{}, err
	}
	page, pageSize = normalizeBrowsePage(page, pageSize)
	if page > 10000 {
		return FileBrowseResult{}, apperror.Validation("page is too large", nil)
	}
	items, total, err := s.repo.ListFileRecords(ctx, id, currentPath, (page-1)*pageSize, pageSize)
	if err != nil {
		return FileBrowseResult{}, err
	}
	entries := make([]IndexedFileEntry, 0, len(items))
	for i := range items {
		status := "available"
		if items[i].MissingAt != nil {
			status = "missing"
		}
		entryType := "file"
		if items[i].IsDir {
			entryType = "directory"
		}
		entries = append(entries, IndexedFileEntry{
			ID: items[i].ID, Type: entryType, Name: items[i].Name, Path: items[i].Path, Status: status,
			Size: items[i].Size, ModTime: items[i].ModTime, LastSeenAt: items[i].LastSeenAt, MissingAt: items[i].MissingAt,
		})
	}
	return FileBrowseResult{CurrentPath: currentPath, ParentPath: parentIndexPath(currentPath), Items: entries, Total: int(total), Page: page, PageSize: pageSize}, nil
}

func remoteRouteRepositoryError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperror.NotFound("remote route not found", err)
	}
	if errors.Is(err, repository.ErrConflict) {
		return apperror.Conflict("remote route is in use, currently scanning, disabled, or already exists", err)
	}
	return err
}
