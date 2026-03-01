// Package worker 实现基于 channel 的任务队列，可配置最大并发与重试，仅通过 rclone 接口执行上传。
package worker

import (
	"context"
	"sync"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"

	"go.uber.org/zap"
)

// TaskMessage 入队消息：仅携带任务 ID，具体数据由 repository 查询。
type TaskMessage struct {
	TaskID uint
}

// ProgressCallback 进度回调，用于写 upload_logs 或推送前端。
type ProgressCallback func(taskID uint, percent float64, bytesDone, bytesTotal, speed int64, message string)

// Queue 任务队列：从 channel 取任务，经 rclone 执行，更新 DB。
type Queue interface {
	// Submit 将任务 ID 放入队列，非阻塞。
	Submit(ctx context.Context, taskID uint) error
	// Run 启动 worker 池，阻塞直到 ctx 取消。
	Run(ctx context.Context)
}

type queue struct {
	taskRepo      repository.TaskRepository
	logRepo       repository.UploadLogRepository
	fileRepo      repository.FileRecordRepository
	rclone        rclone.Client
	maxConcurrent int
	maxRetry      int
	queueSize     int
	ch            chan TaskMessage
	wg            sync.WaitGroup
	onProgress    ProgressCallback
}

// QueueOption 可选配置。
type QueueOption func(*queue)

// WithProgressCallback 设置进度回调（如写 upload_logs）。
func WithProgressCallback(fn ProgressCallback) QueueOption {
	return func(q *queue) {
		q.onProgress = fn
	}
}

