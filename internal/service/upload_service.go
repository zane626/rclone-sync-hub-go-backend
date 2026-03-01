// Package service 业务编排层：调用 repository、worker、scheduler，不写 HTTP 逻辑。
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/scheduler"
	"rclone-sync-hub/internal/worker"
)

// UploadService 上传相关业务接口。
type UploadService interface {
	// ListTasks 分页列出任务，按状态筛选；keyword 非空时对 watch_folder_name/file_name/local_path/remote_name/remote_path 模糊查询。
	ListTasks(ctx context.Context, status, keyword string, page, pageSize int) ([]model.UploadTask, int64, error)
	// GetTask 获取单条任务详情。
	GetTask(ctx context.Context, id uint) (*model.UploadTask, error)
	// CreateTask 新建上传任务。
	CreateTask(ctx context.Context, in CreateTaskInput) (*model.UploadTask, error)
	// DeleteTask 删除上传任务（running 状态不可删除）。
	DeleteTask(ctx context.Context, id uint) error
	// PauseTask 暂停上传（running 状态不可暂停）。
	PauseTask(ctx context.Context, id uint) error
	// BatchSubmitTasks 批量重试，将状态改为 pending 并重新入队。
	BatchSubmitTasks(ctx context.Context, ids []uint) TaskBatchResult
	// BatchPauseTasks 批量暂停上传。
	BatchPauseTasks(ctx context.Context, ids []uint) TaskBatchResult
	// BatchDeleteTasks 批量删除任务。
	BatchDeleteTasks(ctx context.Context, ids []uint) TaskBatchResult
	// TriggerScan 触发一次目录扫描。
	TriggerScan(ctx context.Context) (enqueued int, err error)
	// GetStats 获取各状态任务数量。
	GetStats(ctx context.Context) (pending, running, success, failed int64, err error)
	// SubmitTask 将已有任务 ID 再次入队（用于重试 failed 等）。
	SubmitTask(ctx context.Context, taskID uint) error
	// GetTaskLogs 获取指定任务的上传日志（来自 upload_logs 表），limit 为 0 时默认 500。
	GetTaskLogs(ctx context.Context, taskID uint, limit int) ([]model.UploadLog, error)
}

func (s *uploadService) GetTask(ctx context.Context, id uint) (*model.UploadTask, error) {
	return s.taskRepo.GetByID(id)
}

func (s *uploadService) TriggerScan(ctx context.Context) (int, error) {
	return s.scanner.ScanOnce(ctx)
}

func (s *uploadService) GetStats(ctx context.Context) (pending, running, success, failed int64, err error) {
	pending, err = s.taskRepo.CountByStatus(model.TaskStatusPending)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	running, err = s.taskRepo.CountByStatus(model.TaskStatusRunning)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	success, err = s.taskRepo.CountByStatus(model.TaskStatusSuccess)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	failed, err = s.taskRepo.CountByStatus(model.TaskStatusFailed)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return pending, running, success, failed, nil
}

func (s *uploadService) SubmitTask(ctx context.Context, taskID uint) error {
	// 重试：将任务状态置为 pending，并入队。
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return err
	}
	// 上传中的任务不能重试。
	if task.Status == model.TaskStatusRunning {
		return fmt.Errorf("task is running, cannot retry")
	}
	now := time.Now()
	task.Status = model.TaskStatusPending
	task.Progress = 0
	task.ErrorMsg = ""
	task.StartedAt = nil
	task.FinishedAt = nil
	task.DurationSeconds = 0
	task.LastStatusAt = &now
	task.RetryCount++
	task.AccumulatedFailures++
	if err := s.taskRepo.Update(task); err != nil {
		return err
	}
	return s.queue.Submit(ctx, taskID)
}

func (s *uploadService) GetTaskLogs(ctx context.Context, taskID uint, limit int) ([]model.UploadLog, error) {
	if limit <= 0 {
		limit = 500
	}
	return s.logRepo.ListByTaskID(taskID, limit)
}

type uploadService struct {
	taskRepo repository.TaskRepository
	fileRepo repository.FileRecordRepository
	logRepo  repository.UploadLogRepository
	scanner  scheduler.Scanner
	queue    worker.Queue
}

