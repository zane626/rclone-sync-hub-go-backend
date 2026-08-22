package scheduler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
	"sync"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/security"

	"go.uber.org/zap"
)

type remoteRouteScanRepository interface {
	ListEnabledForScan(ctx context.Context, now time.Time) ([]model.RemoteRoute, error)
	ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error)
	RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error
	UpsertFileRecords(ctx context.Context, records []*model.RemoteFileRecord, batchSize int) error
	MarkUnseenMissing(ctx context.Context, routeID uint, scanStarted, missingAt time.Time) (int64, error)
}

type RemoteRouteScanner interface {
	Run(ctx context.Context)
	ScanOnce(ctx context.Context) error
	Wake()
}

type RemoteRouteScannerConfig struct {
	PollInterval  time.Duration
	RouteTimeout  time.Duration
	MaxConcurrent int
	BatchSize     int
	InstanceID    string
	LeaseDuration time.Duration
	Heartbeat     time.Duration
	Policy        *security.ResourcePolicy
}

type remoteRouteScanner struct {
	repo   remoteRouteScanRepository
	rclone rclone.Client
	cfg    RemoteRouteScannerConfig
	mu     sync.Mutex
	wake   chan struct{}
}

func NewRemoteRouteScanner(repo remoteRouteScanRepository, client rclone.Client, cfg RemoteRouteScannerConfig) RemoteRouteScanner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.RouteTimeout <= 0 {
		cfg.RouteTimeout = 30 * time.Minute
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = scannerInstanceID() + "/remote"
	} else {
		cfg.InstanceID += "/remote"
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 90 * time.Second
	}
	if cfg.Heartbeat <= 0 || cfg.Heartbeat*2 >= cfg.LeaseDuration {
		cfg.Heartbeat = cfg.LeaseDuration / 3
	}
	return &remoteRouteScanner{repo: repo, rclone: client, cfg: cfg, wake: make(chan struct{}, 1)}
}

func (s *remoteRouteScanner) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *remoteRouteScanner) Run(ctx context.Context) {
	logger.L.Info("remote_route_scanner: start", zap.Duration("poll_interval", s.cfg.PollInterval), zap.Int("max_concurrent_routes", s.cfg.MaxConcurrent))
	if err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.L.Error("remote_route_scanner: initial scan failed", zap.Error(err))
	}
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			if err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.L.Error("remote_route_scanner: triggered cycle failed", zap.Error(err))
			}
		case <-ticker.C:
			if err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.L.Error("remote_route_scanner: cycle failed", zap.Error(err))
			}
		}
	}
}

func (s *remoteRouteScanner) ScanOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes, err := s.repo.ListEnabledForScan(ctx, time.Now())
	if err != nil || len(routes) == 0 {
		return err
	}
	semaphore := make(chan struct{}, s.cfg.MaxConcurrent)
	results := make(chan error, len(routes))
	var wg sync.WaitGroup
launchLoop:
	for i := range routes {
		select {
		case <-ctx.Done():
			break launchLoop
		case semaphore <- struct{}{}:
		}
		route := routes[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			if scanErr := s.scanRoute(ctx, &route); scanErr != nil {
				results <- fmt.Errorf("remote route %d (%s): %w", route.ID, route.Name, scanErr)
			}
		}()
	}
	wg.Wait()
	close(results)
	var errs []error
	if ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	for result := range results {
		if result != nil {
			errs = append(errs, result)
		}
	}
	return errors.Join(errs...)
}

func (s *remoteRouteScanner) scanRoute(parent context.Context, route *model.RemoteRoute) error {
	started := time.Now()
	claimed, err := s.repo.ClaimForScan(parent, route.ID, s.cfg.InstanceID, route.UpdatedAt, started, s.cfg.LeaseDuration)
	if err != nil || !claimed {
		return err
	}
	scanCtx, cancel := context.WithTimeout(parent, s.cfg.RouteTimeout)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(s.cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-scanCtx.Done():
				return
			case <-ticker.C:
				ok, renewErr := s.repo.RenewScanLease(scanCtx, route.ID, s.cfg.InstanceID, s.cfg.LeaseDuration)
				if renewErr != nil || !ok {
					logger.L.Warn("remote_route_scanner: lease renewal failed", zap.Uint("remote_route_id", route.ID), zap.Error(renewErr))
					cancel()
					return
				}
			}
		}
	}()

	files, bytes, scanErr := s.indexRoute(scanCtx, route, started)
	cancel()
	<-heartbeatDone
	finished := time.Now()
	status := model.RemoteRouteStatusReady
	errorMessage := ""
	if scanErr != nil {
		status = model.RemoteRouteStatusError
		errorMessage = truncateError(scanErr, 4096)
	}
	interval := time.Duration(route.ScanIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Hour
	}
	updates := map[string]interface{}{
		"status": status, "last_error": errorMessage, "last_scan_finished_at": finished,
		"last_scan_duration_ms": finished.Sub(started).Milliseconds(), "next_scan_at": finished.Add(interval),
		"scan_lease_owner": "", "scan_lease_expires_at": nil,
	}
	if scanErr == nil {
		updates["last_scan_success_at"] = finished
		updates["total_file_count"] = files
		updates["total_file_size"] = bytes
	}
	if errors.Is(scanErr, context.Canceled) && parent.Err() != nil {
		updates["next_scan_at"] = nil
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalizeCancel()
	finishErr := s.repo.FinishScan(finalizeCtx, route.ID, s.cfg.InstanceID, updates)
	logger.L.Info("remote_route_scanner: route done", zap.Uint("remote_route_id", route.ID), zap.String("status", status), zap.Int64("files", files), zap.Int64("bytes", bytes), zap.Error(scanErr))
	return errors.Join(scanErr, finishErr)
}

