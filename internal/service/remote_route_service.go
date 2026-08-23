package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"

	"go.uber.org/zap"
)

type remoteRouteScanWaker interface{ Wake() }

type remoteRouteObjectOperator interface {
	StatRemote(ctx context.Context, remoteName, remotePath string) (rclone.RemoteObject, bool, error)
	MakeRemoteDirectory(ctx context.Context, remoteName, remotePath string) error
	MoveRemoteObject(ctx context.Context, remoteName, sourcePath, destinationPath string) error
}

type RemoteRouteService interface {
	Create(ctx context.Context, in CreateRemoteRouteInput) (*model.RemoteRoute, error)
	Update(ctx context.Context, id uint, in UpdateRemoteRouteInput) (*model.RemoteRoute, error)
	Delete(ctx context.Context, id uint) error
	Get(ctx context.Context, id uint) (*model.RemoteRoute, error)
	List(ctx context.Context, keyword, status string, page, pageSize int) ([]model.RemoteRoute, int64, error)
	ScheduleScan(ctx context.Context, id uint) error
	ScheduleAllScans(ctx context.Context) (int64, error)
	BrowseFiles(ctx context.Context, id uint, currentPath string, page, pageSize int) (FileBrowseResult, error)
	CreateFolder(ctx context.Context, id uint, parentPath, name string) (RemoteFolderMutationResult, error)
	RenameFolder(ctx context.Context, id uint, folderPath, newName string) (RemoteFolderMutationResult, error)
	MoveFiles(ctx context.Context, id uint, sourcePaths []string, targetFolder string) (RemoteBatchMoveResult, error)
	MoveFilesWithProgress(ctx context.Context, id uint, sourcePaths []string, targetFolder string, onProgress func(RemoteMoveProgress)) (RemoteBatchMoveResult, error)
}

type RemoteFolderMutationResult struct {
	Path             string `json:"path"`
	RefreshScheduled bool   `json:"refresh_scheduled"`
}

type RemoteFileMoveResult struct {
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
}

type RemoteFileMoveFailure struct {
	SourcePath string `json:"source_path"`
	Reason     string `json:"reason"`
}

type RemoteBatchMoveResult struct {
	Moved            []RemoteFileMoveResult  `json:"moved"`
	Failed           []RemoteFileMoveFailure `json:"failed"`
	RefreshScheduled bool                    `json:"refresh_scheduled"`
}

// RemoteMoveProgress is an immutable snapshot emitted while a batch move runs.
type RemoteMoveProgress struct {
	Phase       string
	Total       int
	Processed   int
	CurrentPath string
	Result      RemoteBatchMoveResult
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
	repo     repository.RemoteRouteRepository
	policy   *security.ResourcePolicy
	waker    remoteRouteScanWaker
	operator remoteRouteObjectOperator
}

func NewRemoteRouteService(repo repository.RemoteRouteRepository, policy *security.ResourcePolicy, waker remoteRouteScanWaker, operator remoteRouteObjectOperator) RemoteRouteService {
	return &remoteRouteService{repo: repo, policy: policy, waker: waker, operator: operator}
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

const maxRemoteMoveBatch = 100

func validateRemoteObjectName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
		return "", apperror.Validation("folder name is invalid or exceeds 255 characters", nil)
	}
	return value, nil
}

func normalizeRemoteMutationPath(value string, allowRoot bool) (string, error) {
	normalized, err := normalizeIndexPath(value)
	if err != nil {
		return "", err
	}
	if normalized == "" && !allowRoot {
		return "", apperror.Validation("path must identify an item below the route root", nil)
	}
	return normalized, nil
}

func (s *remoteRouteService) writableRoute(ctx context.Context, id uint) (*model.RemoteRoute, error) {
	if s.operator == nil {
		return nil, errors.New("remote mutation operator is unavailable")
	}
	route, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !route.Enabled {
		return nil, apperror.Conflict("remote route must be enabled before it can be modified", nil)
	}
	if route.Status == model.RemoteRouteStatusScanning {
		return nil, apperror.Conflict("remote route is currently scanning; retry after the scan finishes", nil)
	}
	return route, nil
}

func (s *remoteRouteService) routeObjectPath(route *model.RemoteRoute, relativePath string) (string, error) {
	target := route.RemotePath
	if relativePath != "" {
		target = pathpkg.Join(route.RemotePath, relativePath)
	}
	validated, err := s.policy.ValidateRemote(route.RemoteName, target)
	if err != nil {
		return "", apperror.Validation("remote object path is invalid or outside the allowlist", err)
	}
	return validated, nil
}

