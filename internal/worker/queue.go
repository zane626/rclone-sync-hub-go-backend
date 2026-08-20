// Package worker implements a MySQL-leased upload worker pool.
package worker

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/observability"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"

	"go.uber.org/zap"
)

// ProgressCallback is called after a throttled progress snapshot is persisted.
type ProgressCallback func(taskID uint, percent float64, bytesDone, bytesTotal, speed int64, message string)
type StatusCallback func(taskID uint, status, message string)

// Config controls durable task claims and upload execution.
type Config struct {
	MaxConcurrent           int
	MaxRetry                int
	PollInterval            time.Duration
	LeaseDuration           time.Duration
	HeartbeatInterval       time.Duration
	TaskTimeout             time.Duration
	RetryBaseDelay          time.Duration
	RetryMaxDelay           time.Duration
	ProgressPersistInterval time.Duration
	InstanceID              string
	ResourcePolicy          *security.ResourcePolicy
	Metrics                 *observability.Metrics
}

type taskLeaseRepository interface {
	ClaimNext(ctx context.Context, owner string, leaseDuration time.Duration, maxAttempts int) (*model.UploadTask, error)
	RenewLease(ctx context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error)
	UpdateProgress(ctx context.Context, id uint, owner string, percent float64, speed int64, at time.Time) error
	FinishSuccess(ctx context.Context, id uint, owner string, fileRecordID uint, fingerprint, remoteName, remotePath string, finishedAt time.Time, durationSeconds int64) (bool, bool, error)
	FinishCanceled(ctx context.Context, id uint, owner string, finishedAt time.Time, message string) (bool, error)
	FailOrRetry(ctx context.Context, id uint, owner, message string, nextRetryAt time.Time, maxAttempts int) (string, error)
	ReleaseLease(ctx context.Context, id uint, owner string) error
	IsCancellationRequested(ctx context.Context, id uint) (bool, error)
}

type watchFolderStatsRepository interface {
	RecordUploadSuccess(ctx context.Context, id uint, fileSize int64, at time.Time) error
	RecordUploadFailure(ctx context.Context, id uint, at time.Time) error
}

// Queue keeps the historical name used by services; MySQL is now the source of truth.
// Submit only wakes a worker, while workers atomically claim tasks from the database.
type Queue interface {
	Submit(ctx context.Context, taskID uint) error
	Cancel(ctx context.Context, taskID uint) error
	Run(ctx context.Context)
}

type queue struct {
	taskRepo        taskLeaseRepository
	logRepo         repository.UploadLogRepository
	watchFolderRepo watchFolderStatsRepository
	rclone          rclone.Client
	cfg             Config
	maxAttempts     int
	wake            chan struct{}
	active          sync.Map // task ID -> context.CancelFunc
	wg              sync.WaitGroup
	onProgress      ProgressCallback
	onStatus        StatusCallback
}

type QueueOption func(*queue)

func WithProgressCallback(fn ProgressCallback) QueueOption {
	return func(q *queue) { q.onProgress = fn }
}

func WithStatusCallback(fn StatusCallback) QueueOption {
	return func(q *queue) { q.onStatus = fn }
}

func NewQueue(
	taskRepo taskLeaseRepository,
	logRepo repository.UploadLogRepository,
	watchFolderRepo watchFolderStatsRepository,
	rc rclone.Client,
	cfg Config,
	opts ...QueueOption,
) Queue {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	if cfg.MaxRetry < 0 {
		cfg.MaxRetry = 0
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 90 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 || cfg.HeartbeatInterval*2 >= cfg.LeaseDuration {
		cfg.HeartbeatInterval = cfg.LeaseDuration / 3
	}
	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = 4 * time.Hour
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 30 * time.Second
	}
	if cfg.RetryMaxDelay < cfg.RetryBaseDelay {
		cfg.RetryMaxDelay = 30 * time.Minute
	}
	if cfg.ProgressPersistInterval <= 0 {
		cfg.ProgressPersistInterval = 2 * time.Second
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = generatedInstanceID()
	}

	q := &queue{
		taskRepo:        taskRepo,
		logRepo:         logRepo,
		watchFolderRepo: watchFolderRepo,
		rclone:          rc,
		cfg:             cfg,
		maxAttempts:     cfg.MaxRetry + 1,
		wake:            make(chan struct{}, cfg.MaxConcurrent),
	}
	for _, option := range opts {
		option(q)
	}
	return q
}

