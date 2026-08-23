package scheduler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/observability"
	"rclone-sync-hub/internal/pathpipeline"
	"rclone-sync-hub/internal/security"

	"go.uber.org/zap"
)

// parseFilterKeywords 从多行文本解析过滤关键字：按换行分割，每行去除首尾空格，忽略空行。
func parseFilterKeywords(raw string) []string {
	if raw == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var out []string
	for _, line := range lines {
		if keyword := strings.TrimSpace(line); keyword != "" {
			out = append(out, keyword)
		}
	}
	return out
}

func matchFilterKeywords(path string, keywords []string) bool {
	base := filepath.Base(path)
	for _, keyword := range keywords {
		if strings.Contains(path, keyword) || strings.Contains(base, keyword) {
			return true
		}
	}
	return false
}

type watchFolderScanRepository interface {
	ListEnabledForScan(ctx context.Context, now time.Time) ([]model.WatchFolder, error)
	ClaimForScan(ctx context.Context, id uint, owner string, expectedUpdatedAt, startedAt time.Time, leaseDuration time.Duration) (bool, error)
	RenewScanLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	FinishScan(ctx context.Context, id uint, owner string, updates map[string]interface{}) error
	ScheduleAllEnabledNow(ctx context.Context) (int64, error)
}

type fileRecordScanRepository interface {
	CreateSnapshots(ctx context.Context, records []*model.FileRecord, batchSize int) error
	UpdateSnapshots(ctx context.Context, records []*model.FileRecord, clearUploadedIDs []uint, batchSize int) error
	ListForWatchFolder(ctx context.Context, watchFolderID uint, root string) ([]model.FileRecord, error)
	MarkMissing(ctx context.Context, ids []uint, at time.Time, batchSize int) error
}

type taskScanRepository interface {
	ListOpenByWatchFolder(ctx context.Context, watchFolderID uint) ([]model.UploadTask, error)
	CreateIfAbsent(ctx context.Context, task *model.UploadTask) (bool, error)
	CreateManyIfAbsent(ctx context.Context, tasks []*model.UploadTask, batchSize int) (int64, error)
}

type scanRunWriter interface {
	Create(ctx context.Context, run *model.ScanRun) error
	Finish(ctx context.Context, run *model.ScanRun) error
}

// WatchFolderScanner 扫描已到期的监听目录并创建幂等上传任务。
type WatchFolderScanner interface {
	Run(ctx context.Context)
	ScanOnce(ctx context.Context) (created int, err error)
	ScanNow(ctx context.Context) (scheduled int, err error)
}

// ScanNow durably makes every enabled folder due and wakes the background
// scanner. The HTTP request never waits for a potentially long filesystem walk.
func (s *watchFolderScanner) ScanNow(ctx context.Context) (int, error) {
	scheduled, err := s.watchRepo.ScheduleAllEnabledNow(ctx)
	if err != nil {
		return 0, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return int(scheduled), nil
}

// WatchFolderScannerConfig 控制监听目录调度、超时与文件稳定窗口。
type WatchFolderScannerConfig struct {
	PollInterval     time.Duration
	DefaultInterval  time.Duration
	FolderTimeout    time.Duration
	FileStablePeriod time.Duration
	MaxConcurrent    int
	BatchSize        int
	InstanceID       string
	LeaseDuration    time.Duration
	Heartbeat        time.Duration
	ResourcePolicy   *security.ResourcePolicy
	Metrics          *observability.Metrics
}

type watchFolderScanner struct {
	watchRepo watchFolderScanRepository
	fileRepo  fileRecordScanRepository
	taskRepo  taskScanRepository
	runRepo   scanRunWriter
	cfg       WatchFolderScannerConfig
	mu        sync.Mutex
	wake      chan struct{}
}

func NewWatchFolderScanner(
	watchRepo watchFolderScanRepository,
	fileRepo fileRecordScanRepository,
	taskRepo taskScanRepository,
	runRepo scanRunWriter,
	cfg WatchFolderScannerConfig,
) WatchFolderScanner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.DefaultInterval <= 0 {
		cfg.DefaultInterval = 5 * time.Minute
	}
	if cfg.FolderTimeout <= 0 {
		cfg.FolderTimeout = 30 * time.Minute
	}
	if cfg.FileStablePeriod < 0 {
		cfg.FileStablePeriod = 0
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = scannerInstanceID()
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 90 * time.Second
	}
	if cfg.Heartbeat <= 0 || cfg.Heartbeat*2 >= cfg.LeaseDuration {
		cfg.Heartbeat = cfg.LeaseDuration / 3
	}
	return &watchFolderScanner{
		watchRepo: watchRepo,
		fileRepo:  fileRepo,
		taskRepo:  taskRepo,
		runRepo:   runRepo,
		cfg:       cfg,
		wake:      make(chan struct{}, 1),
	}
}