func (s *remoteRouteService) scheduleMutationRefresh(ctx context.Context, routeID uint) bool {
	if err := s.repo.ScheduleScan(ctx, routeID); err != nil {
		logger.L.Warn("remote mutation succeeded but index refresh could not be scheduled", zap.Uint("remote_route_id", routeID), zap.Error(err))
		return false
	}
	if s.waker != nil {
		s.waker.Wake()
	}
	return true
}

func (s *remoteRouteService) CreateFolder(ctx context.Context, id uint, parentPath, name string) (RemoteFolderMutationResult, error) {
	route, err := s.writableRoute(ctx, id)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	parentPath, err = normalizeRemoteMutationPath(parentPath, true)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	name, err = validateRemoteObjectName(name)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	parentRemotePath, err := s.routeObjectPath(route, parentPath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	parent, exists, err := s.operator.StatRemote(ctx, route.RemoteName, parentRemotePath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	if !exists || !parent.IsDir {
		return RemoteFolderMutationResult{}, apperror.NotFound("parent remote folder does not exist", nil)
	}
	createdPath := name
	if parentPath != "" {
		createdPath = pathpkg.Join(parentPath, name)
	}
	createdRemotePath, err := s.routeObjectPath(route, createdPath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	if _, exists, err := s.operator.StatRemote(ctx, route.RemoteName, createdRemotePath); err != nil {
		return RemoteFolderMutationResult{}, err
	} else if exists {
		return RemoteFolderMutationResult{}, apperror.Conflict("a remote file or folder with the same name already exists", nil)
	}
	if err := s.operator.MakeRemoteDirectory(ctx, route.RemoteName, createdRemotePath); err != nil {
		if errors.Is(err, rclone.ErrRemoteDirectoryNotCreated) {
			return RemoteFolderMutationResult{}, apperror.Conflict("远端服务未创建文件夹；若使用 OpenList 115 v4.2.2，请升级至 v4.2.3 或更高版本", err)
		}
		return RemoteFolderMutationResult{}, err
	}
	return RemoteFolderMutationResult{Path: createdPath, RefreshScheduled: s.scheduleMutationRefresh(ctx, route.ID)}, nil
}

func (s *remoteRouteService) RenameFolder(ctx context.Context, id uint, folderPath, newName string) (RemoteFolderMutationResult, error) {
	route, err := s.writableRoute(ctx, id)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	folderPath, err = normalizeRemoteMutationPath(folderPath, false)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	newName, err = validateRemoteObjectName(newName)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	sourceRemotePath, err := s.routeObjectPath(route, folderPath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	source, exists, err := s.operator.StatRemote(ctx, route.RemoteName, sourceRemotePath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	if !exists || !source.IsDir {
		return RemoteFolderMutationResult{}, apperror.NotFound("remote folder does not exist", nil)
	}
	parentPath := parentIndexPath(folderPath)
	destinationPath := newName
	if parentPath != "" {
		destinationPath = pathpkg.Join(parentPath, newName)
	}
	if destinationPath == folderPath {
		return RemoteFolderMutationResult{}, apperror.Validation("new folder name must be different", nil)
	}
	destinationRemotePath, err := s.routeObjectPath(route, destinationPath)
	if err != nil {
		return RemoteFolderMutationResult{}, err
	}
	if _, exists, err := s.operator.StatRemote(ctx, route.RemoteName, destinationRemotePath); err != nil {
		return RemoteFolderMutationResult{}, err
	} else if exists {
		return RemoteFolderMutationResult{}, apperror.Conflict("a remote file or folder with the same name already exists", nil)
	}
	if err := s.operator.MoveRemoteObject(ctx, route.RemoteName, sourceRemotePath, destinationRemotePath); err != nil {
		return RemoteFolderMutationResult{}, err
	}
	return RemoteFolderMutationResult{Path: destinationPath, RefreshScheduled: s.scheduleMutationRefresh(ctx, route.ID)}, nil
}

func (s *remoteRouteService) MoveFiles(ctx context.Context, id uint, sourcePaths []string, targetFolder string) (RemoteBatchMoveResult, error) {
	return s.MoveFilesWithProgress(ctx, id, sourcePaths, targetFolder, nil)
}

func (s *remoteRouteService) MoveFilesWithProgress(ctx context.Context, id uint, sourcePaths []string, targetFolder string, onProgress func(RemoteMoveProgress)) (RemoteBatchMoveResult, error) {
	result := RemoteBatchMoveResult{Moved: []RemoteFileMoveResult{}, Failed: []RemoteFileMoveFailure{}}
	if len(sourcePaths) == 0 {
		return result, apperror.Validation("source_paths must contain at least one file", nil)
	}
	if len(sourcePaths) > maxRemoteMoveBatch {
		return result, apperror.Validation("a batch can move at most 100 files", nil)
	}
	report := func(phase, currentPath string) {
		if onProgress == nil {
			return
		}
		snapshot := RemoteBatchMoveResult{
			Moved:            append([]RemoteFileMoveResult(nil), result.Moved...),
			Failed:           append([]RemoteFileMoveFailure(nil), result.Failed...),
			RefreshScheduled: result.RefreshScheduled,
		}
		onProgress(RemoteMoveProgress{
			Phase:       phase,
			Total:       len(sourcePaths),
			Processed:   len(snapshot.Moved) + len(snapshot.Failed),
			CurrentPath: currentPath,
			Result:      snapshot,
		})
	}
	report("preparing", "")
	route, err := s.writableRoute(ctx, id)
	if err != nil {
		return result, err
	}
	targetFolder, err = normalizeRemoteMutationPath(targetFolder, true)
	if err != nil {
		return result, err
	}
	targetRemotePath, err := s.routeObjectPath(route, targetFolder)
	if err != nil {
		return result, err
	}
	target, exists, err := s.operator.StatRemote(ctx, route.RemoteName, targetRemotePath)
	if err != nil {
		return result, err
	}
	if !exists || !target.IsDir {
		return result, apperror.NotFound("target remote folder does not exist", nil)
	}

	type moveCandidate struct {
		sourcePath       string
		destinationPath  string
		sourceRemotePath string
		destRemotePath   string
	}
	candidates := make([]moveCandidate, 0, len(sourcePaths))
	seenSources := make(map[string]struct{}, len(sourcePaths))
	seenDestinations := make(map[string]struct{}, len(sourcePaths))
	for _, rawSourcePath := range sourcePaths {
		sourcePath, normalizeErr := normalizeRemoteMutationPath(rawSourcePath, false)
		if normalizeErr != nil {
			return result, normalizeErr
		}
		if _, duplicate := seenSources[sourcePath]; duplicate {
			return result, apperror.Validation("source_paths must not contain duplicates", nil)
		}
		seenSources[sourcePath] = struct{}{}
		report("validating", sourcePath)
		destinationPath := pathpkg.Base(sourcePath)
		if targetFolder != "" {
			destinationPath = pathpkg.Join(targetFolder, destinationPath)
		}
		if destinationPath == sourcePath {
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: sourcePath, Reason: "file is already in the target folder"})
			report("validating", sourcePath)
			continue
		}
		if _, duplicate := seenDestinations[destinationPath]; duplicate {
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: sourcePath, Reason: "another selected file has the same destination name"})
			report("validating", sourcePath)
			continue
		}
		seenDestinations[destinationPath] = struct{}{}
		sourceRemotePath, pathErr := s.routeObjectPath(route, sourcePath)
		if pathErr != nil {
			return result, pathErr
		}
		destinationRemotePath, pathErr := s.routeObjectPath(route, destinationPath)
		if pathErr != nil {
			return result, pathErr
		}
		source, sourceExists, statErr := s.operator.StatRemote(ctx, route.RemoteName, sourceRemotePath)
		if statErr != nil {
			return result, statErr
		}
		if !sourceExists {
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: sourcePath, Reason: "source file does not exist"})
			report("validating", sourcePath)
			continue
		}
		if source.IsDir {
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: sourcePath, Reason: "source path is a directory"})
			report("validating", sourcePath)
			continue
		}
		if _, destinationExists, statErr := s.operator.StatRemote(ctx, route.RemoteName, destinationRemotePath); statErr != nil {
			return result, statErr
		} else if destinationExists {
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: sourcePath, Reason: "destination already exists"})
			report("validating", sourcePath)
			continue
		}
		candidates = append(candidates, moveCandidate{sourcePath: sourcePath, destinationPath: destinationPath, sourceRemotePath: sourceRemotePath, destRemotePath: destinationRemotePath})
	}

	for _, candidate := range candidates {
		report("moving", candidate.sourcePath)
		if err := s.operator.MoveRemoteObject(ctx, route.RemoteName, candidate.sourceRemotePath, candidate.destRemotePath); err != nil {
			logger.L.Warn("remote batch move item failed", zap.Uint("remote_route_id", route.ID), zap.String("source_path", candidate.sourcePath), zap.Error(err))
			result.Failed = append(result.Failed, RemoteFileMoveFailure{SourcePath: candidate.sourcePath, Reason: "remote move failed"})
			report("moving", candidate.sourcePath)
			continue
		}
		result.Moved = append(result.Moved, RemoteFileMoveResult{SourcePath: candidate.sourcePath, DestinationPath: candidate.destinationPath})
		report("moving", candidate.sourcePath)
	}
	if len(result.Moved) > 0 {
		report("refreshing", "")
		result.RefreshScheduled = s.scheduleMutationRefresh(ctx, route.ID)
	}
	report("completed", "")
	return result, nil
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