// NewQueue 创建任务队列。上传时使用任务表 upload_tasks 的 remote_name、remote_path，不再从配置注入。
func NewQueue(
	taskRepo repository.TaskRepository,
	logRepo repository.UploadLogRepository,
	fileRepo repository.FileRecordRepository,
	rc rclone.Client,
	maxConcurrent, maxRetry, queueSize int,
	opts ...QueueOption,
) Queue {
	if queueSize <= 0 {
		queueSize = 100
	}
	q := &queue{
		taskRepo:      taskRepo,
		logRepo:       logRepo,
		fileRepo:      fileRepo,
		rclone:        rc,
		maxConcurrent: maxConcurrent,
		maxRetry:      maxRetry,
		queueSize:     queueSize,
		ch:            make(chan TaskMessage, queueSize),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// Submit 将任务放入队列。
func (q *queue) Submit(ctx context.Context, taskID uint) error {
	select {
	case q.ch <- TaskMessage{TaskID: taskID}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run 启动 maxConcurrent 个 goroutine 消费队列。
func (q *queue) Run(ctx context.Context) {
	for i := 0; i < q.maxConcurrent; i++ {
		q.wg.Add(1)
		go q.worker(ctx, i)
	}
	q.wg.Wait()
}

func (q *queue) worker(ctx context.Context, id int) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-q.ch:
			if !ok {
				return
			}
			q.processOne(ctx, msg.TaskID, id)
		}
	}
}

func (q *queue) processOne(ctx context.Context, taskID uint, workerID int) {
	logger.L.Debug("worker: processOne start", zap.Uint("taskID", taskID), zap.Int("workerID", workerID))

	task, err := q.taskRepo.GetByID(taskID)
	if err != nil || task == nil {
		logger.L.Error("worker: get task failed", zap.Uint("taskID", taskID), zap.Error(err))
		return
	}

	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		logger.L.Debug("worker: skip task, status not pending/running",
			zap.Uint("taskID", taskID),
			zap.Int("workerID", workerID),
			zap.String("status", task.Status),
		)
		return
	}

	if task.FileRecord == nil {
		logger.L.Error("worker: task has no file record", zap.Uint("taskID", taskID))
		return
	}

	// 若调度器已标记为 running，此处仅确保 DB 与内存一致
	now := time.Now()
	if task.Status == model.TaskStatusPending {
		task.Status = model.TaskStatusRunning
		task.StartedAt = &now
		if err := q.taskRepo.Update(task); err != nil {
			logger.L.Error("worker: update task running failed", zap.Uint("taskID", taskID), zap.Error(err))
			return
		}
	} else if task.StartedAt == nil {
		task.StartedAt = &now
		_ = q.taskRepo.Update(task)
	}

	localPath := task.FileRecord.LocalPath
	remoteName := task.RemoteName
	remotePath := task.RemotePath
	if remoteName == "" || remotePath == "" {
		logger.L.Warn("worker: task missing remote_name or remote_path, use task table",
			zap.Uint("taskID", taskID),
			zap.String("remote_name", remoteName),
			zap.String("remote_path", remotePath),
		)
	}

	logger.L.Info("worker: start upload",
		zap.Uint("taskID", taskID),
		zap.Int("workerID", workerID),
		zap.String("local_path", localPath),
		zap.String("remote_name", remoteName),
		zap.String("remote_path", remotePath),
	)

	// 通过 rclone 接口执行，支持重试
	var res rclone.Result
	uploadStart := time.Now()
	for attempt := 0; attempt <= q.maxRetry; attempt++ {
		if attempt > 0 {
			task.RetryCount = attempt
			_ = q.taskRepo.Update(task)
			sleepDur := time.Duration(attempt) * 2 * time.Second
			logger.L.Debug("worker: retry after sleep",
				zap.Uint("taskID", taskID),
				zap.Int("attempt", attempt),
				zap.Duration("sleep", sleepDur),
			)
			time.Sleep(sleepDur)
		}

		logger.L.Debug("worker: rclone copy attempt",
			zap.Uint("taskID", taskID),
			zap.Int("attempt", attempt),
			zap.Int("max_retry", q.maxRetry),
		)

		res, err = q.rclone.Copy(ctx, localPath, remoteName, remotePath, func(p rclone.Progress) {
			if q.onProgress != nil {
				q.onProgress(taskID, p.Percent, p.BytesDone, p.BytesTotal, p.Speed, p.Message)
			}
			// 可选：写入 upload_logs
			_ = q.logRepo.Create(&model.UploadLog{
				TaskID:     taskID,
				Percent:    p.Percent,
				BytesDone:  p.BytesDone,
				BytesTotal: p.BytesTotal,
				Speed:      p.Speed,
				Message:    p.Message,
			})
		})

		if err == nil && res.Success {
			logger.L.Debug("worker: rclone copy ok", zap.Uint("taskID", taskID), zap.Int("attempt", attempt))
			break
		}

		logger.L.Warn("worker: rclone copy attempt failed",
			zap.Uint("taskID", taskID),
			zap.Int("attempt", attempt),
			zap.Int("max_retry", q.maxRetry),
			zap.String("res_error", res.Error),
			zap.Error(err),
		)
	}

	// 更新任务状态与 file_record.uploaded_at
	finished := time.Now()
	duration := finished.Sub(uploadStart)
	task.FinishedAt = &finished

	if err == nil && res.Success {
		task.Status = model.TaskStatusSuccess
		task.ErrorMsg = ""
		task.FileRecord.UploadedAt = &finished
		_ = q.fileRepo.Update(task.FileRecord)
		logger.L.Info("worker: upload success",
			zap.Uint("taskID", taskID),
			zap.Int("workerID", workerID),
			zap.String("local_path", localPath),
			zap.Duration("duration", duration),
		)
	} else {
		task.Status = model.TaskStatusFailed
		if res.Error != "" {
			task.ErrorMsg = res.Error
		} else if err != nil {
			task.ErrorMsg = err.Error()
		}
		logger.L.Warn("worker: upload failed",
			zap.Uint("taskID", taskID),
			zap.Int("workerID", workerID),
			zap.String("local_path", localPath),
			zap.Duration("duration", duration),
			zap.String("error", task.ErrorMsg),
		)
	}

	if err := q.taskRepo.Update(task); err != nil {
		logger.L.Error("worker: update task final state failed", zap.Uint("taskID", taskID), zap.Error(err))
		return
	}
	logger.L.Debug("worker: processOne done", zap.Uint("taskID", taskID), zap.String("status", task.Status))
}
