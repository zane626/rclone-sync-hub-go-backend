package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"

	"go.uber.org/zap"
)

// WatchFolderScanner 从 watch_folders 中读取状态为 watching 的记录，遍历本地目录并在需要时创建上传任务。
type WatchFolderScanner interface {
	Run(ctx context.Context)
	ScanOnce(ctx context.Context) (created int, err error)
}

type watchFolderScanner struct {
	watchRepo       repository.WatchFolderRepository
	fileRepo        repository.FileRecordRepository
	taskRepo        repository.TaskRepository
	rc              rclone.Client
	intervalSeconds int
	mu              sync.Mutex
}

// NewWatchFolderScanner 创建 WatchFolderScanner。
func NewWatchFolderScanner(
	watchRepo repository.WatchFolderRepository,
	fileRepo repository.FileRecordRepository,
	taskRepo repository.TaskRepository,
	rc rclone.Client,
	intervalSeconds int,
) WatchFolderScanner {
	if intervalSeconds <= 0 {
		intervalSeconds = 300
	}
	return &watchFolderScanner{
		watchRepo:       watchRepo,
		fileRepo:        fileRepo,
		taskRepo:        taskRepo,
		rc:              rc,
		intervalSeconds: intervalSeconds,
	}
}

// Run 使用可配置的 ticker 定期执行 ScanOnce。
func (s *watchFolderScanner) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.intervalSeconds) * time.Second)
	defer ticker.Stop()
	logger.L.Info("watch_folder_scanner: start", zap.Int("interval_seconds", s.intervalSeconds))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.ScanOnce(ctx); err != nil {
				logger.L.Error("watch_folder_scanner: scan failed", zap.Error(err))
			}
		}
	}
}

// ScanOnce 获取所有状态为 watching 的 watch_folders，遍历本地文件，并根据远端与任务情况创建 upload_tasks。
func (s *watchFolderScanner) ScanOnce(ctx context.Context) (created int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	folders, _, err := s.watchRepo.List(model.WatchFolderStatusWatching, "", 0, 0)
	if err != nil {
		return 0, err
	}
	if len(folders) == 0 {
		logger.L.Info("watch_folder_scanner: no watching folders")
		return 0, nil
	}

	// 在本轮扫描开始前，先将这些监听目录的状态标记为 detecting，表示“正在检测中”。
	for i := range folders {
		folders[i].Status = model.WatchFolderStatusDetecting
		if err := s.watchRepo.Update(&folders[i]); err != nil {
			logger.L.Warn("watch_folder_scanner: set status detecting failed",
				zap.Uint("id", folders[i].ID),
				zap.Error(err),
			)
		}
	}

	start := time.Now()
	logger.L.Info("watch_folder_scanner: scan start", zap.Int("folders", len(folders)))
	for i := range folders {
		select {
		case <-ctx.Done():
			return created, ctx.Err()
		default:
		}
		n, err := s.scanFolder(ctx, &folders[i])
		if err != nil {
			logger.L.Warn("watch_folder_scanner: scan folder failed",
				zap.Uint("id", folders[i].ID),
				zap.String("path", folders[i].LocalPath),
				zap.Error(err),
			)
			continue
		}
		created += n

		// 单个目录扫描完成后，将状态切回 watching。
		folders[i].Status = model.WatchFolderStatusWatching
		if err := s.watchRepo.Update(&folders[i]); err != nil {
			logger.L.Warn("watch_folder_scanner: set status watching failed",
				zap.Uint("id", folders[i].ID),
				zap.Error(err),
			)
		}
	}
	elapsed := time.Since(start)
	logger.L.Info("watch_folder_scanner: scan done", zap.Int("folders", len(folders)), zap.Int("new_tasks", created), zap.Duration("elapsed", elapsed))
	return created, nil
}

func (s *watchFolderScanner) scanFolder(ctx context.Context, wf *model.WatchFolder) (int, error) {
	root := wf.LocalPath
	if root == "" {
		return 0, nil
	}
	root = filepath.Clean(root)
	remotePrefix := strings.TrimSuffix(wf.RemotePath, "/")
	maxDepth := wf.MaxDepth

	now := time.Now()
	wf.LastScanAt = &now
	if err := s.watchRepo.Update(wf); err != nil {
		logger.L.Warn("watch_folder_scanner: update last_scan_at failed",
			zap.Uint("id", wf.ID),
			zap.Error(err),
		)
	}

	rootDepth := depth(root)
	created := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// 深度控制
		if info.IsDir() {
			if maxDepth > 0 && depth(path) > rootDepth+maxDepth {
				return filepath.SkipDir
			}
			return nil
		}

		// 仅处理文件
		rel, errRel := filepath.Rel(root, path)
		if errRel != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		remotePath := remotePrefix + "/" + relSlash

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 1. 如果远端已存在，则跳过
		exists, err := s.rc.FileExists(ctx, wf.RemoteName, remotePath)
		if err != nil {
			logger.L.Warn("watch_folder_scanner: check remote exists failed",
				zap.String("remote", wf.RemoteName),
				zap.String("remotePath", remotePath),
				zap.Error(err),
			)
			return nil
		}
		if exists {
			return nil
		}

		// 2. 如果 upload_tasks 中已有针对该 LocalPath 的任务，则跳过
		if n, err := s.taskRepo.CountByLocalPath(path); err != nil {
			logger.L.Warn("watch_folder_scanner: count tasks by local path failed",
				zap.String("localPath", path),
				zap.Error(err),
			)
			return nil
		} else if n > 0 {
			return nil
		}

		// 3. 确保 file_records 中存在记录
		var size int64
		if st, errStat := os.Stat(path); errStat == nil {
			size = st.Size()
		}
		existsFR, err := s.fileRepo.ExistsByLocalPath(path)
		if err != nil {
			logger.L.Warn("watch_folder_scanner: file record exists check failed",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}
		var frID uint
		if existsFR {
			fr, err := s.fileRepo.GetByLocalPath(path)
			if err != nil {
				logger.L.Warn("watch_folder_scanner: get file record failed",
					zap.String("path", path),
					zap.Error(err),
				)
				return nil
			}
			frID = fr.ID
		} else {
			fr := &model.FileRecord{
				LocalPath:    path,
				RelativePath: relSlash,
				RemotePath:   remotePath,
				FileSize:     size,
			}
			if err := s.fileRepo.Create(fr); err != nil {
				logger.L.Warn("watch_folder_scanner: create file record failed",
					zap.String("path", path),
					zap.Error(err),
				)
				return nil
			}
			frID = fr.ID
		}

		// 4. 创建 upload_task（状态为 pending），不直接入队，由后续逻辑决定。
		task := &model.UploadTask{
			FileRecordID:    frID,
			WatchFolderID:   wf.ID,
			WatchFolderName: wf.Name,
			FileName:        filepath.Base(path),
			LocalPath:       path,
			RemoteName:      wf.RemoteName,
			RemotePath:      remotePath,
			Status:          model.TaskStatusPending,
			FileSize:        size,
		}
		if err := s.taskRepo.Create(task); err != nil {
			logger.L.Warn("watch_folder_scanner: create task failed",
				zap.Uint("fileRecordID", frID),
				zap.Error(err),
			)
			return nil
		}
		created++
		return nil
	})
	if err != nil {
		logger.L.Error("watch_folder_scanner: scan error", zap.Error(err))
		return created, err
	}
	return created, nil
}

// depth 计算路径的层级深度。
func depth(path string) int {
	clean := filepath.Clean(path)
	if clean == string(os.PathSeparator) {
		return 0
	}
	return len(strings.Split(clean, string(os.PathSeparator)))
}