// NewUploadService 创建上传服务。
func NewUploadService(
	taskRepo repository.TaskRepository,
	fileRepo repository.FileRecordRepository,
	logRepo repository.UploadLogRepository,
	scanner scheduler.Scanner,
	queue worker.Queue,
) UploadService {
	return &uploadService{
		taskRepo: taskRepo,
		fileRepo: fileRepo,
		logRepo:  logRepo,
		scanner:  scanner,
		queue:    queue,
	}
}

func (s *uploadService) ListTasks(ctx context.Context, status, keyword string, page, pageSize int) ([]model.UploadTask, int64, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
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

// CreateTaskInput 新建任务入参。
type CreateTaskInput struct {
	WatchFolderID   uint
	WatchFolderName string
	FileName        string
	LocalPath       string
	RemoteName      string
	RemotePath      string
	FileSize        int64
}

func (s *uploadService) CreateTask(ctx context.Context, in CreateTaskInput) (*model.UploadTask, error) {
	if in.LocalPath == "" || in.RemoteName == "" || in.RemotePath == "" {
		return nil, fmt.Errorf("local_path, remote_name and remote_path are required")
	}

	// 确保 FileRecord 存在（如不存在则创建）。
	var fr *model.FileRecord
	exists, err := s.fileRepo.ExistsByLocalPath(in.LocalPath)
	if err != nil {
		return nil, err
	}
	if exists {
		fr, err = s.fileRepo.GetByLocalPath(in.LocalPath)
		if err != nil {
			return nil, err
		}
	} else {
		fr = &model.FileRecord{
			LocalPath:    in.LocalPath,
			RelativePath: "",
			RemotePath:   in.RemotePath,
			FileSize:     in.FileSize,
		}
		if err := s.fileRepo.Create(fr); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	fileName := in.FileName
	if fileName == "" {
		fileName = filepath.Base(in.LocalPath)
	}
	task := &model.UploadTask{
		FileRecordID:     fr.ID,
		WatchFolderID:    in.WatchFolderID,
		WatchFolderName:  in.WatchFolderName,
		FileName:         fileName,
		LocalPath:        in.LocalPath,
		RemoteName:       in.RemoteName,
		RemotePath:       in.RemotePath,
		Status:           model.TaskStatusPending,
		FileSize:         in.FileSize,
		LastStatusAt:     &now,
		Progress:         0,
		RetryCount:       0,
		AccumulatedFailures: 0,
	}
	if err := s.taskRepo.Create(task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *uploadService) DeleteTask(ctx context.Context, id uint) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}
	// 上传中的任务不可删除。
	if task.Status == model.TaskStatusRunning {
		return fmt.Errorf("task is running, cannot delete")
	}
	return s.taskRepo.Delete(id)
}

func (s *uploadService) PauseTask(ctx context.Context, id uint) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}
	// 上传中的任务不可暂停。
	if task.Status == model.TaskStatusRunning {
		return fmt.Errorf("task is running, cannot pause")
	}
	now := time.Now()
	task.Status = model.TaskStatusPaused
	task.LastStatusAt = &now
	return s.taskRepo.Update(task)
}

// TaskBatchResult 批量操作结果，用于前端展示成功 / 失败列表。
type TaskBatchResult struct {
	OKIDs  []uint            `json:"ok_ids"`
	Failed map[uint]string   `json:"failed"` // taskID -> error message
}

func (s *uploadService) BatchSubmitTasks(ctx context.Context, ids []uint) TaskBatchResult {
	res := TaskBatchResult{Failed: map[uint]string{}}
	for _, id := range ids {
		if err := s.SubmitTask(ctx, id); err != nil {
			res.Failed[id] = err.Error()
		} else {
			res.OKIDs = append(res.OKIDs, id)
		}
	}
	return res
}

func (s *uploadService) BatchPauseTasks(ctx context.Context, ids []uint) TaskBatchResult {
	res := TaskBatchResult{Failed: map[uint]string{}}
	for _, id := range ids {
		if err := s.PauseTask(ctx, id); err != nil {
			res.Failed[id] = err.Error()
		} else {
			res.OKIDs = append(res.OKIDs, id)
		}
	}
	return res
}

func (s *uploadService) BatchDeleteTasks(ctx context.Context, ids []uint) TaskBatchResult {
	res := TaskBatchResult{Failed: map[uint]string{}}
	for _, id := range ids {
		if err := s.DeleteTask(ctx, id); err != nil {
			res.Failed[id] = err.Error()
		} else {
			res.OKIDs = append(res.OKIDs, id)
		}
	}
	return res
}