// Submit is intentionally non-blocking: the durable pending row already is the queue item.
func (q *queue) Submit(ctx context.Context, _ uint) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Cancel immediately stops a task running in this process. The durable cancel flag is written by the service first.
func (q *queue) Cancel(ctx context.Context, taskID uint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cancelValue, ok := q.active.Load(taskID); ok {
		cancelValue.(context.CancelFunc)()
	}
	return q.Submit(ctx, taskID)
}

func (q *queue) Run(ctx context.Context) {
	logger.L.Info("worker_pool: start",
		zap.String("instance_id", q.cfg.InstanceID),
		zap.Int("max_concurrent", q.cfg.MaxConcurrent),
		zap.Int("max_attempts", q.maxAttempts),
		zap.Duration("lease_duration", q.cfg.LeaseDuration),
	)
	for workerID := 0; workerID < q.cfg.MaxConcurrent; workerID++ {
		q.wg.Add(1)
		go q.worker(ctx, workerID)
	}
	q.wg.Wait()
	logger.L.Info("worker_pool: stopped", zap.String("instance_id", q.cfg.InstanceID))
}

func (q *queue) worker(ctx context.Context, workerID int) {
	defer q.wg.Done()
	owner := fmt.Sprintf("%s/worker-%d", q.cfg.InstanceID, workerID)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		task, err := q.taskRepo.ClaimNext(ctx, owner, q.cfg.LeaseDuration, q.maxAttempts)
		if err != nil {
			logger.L.Warn("worker: claim task failed", zap.String("owner", owner), zap.Error(err))
			if !q.waitForWork(ctx) {
				return
			}
			continue
		}
		if task == nil {
			if !q.waitForWork(ctx) {
				return
			}
			continue
		}
		q.processClaimed(ctx, owner, task)
	}
}

