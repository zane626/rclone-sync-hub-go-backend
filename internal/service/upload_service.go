// Package service contains application orchestration without HTTP concerns.
package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"
	"rclone-sync-hub/internal/worker"
)

type UploadService interface {
	ListTasks(ctx context.Context, status, keyword string, page, pageSize int) ([]model.UploadTask, int64, error)
	GetTask(ctx context.Context, id uint) (*model.UploadTask, error)
	CreateTask(ctx context.Context, in CreateTaskInput) (*model.UploadTask, error)
	DeleteTask(ctx context.Context, id uint) error
	PauseTask(ctx context.Context, id uint) error
	CancelTask(ctx context.Context, id uint) error
	BatchSubmitTasks(ctx context.Context, ids []uint) TaskBatchResult
	BatchPauseTasks(ctx context.Context, ids []uint) TaskBatchResult
	BatchDeleteTasks(ctx context.Context, ids []uint) TaskBatchResult
	BatchCancelTasks(ctx context.Context, ids []uint) TaskBatchResult
	TriggerScan(ctx context.Context) (enqueued int, err error)
	GetStats(ctx context.Context) (map[string]int64, error)
	SubmitTask(ctx context.Context, taskID uint) error
	GetTaskLogs(ctx context.Context, taskID uint, limit int) ([]model.UploadLog, error)
}

type uploadService struct {
	taskRepo repository.TaskRepository
	fileRepo repository.FileRecordRepository
	logRepo  repository.UploadLogRepository
	scanner  scanTrigger
	queue    worker.Queue
	policy   *security.ResourcePolicy
}

func NewUploadService(
	taskRepo repository.TaskRepository,
	fileRepo repository.FileRecordRepository,
	logRepo repository.UploadLogRepository,
	scanner scanTrigger,
	queue worker.Queue,
	policy *security.ResourcePolicy,
) UploadService {
	return &uploadService{taskRepo: taskRepo, fileRepo: fileRepo, logRepo: logRepo, scanner: scanner, queue: queue, policy: policy}
}

func (s *uploadService) GetTask(_ context.Context, id uint) (*model.UploadTask, error) {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return nil, taskLookupError(err)
	}
	return task, nil
}

func taskLookupError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperror.NotFound("task not found", err)
	}
	return err
}

func (s *uploadService) TriggerScan(ctx context.Context) (int, error) {
	return s.scanner.ScanNow(ctx)
}

type scanTrigger interface {
	ScanNow(ctx context.Context) (int, error)
}

func (s *uploadService) GetStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64, 6)
	counts, err := s.taskRepo.CountByStatuses(ctx)
	if err != nil {
		return nil, err
	}
	for _, status := range []string{
		model.TaskStatusPending,
		model.TaskStatusRunning,
		model.TaskStatusSuccess,
		model.TaskStatusFailed,
		model.TaskStatusPaused,
		model.TaskStatusCanceled,
	} {
		stats[status] = counts[status]
	}
	return stats, nil
}

func (s *uploadService) SubmitTask(ctx context.Context, taskID uint) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	switch task.Status {
	case model.TaskStatusPending:
		return s.queue.Submit(ctx, taskID)
	case model.TaskStatusRunning:
		return apperror.Conflict("running task cannot be retried", nil)
	case model.TaskStatusSuccess:
		return apperror.Conflict("successful task cannot be retried", nil)
	}
	reset, err := s.taskRepo.ResetForRetry(ctx, taskID, time.Now())
	if err != nil {
		return err
	}
	if !reset {
		return apperror.Conflict("task cannot be retried in its current state", nil)
	}
	return s.queue.Submit(ctx, taskID)
}

func (s *uploadService) GetTaskLogs(ctx context.Context, taskID uint, limit int) ([]model.UploadLog, error) {
	if _, err := s.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	return s.logRepo.ListByTaskID(taskID, limit)
}

