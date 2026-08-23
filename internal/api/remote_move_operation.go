package api

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/service"

	"go.uber.org/zap"
)

const (
	maxRemoteMoveOperationBatch = 100

	remoteMoveStatusQueued    = "queued"
	remoteMoveStatusRunning   = "running"
	remoteMoveStatusCompleted = "completed"
	remoteMoveStatusFailed    = "failed"
)

// RemoteMoveOperation is the pollable state of one asynchronous batch move.
type RemoteMoveOperation struct {
	ID               string                          `json:"id"`
	RouteID          uint                            `json:"route_id"`
	Status           string                          `json:"status"`
	Phase            string                          `json:"phase"`
	Total            int                             `json:"total"`
	Processed        int                             `json:"processed"`
	Percent          int                             `json:"percent"`
	CurrentPath      string                          `json:"current_path,omitempty"`
	Moved            []service.RemoteFileMoveResult  `json:"moved"`
	Failed           []service.RemoteFileMoveFailure `json:"failed"`
	RefreshScheduled bool                            `json:"refresh_scheduled"`
	Error            string                          `json:"error,omitempty"`
	CreatedAt        time.Time                       `json:"created_at"`
	StartedAt        *time.Time                      `json:"started_at,omitempty"`
	FinishedAt       *time.Time                      `json:"finished_at,omitempty"`
}

type remoteMoveRunner interface {
	MoveFilesWithProgress(context.Context, uint, []string, string, func(service.RemoteMoveProgress)) (service.RemoteBatchMoveResult, error)
}

type remoteMoveOperationManager struct {
	runner           remoteMoveRunner
	operationTimeout time.Duration
	retention        time.Duration

	mu            sync.RWMutex
	operations    map[string]*RemoteMoveOperation
	activeByRoute map[uint]string
}

func newRemoteMoveOperationManager(runner remoteMoveRunner) *remoteMoveOperationManager {
	return &remoteMoveOperationManager{
		runner:           runner,
		operationTimeout: 6 * time.Hour,
		retention:        time.Hour,
		operations:       make(map[string]*RemoteMoveOperation),
		activeByRoute:    make(map[uint]string),
	}
}

func newRemoteMoveOperationID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := cryptorand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (m *remoteMoveOperationManager) Start(routeID uint, sourcePaths []string, targetFolder string) (RemoteMoveOperation, error) {
	if len(sourcePaths) == 0 || len(sourcePaths) > maxRemoteMoveOperationBatch {
		return RemoteMoveOperation{}, apperror.Validation("source_paths must contain between 1 and 100 files", nil)
	}
	operationID, err := newRemoteMoveOperationID()
	if err != nil {
		return RemoteMoveOperation{}, err
	}
	now := time.Now()
	m.mu.Lock()
	m.cleanupLocked(now)
	if activeID := m.activeByRoute[routeID]; activeID != "" {
		m.mu.Unlock()
		return RemoteMoveOperation{}, apperror.Conflict("this remote route already has a file move operation running", nil)
	}
	operation := &RemoteMoveOperation{
		ID: operationID, RouteID: routeID, Status: remoteMoveStatusQueued, Phase: "queued", Total: len(sourcePaths),
		Moved: []service.RemoteFileMoveResult{}, Failed: []service.RemoteFileMoveFailure{}, CreatedAt: now,
	}
	m.operations[operationID] = operation
	m.activeByRoute[routeID] = operationID
	snapshot := cloneRemoteMoveOperation(operation)
	m.mu.Unlock()

	paths := append([]string(nil), sourcePaths...)
	go m.run(operationID, routeID, paths, targetFolder)
	return snapshot, nil
}

func (m *remoteMoveOperationManager) Get(routeID uint, operationID string) (RemoteMoveOperation, error) {
	now := time.Now()
	m.mu.Lock()
	m.cleanupLocked(now)
	operation, ok := m.operations[operationID]
	if !ok || operation.RouteID != routeID {
		m.mu.Unlock()
		return RemoteMoveOperation{}, apperror.NotFound("file move operation not found", nil)
	}
	snapshot := cloneRemoteMoveOperation(operation)
	m.mu.Unlock()
	return snapshot, nil
}