func (q *queue) waitForWork(ctx context.Context) bool {
	timer := time.NewTimer(q.cfg.PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-q.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (q *queue) processClaimed(parentCtx context.Context, owner string, task *model.UploadTask) {
	started := time.Now()
	q.cfg.Metrics.WorkerStarted()
	defer q.cfg.Metrics.WorkerReleased()
	logger.L.Info("worker: task claimed",
		zap.Uint("task_id", task.ID),
		zap.String("owner", owner),
		zap.Int("attempt", task.RetryCount),
	)

	if validationErr := validateClaimedTask(task); validationErr != nil {
		q.finishFailure(parentCtx, owner, task, started, validationErr)
		return
	}
	if q.cfg.ResourcePolicy != nil {
		validatedLocalPath, validationErr := q.cfg.ResourcePolicy.ValidateLocalFile(task.FileRecord.LocalPath)
		if validationErr != nil {
			q.finishFailure(parentCtx, owner, task, started, validationErr)
			return
		}
		validatedRemotePath, validationErr := q.cfg.ResourcePolicy.ValidateRemote(task.RemoteName, task.RemotePath)
		if validationErr != nil {
			q.finishFailure(parentCtx, owner, task, started, validationErr)
			return
		}
		task.FileRecord.LocalPath = validatedLocalPath
		task.RemotePath = validatedRemotePath
	}
	if task.FileFingerprint != "" {
		info, err := os.Stat(task.FileRecord.LocalPath)
		if err != nil {
			q.finishFailure(parentCtx, owner, task, started, fmt.Errorf("stat local file: %w", err))
			return
		}
		if current := fileMetadataFingerprint(info.Size(), info.ModTime()); current != task.FileFingerprint {
			q.finishCanceled(owner, task.ID, "file version was superseded before upload", started)
			return
		}
	}

	taskCtx, cancelTask := context.WithTimeout(parentCtx, q.cfg.TaskTimeout)
	q.active.Store(task.ID, context.CancelFunc(cancelTask))
	defer q.active.Delete(task.ID)
	var leaseLost atomic.Bool
	heartbeatDone := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	leaseDeadline := started.Add(q.cfg.LeaseDuration)
	if task.LeaseExpiresAt != nil {
		leaseDeadline = *task.LeaseExpiresAt
	}
	go func() {
		defer close(heartbeatStopped)
		q.heartbeat(taskCtx, cancelTask, heartbeatDone, owner, task.ID, leaseDeadline, &leaseLost)
	}()

	var progressMu sync.Mutex
	var lastProgressAt time.Time
	res, copyErr := q.rclone.Copy(taskCtx, task.FileRecord.LocalPath, task.RemoteName, task.RemotePath, func(progress rclone.Progress) {
		now := time.Now()
		progressMu.Lock()
		defer progressMu.Unlock()
		hasProgress := progress.Percent > 0 || progress.BytesDone > 0 || progress.BytesTotal > 0 || progress.Speed > 0
		if !hasProgress || now.Sub(lastProgressAt) < q.cfg.ProgressPersistInterval {
			return
		}
		lastProgressAt = now
		persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := q.taskRepo.UpdateProgress(persistCtx, task.ID, owner, progress.Percent, progress.Speed, now); err != nil {
			logger.L.Debug("worker: persist progress failed", zap.Uint("task_id", task.ID), zap.Error(err))
			return
		}
		_ = q.logRepo.Create(&model.UploadLog{
			TaskID:     task.ID,
			Percent:    progress.Percent,
			BytesDone:  progress.BytesDone,
			BytesTotal: progress.BytesTotal,
			Speed:      progress.Speed,
			Message:    truncate(progress.Message, 2048),
		})
		if q.onProgress != nil {
			q.onProgress(task.ID, progress.Percent, progress.BytesDone, progress.BytesTotal, progress.Speed, progress.Message)
		}
	})
	taskContextErr := taskCtx.Err()
	close(heartbeatDone)
	cancelTask()
	<-heartbeatStopped

	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finalizeCancel()
	cancelRequested, cancelCheckErr := q.taskRepo.IsCancellationRequested(finalizeCtx, task.ID)
	if cancelCheckErr != nil {
		logger.L.Warn("worker: cancellation check failed", zap.Uint("task_id", task.ID), zap.Error(cancelCheckErr))
	}
	if cancelRequested {
		q.finishCanceled(owner, task.ID, "task canceled by user", started)
		return
	}
	if parentCtx.Err() != nil {
		if err := q.taskRepo.ReleaseLease(finalizeCtx, task.ID, owner); err != nil {
			logger.L.Warn("worker: release lease on shutdown failed", zap.Uint("task_id", task.ID), zap.Error(err))
		}
		q.cfg.Metrics.ObserveWorkerResult("released", time.Since(started))
		return
	}
	if leaseLost.Load() {
		logger.L.Warn("worker: lease lost; discard local result", zap.Uint("task_id", task.ID), zap.String("owner", owner))
		q.cfg.Metrics.ObserveWorkerResult("lease_lost", time.Since(started))
		return
	}
	if errors.Is(taskContextErr, context.DeadlineExceeded) {
		copyErr = fmt.Errorf("upload exceeded task timeout %s", q.cfg.TaskTimeout)
	}
	if copyErr == nil && res.Success {
		if fingerprintErr := verifyTaskFileFingerprint(task); fingerprintErr != nil {
			q.finishCanceled(owner, task.ID, fingerprintErr.Error(), started)
			return
		}
		q.finishSuccess(finalizeCtx, owner, task, started)
		return
	}
	if copyErr == nil {
		copyErr = errors.New(res.Error)
	}
	if res.Error != "" {
		copyErr = fmt.Errorf("%w: %s", copyErr, res.Error)
	}
	q.finishFailure(finalizeCtx, owner, task, started, copyErr)
}

func (q *queue) heartbeat(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, owner string, taskID uint, leaseDeadline time.Time, leaseLost *atomic.Bool) {
	ticker := time.NewTicker(q.cfg.HeartbeatInterval)
	defer ticker.Stop()
	consecutiveErrors := 0
	attemptTimeout := minDuration(q.cfg.HeartbeatInterval, 5*time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			attemptStarted := time.Now()
			heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), attemptTimeout)
			active, err := q.taskRepo.RenewLease(heartbeatCtx, taskID, owner, q.cfg.LeaseDuration)
			heartbeatCancel()
			if err != nil {
				consecutiveErrors++
				logger.L.Warn("worker: lease heartbeat failed", zap.Uint("task_id", taskID), zap.Int("consecutive_errors", consecutiveErrors), zap.Error(err))
				if renewalRetryWouldExceedLease(time.Now(), leaseDeadline, q.cfg.HeartbeatInterval, attemptTimeout) {
					leaseLost.Store(true)
					cancel()
					return
				}
				continue
			}
			consecutiveErrors = 0
			if !active {
				leaseLost.Store(true)
				cancel()
				return
			}
			leaseDeadline = attemptStarted.Add(q.cfg.LeaseDuration)
		}
	}
}

