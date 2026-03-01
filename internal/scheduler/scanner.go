// Package scheduler 定时扫描本地目录，将未上传文件加入任务队列。
package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/worker"

	"go.uber.org/zap"
)

// Scanner 扫描本地目录，创建 file_record + upload_task 并提交到队列。
type Scanner interface {
	// Run 按 cron 周期执行扫描，阻塞直到 ctx 取消。
	Run(ctx context.Context)
	// ScanOnce 立即执行一次扫描（供 API 触发）。
	ScanOnce(ctx context.Context) (enqueued int, err error)
}

// ScannerConfig 扫描配置（来自 config.ScanConfig）。上传目标由任务表 remote_name/remote_path 决定，此处不再包含。
type ScannerConfig struct {
	LocalPath       string
	CronSchedule   string
	Enabled        bool
	IntervalSeconds int
}

type scanner struct {
	fileRepo repository.FileRecordRepository
	taskRepo repository.TaskRepository
	queue    worker.Queue
	cfg      ScannerConfig
	mu       sync.Mutex
}

// NewScanner 创建扫描器。queue 用于提交新任务。
func NewScanner(
	fileRepo repository.FileRecordRepository,
	taskRepo repository.TaskRepository,
	queue worker.Queue,
	cfg ScannerConfig,
) Scanner {
	return &scanner{
		fileRepo: fileRepo,
		taskRepo: taskRepo,
		queue:    queue,
		cfg:      cfg,
	}
}

// Run 简单按固定间隔执行扫描（cron 解析可选后续用 cron 库），间隔由 ScanConfig.IntervalSeconds 控制。
func (s *scanner) Run(ctx context.Context) {
	interval := time.Duration(s.cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	logger.L.Info("scheduler: start base scanner", zap.Duration("interval", interval), zap.String("root", s.cfg.LocalPath))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.cfg.Enabled {
				continue
			}
			if _, err := s.ScanOnce(ctx); err != nil {
				logger.L.Error("scheduler: scan failed", zap.Error(err))
			}
		}
	}
}

// ScanOnce 遍历 LocalPath，未在 file_records 中且未上传的创建记录并入队。
func (s *scanner) ScanOnce(ctx context.Context) (enqueued int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	root := s.cfg.LocalPath
	if root == "" {
		return 0, nil
	}
	start := time.Now()
	logger.L.Info("scheduler: base scan start", zap.String("root", root))
	// 上传目标由任务表 remote_name/remote_path 决定，此处仅生成相对路径作为 remote_path；remote_name 需由调用方或 watch_folders 提供
	remotePrefix := ""

	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, errWalk error) error {
		if errWalk != nil {
			return errWalk
		}
		if info.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		logger.L.Error("scheduler: base scan walk error", zap.String("root", root), zap.Error(err))
		return 0, err
	}

	for _, localPath := range files {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}
		rel, errRel := filepath.Rel(root, localPath)
		if errRel != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		remotePath := remotePrefix + "/" + rel

		exists, errExists := s.fileRepo.ExistsByLocalPath(localPath)
		if errExists != nil {
			logger.L.Warn("scheduler: exists check failed", zap.String("path", localPath), zap.Error(errExists))
			continue
		}
		if exists {
			continue
		}

		// 创建 file_record
		var size int64
		if st, errStat := os.Stat(localPath); errStat == nil {
			size = st.Size()
		}
		fr := &model.FileRecord{
			LocalPath:    localPath,
			RelativePath: rel,
			RemotePath:   remotePath,
			FileSize:     size,
		}
		if errCreate := s.fileRepo.Create(fr); errCreate != nil {
			logger.L.Warn("scheduler: create file record failed", zap.String("path", localPath), zap.Error(errCreate))
			continue
		}
		// 创建 upload_task（RemoteName/RemotePath 由 watch_folders 或人工创建任务时填写；此处仅本地扫描不填）
		task := &model.UploadTask{
			FileRecordID: fr.ID,
			Status:       model.TaskStatusPending,
			RemotePath:   remotePath, // 与 file_record 一致，便于 worker 使用
		}
		if errTask := s.taskRepo.Create(task); errTask != nil {
			logger.L.Warn("scheduler: create task failed", zap.Uint("fileRecordID", fr.ID), zap.Error(errTask))
			continue
		}
		if errSub := s.queue.Submit(ctx, task.ID); errSub != nil {
			logger.L.Warn("scheduler: submit task failed", zap.Uint("taskID", task.ID), zap.Error(errSub))
			continue
		}
		enqueued++
	}
	elapsed := time.Since(start)
	logger.L.Info("scheduler: base scan done", zap.String("root", root), zap.Int("new_tasks", enqueued), zap.Duration("elapsed", elapsed))
	return enqueued, nil
}