func (m *remoteMoveOperationManager) run(operationID string, routeID uint, sourcePaths []string, targetFolder string) {
	startedAt := time.Now()
	m.mu.Lock()
	if operation := m.operations[operationID]; operation != nil {
		operation.Status = remoteMoveStatusRunning
		operation.Phase = "preparing"
		operation.StartedAt = &startedAt
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), m.operationTimeout)
	defer cancel()
	result, moveErr := m.runner.MoveFilesWithProgress(ctx, routeID, sourcePaths, targetFolder, func(progress service.RemoteMoveProgress) {
		m.updateProgress(operationID, progress)
	})

	finishedAt := time.Now()
	m.mu.Lock()
	operation := m.operations[operationID]
	if operation != nil {
		operation.Moved = append([]service.RemoteFileMoveResult(nil), result.Moved...)
		operation.Failed = append([]service.RemoteFileMoveFailure(nil), result.Failed...)
		operation.Processed = len(operation.Moved) + len(operation.Failed)
		operation.RefreshScheduled = result.RefreshScheduled
		operation.CurrentPath = ""
		operation.FinishedAt = &finishedAt
		if moveErr != nil {
			operation.Status = remoteMoveStatusFailed
			operation.Phase = "failed"
			operation.Error = publicRemoteMoveError(moveErr)
		} else {
			operation.Status = remoteMoveStatusCompleted
			operation.Phase = "completed"
			operation.Percent = 100
		}
	}
	if m.activeByRoute[routeID] == operationID {
		delete(m.activeByRoute, routeID)
	}
	m.mu.Unlock()

	if moveErr != nil {
		logger.L.Error("remote file move operation failed", zap.String("operation_id", operationID), zap.Uint("remote_route_id", routeID), zap.Error(moveErr))
	}
}

func (m *remoteMoveOperationManager) updateProgress(operationID string, progress service.RemoteMoveProgress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	operation := m.operations[operationID]
	if operation == nil {
		return
	}
	operation.Phase = progress.Phase
	operation.Total = progress.Total
	operation.Processed = progress.Processed
	operation.CurrentPath = progress.CurrentPath
	operation.Moved = append([]service.RemoteFileMoveResult(nil), progress.Result.Moved...)
	operation.Failed = append([]service.RemoteFileMoveFailure(nil), progress.Result.Failed...)
	operation.RefreshScheduled = progress.Result.RefreshScheduled
	operation.Percent = remoteMovePercent(progress.Processed, progress.Total)
}

func (m *remoteMoveOperationManager) cleanupLocked(now time.Time) {
	for operationID, operation := range m.operations {
		if operation.FinishedAt != nil && now.Sub(*operation.FinishedAt) > m.retention {
			delete(m.operations, operationID)
		}
	}
}

func remoteMovePercent(processed, total int) int {
	if total <= 0 {
		return 0
	}
	percent := processed * 100 / total
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func publicRemoteMoveError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "远端文件移动超过 6 小时后台执行时限"
	}
	if publicError, ok := apperror.As(err); ok {
		return publicError.Message
	}
	return "远端文件移动失败，请查看服务日志"
}

func cloneRemoteMoveOperation(operation *RemoteMoveOperation) RemoteMoveOperation {
	clone := *operation
	clone.Moved = append([]service.RemoteFileMoveResult(nil), operation.Moved...)
	clone.Failed = append([]service.RemoteFileMoveFailure(nil), operation.Failed...)
	if operation.StartedAt != nil {
		value := *operation.StartedAt
		clone.StartedAt = &value
	}
	if operation.FinishedAt != nil {
		value := *operation.FinishedAt
		clone.FinishedAt = &value
	}
	return clone
}