// Run 启动后立即扫描一次，之后按较短轮询周期选择各自已经到期的目录。
func (s *watchFolderScanner) Run(ctx context.Context) {
	logger.L.Info("watch_folder_scanner: start",
		zap.Duration("poll_interval", s.cfg.PollInterval),
		zap.Duration("default_interval", s.cfg.DefaultInterval),
		zap.Duration("folder_timeout", s.cfg.FolderTimeout),
		zap.Duration("file_stable_period", s.cfg.FileStablePeriod),
		zap.Int("max_concurrent_folders", s.cfg.MaxConcurrent),
		zap.Int("batch_size", s.cfg.BatchSize),
		zap.String("instance_id", s.cfg.InstanceID),
		zap.Duration("lease_duration", s.cfg.LeaseDuration),
		zap.Duration("heartbeat_interval", s.cfg.Heartbeat),
	)
	if _, err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.L.Error("watch_folder_scanner: initial scan failed", zap.Error(err))
	}

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			if _, err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.L.Error("watch_folder_scanner: triggered scan cycle failed", zap.Error(err))
			}
		case <-ticker.C:
			if _, err := s.ScanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.L.Error("watch_folder_scanner: scan cycle failed", zap.Error(err))
			}
		}
	}
}

// ScanOnce 只处理已到 NextScanAt 的启用目录。单个目录失败不会阻断其他目录。
func (s *watchFolderScanner) ScanOnce(ctx context.Context) (created int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanDueFolders(ctx)
}

func (s *watchFolderScanner) scanDueFolders(ctx context.Context) (created int, err error) {
	folders, err := s.watchRepo.ListEnabledForScan(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	if len(folders) == 0 {
		logger.L.Debug("watch_folder_scanner: no due folders")
		return 0, nil
	}

	started := time.Now()
	type folderResult struct {
		folder model.WatchFolder
		count  int
		err    error
	}
	results := make(chan folderResult, len(folders))
	semaphore := make(chan struct{}, s.cfg.MaxConcurrent)
	var wg sync.WaitGroup
	launched := 0
launchLoop:
	for i := range folders {
		select {
		case <-ctx.Done():
			break launchLoop
		case semaphore <- struct{}{}:
		}
		folder := folders[i]
		launched++
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			count, scanErr := s.scanOneFolder(ctx, &folder)
			results <- folderResult{folder: folder, count: count, err: scanErr}
		}()
	}
	wg.Wait()
	close(results)
	var scanErrors []error
	if launched < len(folders) && ctx.Err() != nil {
		scanErrors = append(scanErrors, ctx.Err())
	}
	for result := range results {
		created += result.count
		if result.err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("watch folder %d (%s): %w", result.folder.ID, result.folder.Name, result.err))
		}
	}

	logger.L.Info("watch_folder_scanner: cycle done",
		zap.Int("folders", len(folders)),
		zap.Int("new_tasks", created),
		zap.Int("errors", len(scanErrors)),
		zap.Duration("elapsed", time.Since(started)),
	)
	return created, errors.Join(scanErrors...)
}