func renewalRetryWouldExceedLease(now, leaseDeadline time.Time, retryInterval, attemptTimeout time.Duration) bool {
	if leaseDeadline.IsZero() {
		return true
	}
	return !now.Add(retryInterval + attemptTimeout).Before(leaseDeadline)
}

func (q *queue) finishSuccess(ctx context.Context, owner string, task *model.UploadTask, started time.Time) {
	finished := time.Now()
	durationSeconds := int64(finished.Sub(started).Seconds())
	expectedFingerprint := task.FileFingerprint
	if expectedFingerprint == "" && task.FileRecord != nil {
		expectedFingerprint = task.FileRecord.Fingerprint
	}
	transitioned, marked, err := q.taskRepo.FinishSuccess(ctx, task.ID, owner, task.FileRecordID, expectedFingerprint, task.RemoteName, task.RemotePath, finished, durationSeconds)
	if err != nil || !transitioned {
		logger.L.Warn("worker: finish success transition failed", zap.Uint("task_id", task.ID), zap.Bool("transitioned", transitioned), zap.Error(err))
		return
	}
	if !marked {
		logger.L.Info("worker: file version or destination changed; current snapshot remains pending", zap.Uint("task_id", task.ID))
	}
	_ = q.logRepo.Create(&model.UploadLog{TaskID: task.ID, Percent: 100, Message: "rclone upload succeeded"})
	fileSize := task.FileSize
	if fileSize <= 0 && task.FileRecord != nil {
		fileSize = task.FileRecord.FileSize
	}
	if task.WatchFolderID > 0 {
		if err := q.watchFolderRepo.RecordUploadSuccess(ctx, task.WatchFolderID, fileSize, finished); err != nil {
			logger.L.Warn("worker: update watch folder success stats failed", zap.Uint("task_id", task.ID), zap.Error(err))
		}
	}
	logger.L.Info("worker: upload succeeded", zap.Uint("task_id", task.ID), zap.Duration("duration", finished.Sub(started)))
	if q.onStatus != nil {
		q.onStatus(task.ID, model.TaskStatusSuccess, "upload succeeded")
	}
	q.cfg.Metrics.ObserveWorkerResult(model.TaskStatusSuccess, finished.Sub(started))
}

