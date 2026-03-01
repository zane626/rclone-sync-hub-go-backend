package scheduler

import (
	"context"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/worker"

	"go.uber.org/zap"
)

// TaskScheduler 从数据库中选取 pending 任务，根据并发限制调度到 worker。
type TaskScheduler struct {
	taskRepo      repository.TaskRepository
	queue         worker.Queue
	maxConcurrent int
	pollInterval  time.Duration
}

// NewTaskScheduler 创建调度器。
func NewTaskScheduler(
	taskRepo repository.TaskRepository,
	queue worker.Queue,
	maxConcurrent int,
	pollInterval time.Duration,
) *TaskScheduler {
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &TaskScheduler{
		taskRepo:      taskRepo,
		queue:         queue,
		maxConcurrent: maxConcurrent,
		pollInterval:  pollInterval,
	}
}

// Run 启动调度循环，使用 time.Ticker 控制轮询间隔。
func (s *TaskScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	logger.L.Info("task_scheduler: start",
		zap.Int("max_concurrent", s.maxConcurrent),
		zap.Duration("interval", s.pollInterval),
	)

	for {
		select {
		case <-ctx.Done():
			logger.L.Info("task_scheduler: stop")
			return
		case <-ticker.C:
			if err := s.dispatchOnce(ctx); err != nil {
				logger.L.Warn("task_scheduler: dispatchOnce failed", zap.Error(err))
			}
		}
	}
}

// dispatchOnce 执行一次调度：查看 pending 任务，与当前 running 数比较，必要时创建新任务执行。
func (s *TaskScheduler) dispatchOnce(ctx context.Context) error {
	// 1. 统计当前 running 数量（从数据库看状态，避免跨进程不一致）
	runningCount, err := s.taskRepo.CountByStatus(model.TaskStatusRunning)
	if err != nil {
		return err
	}
	slots := s.maxConcurrent - int(runningCount)
	if slots <= 0 {
		return nil
	}

	// 2. 取出部分 pending 任务的 ID
	ids, err := s.taskRepo.ListPendingIDs(slots)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	// 3. 对每个任务使用事务 + 状态条件抢占
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ok, err := s.taskRepo.MarkRunningIfPending(ctx, id)
		if err != nil {
			logger.L.Warn("task_scheduler: mark running failed", zap.Uint("taskID", id), zap.Error(err))
			continue
		}
		if !ok {
			// 可能被其他调度器/进程抢走
			continue
		}

		// 4. 抢占成功后，将任务提交给 worker pool（通过 queue）
		if err := s.queue.Submit(ctx, id); err != nil {
			logger.L.Warn("task_scheduler: queue submit failed", zap.Uint("taskID", id), zap.Error(err))
			// 这里可以根据需要，将任务状态改回 pending（可选）
		}
	}

	return nil
}