func (s *watchFolderScanner) scanOneFolder(ctx context.Context, wf *model.WatchFolder) (int, error) {
	started := time.Now()
	claimed, err := s.watchRepo.ClaimForScan(ctx, wf.ID, s.cfg.InstanceID, wf.UpdatedAt, started, s.cfg.LeaseDuration)
	if err != nil {
		return 0, err
	}
	if !claimed {
		return 0, nil
	}

	run := &model.ScanRun{
		WatchFolderID:   wf.ID,
		WatchFolderName: wf.Name,
		Status:          model.ScanRunStatusRunning,
		StartedAt:       started,
	}
	if err := s.runRepo.Create(ctx, run); err != nil {
		logger.L.Warn("watch_folder_scanner: create scan run failed", zap.Uint("watch_folder_id", wf.ID), zap.Error(err))
	}

	folderCtx, cancel := context.WithTimeout(ctx, s.cfg.FolderTimeout)
	stopHeartbeat := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	leaseProblems := make(chan error, 1)
	go func() {
		defer close(heartbeatStopped)
		ticker := time.NewTicker(s.cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-folderCtx.Done():
				return
			case <-ticker.C:
				renewed, renewErr := s.watchRepo.RenewScanLease(folderCtx, wf.ID, s.cfg.InstanceID, s.cfg.LeaseDuration)
				if renewErr != nil || !renewed {
					if renewErr == nil {
						renewErr = errors.New("scan lease was lost")
					}
					leaseProblems <- renewErr
					cancel()
					return
				}
			}
		}
	}()
	stats, scanErr := s.scanFolder(folderCtx, wf)
	close(stopHeartbeat)
	<-heartbeatStopped
	select {
	case leaseErr := <-leaseProblems:
		scanErr = errors.Join(scanErr, leaseErr)
	default:
	}
	cancel()
	finished := time.Now()
	duration := finished.Sub(started)

	status := model.WatchFolderStatusWatching
	runStatus := model.ScanRunStatusSuccess
	errorMessage := ""
	if scanErr != nil {
		errorMessage = truncateError(scanErr, 4096)
		if errors.Is(scanErr, context.Canceled) && ctx.Err() != nil {
			runStatus = model.ScanRunStatusCanceled
		} else {
			status = model.WatchFolderStatusError
			runStatus = model.ScanRunStatusFailed
		}
	}

	updates := map[string]interface{}{
		"status":                status,
		"last_error":            errorMessage,
		"last_scan_finished_at": finished,
		"last_scan_duration_ms": duration.Milliseconds(),
		"next_scan_at":          finished.Add(s.intervalFor(wf)),
		"total_file_count":      stats.FilesSeen,
		"total_file_size":       stats.TotalBytes,
		"scan_lease_owner":      "",
		"scan_lease_expires_at": nil,
	}
	if scanErr == nil {
		updates["last_scan_success_at"] = finished
	}
	if runStatus == model.ScanRunStatusCanceled {
		// 关机中断不应让目录在重启后额外等待一个完整周期。
		updates["next_scan_at"] = nil
	}

	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalizeCancel()
	var finalizeErrors []error
	if err := s.watchRepo.FinishScan(finalizeCtx, wf.ID, s.cfg.InstanceID, updates); err != nil {
		finalizeErrors = append(finalizeErrors, err)
	}

	if run.ID != 0 {
		run.Status = runStatus
		run.FinishedAt = &finished
		run.DurationMilliseconds = duration.Milliseconds()
		run.FilesSeen = stats.FilesSeen
		run.TotalBytes = stats.TotalBytes
		run.FilesUnchanged = stats.FilesUnchanged
		run.FilesStableSkipped = stats.FilesStableSkipped
		run.FilesMissing = stats.FilesMissing
		run.TasksCreated = stats.TasksCreated
		run.ScanErrors = stats.ScanErrors
		run.ErrorMessage = errorMessage
		if err := s.runRepo.Finish(finalizeCtx, run); err != nil {
			finalizeErrors = append(finalizeErrors, err)
		}
	}

	logger.L.Info("watch_folder_scanner: folder done",
		zap.Uint("watch_folder_id", wf.ID),
		zap.String("path", wf.LocalPath),
		zap.String("status", runStatus),
		zap.Int64("files_seen", stats.FilesSeen),
		zap.Int64("files_unchanged", stats.FilesUnchanged),
		zap.Int64("stable_skipped", stats.FilesStableSkipped),
		zap.Int64("files_missing", stats.FilesMissing),
		zap.Int64("tasks_created", stats.TasksCreated),
		zap.Int64("scan_errors", stats.ScanErrors),
		zap.Duration("elapsed", duration),
	)
	s.cfg.Metrics.ObserveScan(runStatus, duration, stats.FilesSeen, stats.TasksCreated)
	return int(stats.TasksCreated), errors.Join(append([]error{scanErr}, finalizeErrors...)...)
}