func (s *remoteRouteScanner) indexRoute(ctx context.Context, route *model.RemoteRoute, started time.Time) (int64, int64, error) {
	remotePath := route.RemotePath
	// GORM's MySQL timestamps are millisecond-precision by default. Use the
	// same precision for the scan marker so freshly upserted rows cannot compare
	// as older than the higher-precision in-memory timestamp below.
	scanMarker := started.Truncate(time.Millisecond)
	var err error
	if s.cfg.Policy != nil {
		remotePath, err = s.cfg.Policy.ValidateRemote(route.RemoteName, route.RemotePath)
		if err != nil {
			return 0, 0, err
		}
	}
	batch := make([]*model.RemoteFileRecord, 0, s.cfg.BatchSize)
	seenDirectories := make(map[string]struct{})
	var files, bytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.repo.UpsertFileRecords(ctx, batch, s.cfg.BatchSize); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	queue := func(objectPath, name string, isDir bool, size int64, modTime *time.Time, mimeType string) error {
		hash := sha256.Sum256([]byte(objectPath))
		parentPath := pathpkg.Dir(objectPath)
		if parentPath == "." {
			parentPath = ""
		}
		parentHash := sha256.Sum256([]byte(parentPath))
		seen := scanMarker
		batch = append(batch, &model.RemoteFileRecord{
			RemoteRouteID: route.ID, PathHash: fmt.Sprintf("%x", hash), Path: objectPath, ParentHash: fmt.Sprintf("%x", parentHash), ParentPath: parentPath,
			Name: name, IsDir: isDir, Size: size, ModTime: modTime, MimeType: mimeType, LastSeenAt: &seen, MissingAt: nil,
		})
		if len(batch) >= s.cfg.BatchSize {
			return flush()
		}
		return nil
	}
	ensureDirectories := func(objectPath string, includeSelf bool) error {
		directory := pathpkg.Dir(objectPath)
		if includeSelf {
			directory = objectPath
		}
		if directory == "." || directory == "" {
			return nil
		}
		parts := strings.Split(directory, "/")
		for i := range parts {
			current := strings.Join(parts[:i+1], "/")
			if _, exists := seenDirectories[current]; exists {
				continue
			}
			seenDirectories[current] = struct{}{}
			if err := queue(current, parts[i], true, 0, nil, ""); err != nil {
				return err
			}
		}
		return nil
	}
	walkErr := s.rclone.WalkRemote(ctx, route.RemoteName, remotePath, func(object rclone.RemoteObject) error {
		objectPath, cleanErr := cleanRemoteObjectPath(object.Path)
		if cleanErr != nil {
			return cleanErr
		}
		if objectPath == "" {
			return nil
		}
		if err := ensureDirectories(objectPath, object.IsDir); err != nil {
			return err
		}
		if object.IsDir {
			return nil
		}
		modTime := object.ModTime
		var modTimePointer *time.Time
		if !modTime.IsZero() {
			modTimePointer = &modTime
		}
		name := object.Name
		if strings.TrimSpace(name) == "" {
			name = pathpkg.Base(objectPath)
		}
		if err := queue(objectPath, name, false, object.Size, modTimePointer, object.MimeType); err != nil {
			return err
		}
		files++
		if object.Size > 0 {
			bytes += object.Size
		}
		return nil
	})
	if walkErr != nil {
		return files, bytes, walkErr
	}
	if err := flush(); err != nil {
		return files, bytes, err
	}
	if _, err := s.repo.MarkUnseenMissing(ctx, route.ID, scanMarker, time.Now()); err != nil {
		return files, bytes, err
	}
	return files, bytes, nil
}

func cleanRemoteObjectPath(value string) (string, error) {
	value = strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
	if value == "" {
		return "", nil
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || len(cleaned) > 2048 {
		return "", fmt.Errorf("invalid remote object path %q", value)
	}
	return cleaned, nil
}