func (s *uploadService) ListTasks(_ context.Context, status, keyword string, page, pageSize int) ([]model.UploadTask, int64, error) {
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
	list, err := s.taskRepo.List(status, keyword, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.taskRepo.CountForList(status, keyword)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

type CreateTaskInput struct {
	WatchFolderID   uint
	WatchFolderName string
	FileName        string
	LocalPath       string
	RemoteName      string
	RemotePath      string
	FileSize        int64 // ignored; the trusted size is read from the local file
}

func (s *uploadService) CreateTask(ctx context.Context, in CreateTaskInput) (*model.UploadTask, error) {
	if in.LocalPath == "" || in.RemoteName == "" || in.RemotePath == "" {
		return nil, apperror.Validation("local_path, remote_name and remote_path are required", nil)
	}
	if len(in.FileName) > 512 {
		return nil, apperror.Validation("file_name must not exceed 512 characters", nil)
	}
	localPath, err := s.policy.ValidateLocalFile(in.LocalPath)
	if err != nil {
		return nil, apperror.Validation("local path is invalid, inaccessible, or outside the allowlist", err)
	}
	remotePath, err := s.policy.ValidateRemote(in.RemoteName, in.RemotePath)
	if err != nil {
		return nil, apperror.Validation("remote destination is invalid or outside the allowlist", err)
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, apperror.Validation("local file is not accessible", err)
	}
	if !info.Mode().IsRegular() {
		return nil, apperror.Validation("local_path must be a regular file", nil)
	}
	fingerprint := uploadMetadataFingerprint(info.Size(), info.ModTime())

	var fileRecord *model.FileRecord
	fileRecord, err = s.fileRepo.GetByLocalPath(localPath)
	exists := err == nil
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	now := time.Now()
	if exists {
		clearUploaded := fileRecord.Fingerprint != "" && (fileRecord.Fingerprint != fingerprint || fileRecord.RemoteName != in.RemoteName || fileRecord.RemotePath != remotePath)
		fileRecord.WatchFolderID = in.WatchFolderID
		fileRecord.RelativePath = filepath.Base(localPath)
		fileRecord.RemoteName = in.RemoteName
		fileRecord.RemotePath = remotePath
		fileRecord.FileSize = info.Size()
		fileRecord.FileModTime = info.ModTime()
		fileRecord.Fingerprint = fingerprint
		fileRecord.LastSeenAt = &now
		if clearUploaded {
			fileRecord.UploadedAt = nil
		}
		if err := s.fileRepo.UpdateSnapshot(ctx, fileRecord, clearUploaded); err != nil {
			return nil, err
		}
	} else {
		fileRecord = &model.FileRecord{
			WatchFolderID: in.WatchFolderID,
			LocalPath:     localPath,
			RelativePath:  filepath.Base(localPath),
			RemoteName:    in.RemoteName,
			RemotePath:    remotePath,
			FileSize:      info.Size(),
			FileModTime:   info.ModTime(),
			Fingerprint:   fingerprint,
			LastSeenAt:    &now,
		}
		if err := s.fileRepo.CreateSnapshot(ctx, fileRecord); err != nil {
			return nil, err
		}
		if fileRecord.ID == 0 {
			// A concurrent request may have won the unique local_path insert.
			fileRecord, err = s.fileRepo.GetByLocalPath(localPath)
			if err != nil {
				return nil, err
			}
		}
	}

	fileName := in.FileName
	if fileName == "" {
		fileName = filepath.Base(localPath)
	}
	idempotencyKey := uploadTaskIdempotencyKey(in.WatchFolderID, localPath, fingerprint, in.RemoteName, remotePath)
	task := &model.UploadTask{
		FileRecordID:    fileRecord.ID,
		WatchFolderID:   in.WatchFolderID,
		WatchFolderName: in.WatchFolderName,
		FileName:        fileName,
		LocalPath:       localPath,
		RemoteName:      in.RemoteName,
		RemotePath:      remotePath,
		Status:          model.TaskStatusPending,
		FileSize:        info.Size(),
		FileFingerprint: fingerprint,
		IdempotencyKey:  &idempotencyKey,
		LastStatusAt:    &now,
	}
	created, err := s.taskRepo.CreateIfAbsent(ctx, task)
	if err != nil {
		return nil, err
	}
	if !created {
		return s.taskRepo.GetByIdempotencyKey(ctx, idempotencyKey)
	}
	_ = s.queue.Submit(ctx, task.ID)
	return task, nil
}

func (s *uploadService) DeleteTask(ctx context.Context, id uint) error {
	return taskDeleteError(s.taskRepo.Delete(ctx, id))
}

func taskDeleteError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperror.NotFound("task not found", err)
	}
	if errors.Is(err, repository.ErrConflict) {
		return apperror.Conflict("running task must be canceled before deletion", err)
	}
	return err
}