type folderScanStats struct {
	FilesSeen          int64
	TotalBytes         int64
	FilesUnchanged     int64
	FilesStableSkipped int64
	FilesMissing       int64
	TasksCreated       int64
	ScanErrors         int64
}

func (s *watchFolderScanner) scanFolder(ctx context.Context, wf *model.WatchFolder) (folderScanStats, error) {
	var stats folderScanStats
	if strings.TrimSpace(wf.LocalPath) == "" {
		return stats, errors.New("local path is empty")
	}
	root, err := filepath.Abs(filepath.Clean(wf.LocalPath))
	if err != nil {
		return stats, fmt.Errorf("resolve local path: %w", err)
	}
	if s.cfg.ResourcePolicy != nil {
		root, err = s.cfg.ResourcePolicy.ValidateLocalDirectory(root)
		if err != nil {
			return stats, err
		}
		validatedRemotePath, validateErr := s.cfg.ResourcePolicy.ValidateRemote(wf.RemoteName, wf.RemotePath)
		if validateErr != nil {
			return stats, validateErr
		}
		wf.RemotePath = validatedRemotePath
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return stats, fmt.Errorf("stat local path: %w", err)
	}
	if !rootInfo.IsDir() {
		return stats, fmt.Errorf("local path is not a directory: %s", root)
	}
	uploadPathPipeline, err := pathpipeline.Compile(wf.PathPipeline)
	if err != nil {
		return stats, fmt.Errorf("compile upload path pipeline: %w", err)
	}

	records, err := s.fileRepo.ListForWatchFolder(ctx, wf.ID, root)
	if err != nil {
		return stats, fmt.Errorf("load file snapshots: %w", err)
	}
	recordByPath := make(map[string]*model.FileRecord, len(records))
	for i := range records {
		recordByPath[filepath.Clean(records[i].LocalPath)] = &records[i]
	}

	openTasks, err := s.taskRepo.ListOpenByWatchFolder(ctx, wf.ID)
	if err != nil {
		return stats, fmt.Errorf("load open tasks: %w", err)
	}
	activeKeys := make(map[string]struct{}, len(openTasks))
	legacyActivePaths := make(map[string]struct{})
	for i := range openTasks {
		if openTasks[i].IdempotencyKey != nil && *openTasks[i].IdempotencyKey != "" {
			activeKeys[*openTasks[i].IdempotencyKey] = struct{}{}
		} else if openTasks[i].LocalPath != "" {
			legacyActivePaths[filepath.Clean(openTasks[i].LocalPath)] = struct{}{}
		}
	}

	keywords := parseFilterKeywords(wf.FilterKeywords)
	remotePrefix := strings.TrimSuffix(wf.RemotePath, "/")
	newSnapshots := make([]*model.FileRecord, 0)
	changedSnapshots := make([]*model.FileRecord, 0)
	clearUploadedIDs := make([]uint, 0)
	taskCandidates := make([]*model.UploadTask, 0)
	seenPaths := make(map[string]struct{}, len(records))
	excludedPrefixes := make([]string, 0)
	var firstProblem error
	recordProblem := func(problem error) {
		stats.ScanErrors++
		if firstProblem == nil {
			firstProblem = problem
		}
	}

	walkErr := filepath.WalkDir(root, func(localPath string, entry fs.DirEntry, walkProblem error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkProblem != nil {
			recordProblem(fmt.Errorf("walk %s: %w", localPath, walkProblem))
			if localPath == root {
				return walkProblem
			}
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relativePath, relErr := filepath.Rel(root, localPath)
		if relErr != nil {
			recordProblem(fmt.Errorf("relative path %s: %w", localPath, relErr))
			return nil
		}
		relativeSlash := filepath.ToSlash(relativePath)
		if relativePath != "." && matchFilterKeywords(relativeSlash, keywords) {
			if entry.IsDir() {
				excludedPrefixes = append(excludedPrefixes, filepath.Clean(localPath))
				return filepath.SkipDir
			}
			seenPaths[filepath.Clean(localPath)] = struct{}{}
			return nil
		}
		if entry.IsDir() {
			if wf.MaxDepth > 0 && relativePath != "." && relativeDepth(relativePath) > wf.MaxDepth {
				excludedPrefixes = append(excludedPrefixes, filepath.Clean(localPath))
				return filepath.SkipDir
			}
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			recordProblem(fmt.Errorf("stat file %s: %w", localPath, infoErr))
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		stats.FilesSeen++
		stats.TotalBytes += info.Size()

		localPath = filepath.Clean(localPath)
		seenPaths[localPath] = struct{}{}
		fingerprint := metadataFingerprint(info.Size(), info.ModTime())
		remoteRelativePath := relativeSlash
		pipelineDirectory, pipelineMatched, pipelineErr := uploadPathPipeline.Resolve(entry.Name())
		if pipelineErr != nil {
			recordProblem(fmt.Errorf("resolve upload path for %s: %w", localPath, pipelineErr))
			return nil
		}
		if pipelineMatched {
			remoteRelativePath = joinRemotePath(pipelineDirectory, relativeSlash)
		}
		remotePath := joinRemotePath(remotePrefix, remoteRelativePath)
		if len(remotePath) > 768 {
			recordProblem(fmt.Errorf("remote path for %s exceeds 768 bytes", localPath))
			return nil
		}
		now := time.Now()
		fr, exists := recordByPath[localPath]
		previousFingerprint := ""
		versionChanged := false
		targetChanged := false
		if exists {
			previousFingerprint = fr.Fingerprint
			versionChanged = previousFingerprint != "" && previousFingerprint != fingerprint
			targetChanged = fr.WatchFolderID != wf.ID || fr.RemoteName != wf.RemoteName || fr.RemotePath != remotePath
		} else {
			fr = &model.FileRecord{LocalPath: localPath}
		}

		legacySnapshot := exists && previousFingerprint == ""
		needsSnapshotWrite := !exists || previousFingerprint != fingerprint || targetChanged || fr.RelativePath != relativeSlash || fr.MissingAt != nil
		if needsSnapshotWrite {
			fr.WatchFolderID = wf.ID
			fr.RelativePath = relativeSlash
			fr.RemoteName = wf.RemoteName
			fr.RemotePath = remotePath
			fr.FileSize = info.Size()
			fr.FileModTime = info.ModTime()
			fr.Fingerprint = fingerprint
			fr.LastSeenAt = &now
			// 升级前的记录没有指纹/remote_name，首次补齐元数据时保留其 uploaded_at，避免全量重传。
			clearUploaded := !legacySnapshot && (versionChanged || targetChanged)
			if clearUploaded {
				fr.UploadedAt = nil
			}
			if !exists {
				newSnapshots = append(newSnapshots, fr)
				recordByPath[localPath] = fr
			} else {
				changedSnapshots = append(changedSnapshots, fr)
				if clearUploaded {
					clearUploadedIDs = append(clearUploadedIDs, fr.ID)
				}
			}
		}

		if !fileVersionStable(now, info.ModTime(), fr.LastSeenAt, s.cfg.FileStablePeriod) {
			stats.FilesStableSkipped++
			return nil
		}
		if fr.UploadedAt != nil {
			stats.FilesUnchanged++
			return nil
		}

		idempotencyKey := taskIdempotencyKey(wf.ID, localPath, fingerprint, wf.RemoteName, remotePath)
		if _, ok := activeKeys[idempotencyKey]; ok {
			stats.FilesUnchanged++
			return nil
		}
		if _, ok := legacyActivePaths[localPath]; ok {
			stats.FilesUnchanged++
			return nil
		}

		task := &model.UploadTask{
			FileRecordID:    fr.ID,
			WatchFolderID:   wf.ID,
			WatchFolderName: wf.Name,
			FileName:        filepath.Base(localPath),
			LocalPath:       localPath,
			RemoteName:      wf.RemoteName,
			RemotePath:      remotePath,
			Status:          model.TaskStatusPending,
			FileSize:        info.Size(),
			FileFingerprint: fingerprint,
			IdempotencyKey:  &idempotencyKey,
		}
		taskCandidates = append(taskCandidates, task)
		activeKeys[idempotencyKey] = struct{}{}
		return nil
	})
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if err := s.fileRepo.UpdateSnapshots(ctx, changedSnapshots, clearUploadedIDs, s.cfg.BatchSize); err != nil {
		return stats, err
	}
	if walkErr == nil && firstProblem == nil {
		missingIDs := make([]uint, 0)
		for i := range records {
			recordPath := filepath.Clean(records[i].LocalPath)
			if _, seen := seenPaths[recordPath]; seen || records[i].MissingAt != nil || pathWithinAny(recordPath, excludedPrefixes) {
				continue
			}
			missingIDs = append(missingIDs, records[i].ID)
		}
		if err := s.fileRepo.MarkMissing(ctx, missingIDs, time.Now(), s.cfg.BatchSize); err != nil {
			return stats, fmt.Errorf("mark missing file snapshots: %w", err)
		}
		stats.FilesMissing = int64(len(missingIDs))
	}
	if len(newSnapshots) > 0 {
		if err := s.fileRepo.CreateSnapshots(ctx, newSnapshots, s.cfg.BatchSize); err != nil {
			return stats, fmt.Errorf("batch create file snapshots: %w", err)
		}
		// Reload once so IDs are correct even when another scanner won a unique-key race.
		persisted, err := s.fileRepo.ListForWatchFolder(ctx, wf.ID, root)
		if err != nil {
			return stats, fmt.Errorf("reload file snapshots: %w", err)
		}
		for i := range persisted {
			recordByPath[filepath.Clean(persisted[i].LocalPath)] = &persisted[i]
		}
	}
	for _, task := range taskCandidates {
		record := recordByPath[filepath.Clean(task.LocalPath)]
		if record == nil || record.ID == 0 {
			recordProblem(fmt.Errorf("file snapshot ID unavailable for %s", task.LocalPath))
			continue
		}
		task.FileRecordID = record.ID
	}
	validTasks := taskCandidates[:0]
	for _, task := range taskCandidates {
		if task.FileRecordID != 0 {
			validTasks = append(validTasks, task)
		}
	}
	if len(validTasks) > 0 {
		inserted, err := s.taskRepo.CreateManyIfAbsent(ctx, validTasks, s.cfg.BatchSize)
		if err != nil {
			return stats, err
		}
		stats.TasksCreated += inserted
		stats.FilesUnchanged += int64(len(validTasks)) - inserted
	}
	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return stats, walkErr
		}
		if firstProblem == nil {
			firstProblem = walkErr
		}
	}
	if firstProblem != nil {
		return stats, fmt.Errorf("scan completed with %d error(s), first: %w", stats.ScanErrors, firstProblem)
	}
	return stats, nil
}