func (q *queue) finishCanceled(owner string, taskID uint, message string, started time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transitioned, err := q.taskRepo.FinishCanceled(ctx, taskID, owner, time.Now(), message)
	if err != nil || !transitioned {
		logger.L.Warn("worker: finish canceled transition failed", zap.Uint("task_id", taskID), zap.Bool("transitioned", transitioned), zap.Error(err))
		return
	}
	_ = q.logRepo.Create(&model.UploadLog{TaskID: taskID, Message: message})
	if q.onStatus != nil {
		q.onStatus(taskID, model.TaskStatusCanceled, message)
	}
	q.cfg.Metrics.ObserveWorkerResult(model.TaskStatusCanceled, time.Since(started))
}

func (q *queue) finishFailure(ctx context.Context, owner string, task *model.UploadTask, started time.Time, failure error) {
	if failure == nil {
		failure = errors.New("unknown upload failure")
	}
	delay := q.retryDelay(task.RetryCount)
	nextRetryAt := time.Now().Add(delay)
	message := truncate(failure.Error(), 8192)
	status, err := q.taskRepo.FailOrRetry(ctx, task.ID, owner, message, nextRetryAt, q.maxAttempts)
	if err != nil {
		logger.L.Warn("worker: failure transition failed", zap.Uint("task_id", task.ID), zap.Error(err))
		return
	}
	_ = q.logRepo.Create(&model.UploadLog{TaskID: task.ID, Message: fmt.Sprintf("upload attempt %d failed (%s): %s", task.RetryCount, status, message)})
	if q.onStatus != nil {
		q.onStatus(task.ID, status, message)
	}
	if status == model.TaskStatusFailed && task.WatchFolderID > 0 {
		_ = q.watchFolderRepo.RecordUploadFailure(ctx, task.WatchFolderID, time.Now())
	}
	logger.L.Warn("worker: upload attempt failed",
		zap.Uint("task_id", task.ID),
		zap.Int("attempt", task.RetryCount),
		zap.String("next_status", status),
		zap.Duration("elapsed", time.Since(started)),
		zap.Error(failure),
	)
	if status == model.TaskStatusPending {
		q.cfg.Metrics.RetryScheduled()
		_ = q.Submit(ctx, task.ID)
	}
	q.cfg.Metrics.ObserveWorkerResult(status, time.Since(started))
}

func (q *queue) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := q.cfg.RetryBaseDelay
	for i := 1; i < attempt && delay < q.cfg.RetryMaxDelay; i++ {
		if delay > q.cfg.RetryMaxDelay/2 {
			delay = q.cfg.RetryMaxDelay
			break
		}
		delay *= 2
	}
	if delay > q.cfg.RetryMaxDelay {
		delay = q.cfg.RetryMaxDelay
	}
	// Add 0-20% jitter to avoid a retry stampede after a remote outage.
	jitterLimit := int64(delay / 5)
	if jitterLimit > 0 {
		delay += time.Duration(rand.Int63n(jitterLimit + 1))
	}
	return delay
}

func validateClaimedTask(task *model.UploadTask) error {
	if task.FileRecord == nil {
		return errors.New("task has no file record")
	}
	if task.FileRecord.LocalPath == "" {
		return errors.New("task local path is empty")
	}
	if task.RemoteName == "" {
		return errors.New("task remote name is empty")
	}
	if task.RemotePath == "" {
		return errors.New("task remote path is empty")
	}
	return nil
}

func fileMetadataFingerprint(size int64, modTime time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d", size, modTime.UnixNano())))
	return fmt.Sprintf("%x", sum)
}

func verifyTaskFileFingerprint(task *model.UploadTask) error {
	if task == nil || task.FileRecord == nil || task.FileFingerprint == "" {
		return nil
	}
	info, err := os.Stat(task.FileRecord.LocalPath)
	if err != nil {
		return fmt.Errorf("file became unavailable during upload; rescan required: %w", err)
	}
	if current := fileMetadataFingerprint(info.Size(), info.ModTime()); current != task.FileFingerprint {
		return errors.New("file changed during upload; rescan required")
	}
	return nil
}

func generatedInstanceID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	randomBytes := make([]byte, 6)
	if _, err := cryptorand.Read(randomBytes); err != nil {
		randomBytes = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), hex.EncodeToString(randomBytes))
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
