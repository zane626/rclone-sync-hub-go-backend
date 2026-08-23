package api

import (
	"context"
	"testing"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/service"
)

type blockingRemoteMoveRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRemoteMoveRunner) MoveFilesWithProgress(ctx context.Context, _ uint, sourcePaths []string, targetFolder string, onProgress func(service.RemoteMoveProgress)) (service.RemoteBatchMoveResult, error) {
	result := service.RemoteBatchMoveResult{Moved: []service.RemoteFileMoveResult{}, Failed: []service.RemoteFileMoveFailure{}}
	onProgress(service.RemoteMoveProgress{Phase: "preparing", Total: len(sourcePaths), Result: result})
	close(r.started)
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-r.release:
	}
	for index, sourcePath := range sourcePaths {
		destinationPath := targetFolder + "/file"
		result.Moved = append(result.Moved, service.RemoteFileMoveResult{SourcePath: sourcePath, DestinationPath: destinationPath})
		onProgress(service.RemoteMoveProgress{
			Phase: "moving", Total: len(sourcePaths), Processed: index + 1, CurrentPath: sourcePath, Result: result,
		})
	}
	result.RefreshScheduled = true
	return result, nil
}

func TestRemoteMoveOperationRunsInBackgroundAndReportsProgress(t *testing.T) {
	runner := &blockingRemoteMoveRunner{started: make(chan struct{}), release: make(chan struct{})}
	manager := newRemoteMoveOperationManager(runner)
	operation, err := manager.Start(9, []string{"source/one", "source/two"}, "target")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("background move did not start")
	}
	if _, err := manager.Start(9, []string{"source/other"}, "target"); err == nil {
		t.Fatal("expected a second active move on the same route to be rejected")
	} else if publicError, ok := apperror.As(err); !ok || publicError.Status != 409 {
		t.Fatalf("expected conflict for concurrent route move, got %v", err)
	}
	running, err := manager.Get(9, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != remoteMoveStatusRunning || running.Total != 2 {
		t.Fatalf("unexpected running operation: %+v", running)
	}
	close(runner.release)

	deadline := time.Now().Add(time.Second)
	for {
		completed, getErr := manager.Get(9, operation.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if completed.Status == remoteMoveStatusCompleted {
			if completed.Processed != 2 || completed.Percent != 100 || len(completed.Moved) != 2 || !completed.RefreshScheduled {
				t.Fatalf("unexpected completed operation: %+v", completed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("move operation did not complete: %+v", completed)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