func (s *watchFolderScanner) intervalFor(wf *model.WatchFolder) time.Duration {
	if wf.ScanIntervalSeconds > 0 {
		return time.Duration(wf.ScanIntervalSeconds) * time.Second
	}
	return s.cfg.DefaultInterval
}

func metadataFingerprint(size int64, modTime time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d", size, modTime.UnixNano())))
	return fmt.Sprintf("%x", sum)
}

func fileVersionStable(now, modTime time.Time, observedAt *time.Time, period time.Duration) bool {
	if period <= 0 {
		return true
	}
	stableSince := modTime
	// A source clock can be ahead of the application clock. Falling back to
	// the first observation of this fingerprint prevents such a file from
	// remaining permanently in the unstable window.
	if modTime.After(now) {
		if observedAt == nil {
			return false
		}
		stableSince = *observedAt
	}
	return now.Sub(stableSince) >= period
}

func taskIdempotencyKey(watchFolderID uint, localPath, fingerprint, remoteName, remotePath string) string {
	payload := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", watchFolderID, filepath.Clean(localPath), fingerprint, remoteName, remotePath)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum)
}

func joinRemotePath(prefix, relative string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	relative = strings.TrimPrefix(filepath.ToSlash(relative), "/")
	if prefix == "" {
		return relative
	}
	return prefix + "/" + relative
}

func relativeDepth(relativePath string) int {
	clean := filepath.Clean(relativePath)
	if clean == "." || clean == "" {
		return 0
	}
	return len(strings.Split(clean, string(os.PathSeparator)))
}

func pathWithinAny(candidate string, roots []string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, candidate)
		if err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func truncateError(err error, max int) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) <= max {
		return message
	}
	return message[:max]
}

func scannerInstanceID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