func (s *uploadService) PauseTask(ctx context.Context, id uint) error {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if task.Status == model.TaskStatusRunning {
		return apperror.Conflict("running task cannot be paused; cancel it instead", nil)
	}
	paused, err := s.taskRepo.PauseIfNotRunning(ctx, id, time.Now())
	if err != nil {
		return err
	}
	if !paused {
		return apperror.Conflict("task cannot be paused in its current state", nil)
	}
	return nil
}

func (s *uploadService) CancelTask(ctx context.Context, id uint) error {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if task.Status == model.TaskStatusSuccess {
		return apperror.Conflict("successful task cannot be canceled", nil)
	}
	status, err := s.taskRepo.RequestCancel(ctx, id, time.Now())
	if err != nil {
		return err
	}
	if status == model.TaskStatusCanceled {
		return nil
	}
	if status == model.TaskStatusSuccess {
		return apperror.Conflict("successful task cannot be canceled", nil)
	}
	return s.queue.Cancel(ctx, id)
}

type TaskBatchResult struct {
	OKIDs  []uint          `json:"ok_ids"`
	Failed map[uint]string `json:"failed"`
}

func (s *uploadService) BatchSubmitTasks(ctx context.Context, ids []uint) TaskBatchResult {
	return runTaskBatch(ids, func(id uint) error { return s.SubmitTask(ctx, id) })
}

func (s *uploadService) BatchPauseTasks(ctx context.Context, ids []uint) TaskBatchResult {
	return runTaskBatch(ids, func(id uint) error { return s.PauseTask(ctx, id) })
}

func (s *uploadService) BatchDeleteTasks(ctx context.Context, ids []uint) TaskBatchResult {
	// Deduplicate while preserving input order in the response.
	uniqueIDs := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	failed, err := s.taskRepo.DeleteMany(ctx, uniqueIDs)
	return runTaskBatch(uniqueIDs, func(id uint) error {
		if err != nil {
			return err
		}
		return taskDeleteError(failed[id])
	})
}

func (s *uploadService) BatchCancelTasks(ctx context.Context, ids []uint) TaskBatchResult {
	return runTaskBatch(ids, func(id uint) error { return s.CancelTask(ctx, id) })
}

func runTaskBatch(ids []uint, action func(uint) error) TaskBatchResult {
	result := TaskBatchResult{Failed: map[uint]string{}}
	for _, id := range ids {
		if err := action(id); err != nil {
			if publicError, ok := apperror.As(err); ok {
				result.Failed[id] = publicError.Message
			} else {
				result.Failed[id] = "operation failed"
			}
		} else {
			result.OKIDs = append(result.OKIDs, id)
		}
	}
	return result
}

func uploadMetadataFingerprint(size int64, modTime time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d", size, modTime.UnixNano())))
	return fmt.Sprintf("%x", sum)
}

func uploadTaskIdempotencyKey(watchFolderID uint, localPath, fingerprint, remoteName, remotePath string) string {
	payload := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", watchFolderID, filepath.Clean(localPath), fingerprint, remoteName, remotePath)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum)
}
