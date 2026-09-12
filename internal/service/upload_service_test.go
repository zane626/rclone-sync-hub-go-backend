package service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/repository"
)

type deleteTaskRepo struct {
	repository.TaskRepository
	delete     func(context.Context, uint) error
	deleteMany func(context.Context, []uint) (map[uint]error, error)
}

func (r *deleteTaskRepo) Delete(ctx context.Context, id uint) error {
	return r.delete(ctx, id)
}

func (r *deleteTaskRepo) DeleteMany(ctx context.Context, ids []uint) (map[uint]error, error) {
	return r.deleteMany(ctx, ids)
}

func TestDeleteTaskUsesAtomicDeleteAndMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"success", nil, 0},
		{"missing", repository.ErrNotFound, http.StatusNotFound},
		{"running", repository.ErrConflict, http.StatusConflict},
		{"canceled request", context.Canceled, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			repo := &deleteTaskRepo{delete: func(got context.Context, id uint) error {
				calls++
				if got != ctx || id != 42 {
					t.Fatal("delete did not receive the request context and ID")
				}
				return tc.err
			}}
			// Any preliminary GetByID would panic on the embedded nil repository.
			err := (&uploadService{taskRepo: repo}).DeleteTask(ctx, 42)
			if calls != 1 || !errors.Is(err, tc.err) {
				t.Fatalf("calls=%d err=%v, want %v", calls, err, tc.err)
			}
			if tc.status != 0 {
				public, ok := apperror.As(err)
				if !ok || public.Status != tc.status {
					t.Fatalf("error=%v, want HTTP %d", err, tc.status)
				}
			}
		})
	}
}

func TestBatchDeleteTasksPreservesPartialResultsAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	calls := 0
	repo := &deleteTaskRepo{deleteMany: func(got context.Context, ids []uint) (map[uint]error, error) {
		calls++
		if got != ctx || !reflect.DeepEqual(ids, []uint{8, 2, 4, 9}) {
			t.Fatalf("unexpected batch IDs: %v", ids)
		}
		return map[uint]error{2: repository.ErrConflict, 4: repository.ErrNotFound}, nil
	}}
	result := (&uploadService{taskRepo: repo}).BatchDeleteTasks(ctx, []uint{8, 2, 4, 8, 9, 2})
	if calls != 1 || !reflect.DeepEqual(result.OKIDs, []uint{8, 9}) {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	wantFailed := map[uint]string{2: "running task must be canceled before deletion", 4: "task not found"}
	if !reflect.DeepEqual(result.Failed, wantFailed) {
		t.Fatalf("failed=%v, want %v", result.Failed, wantFailed)
	}
}

func TestBatchDeleteTasksDoesNotReportSuccessAfterTransactionFailure(t *testing.T) {
	for _, failure := range []error{errors.New("private database error"), context.Canceled} {
		repo := &deleteTaskRepo{deleteMany: func(context.Context, []uint) (map[uint]error, error) {
			return nil, failure
		}}
		result := (&uploadService{taskRepo: repo}).BatchDeleteTasks(context.Background(), []uint{1, 2})
		if len(result.OKIDs) != 0 || !reflect.DeepEqual(result.Failed, map[uint]string{1: "operation failed", 2: "operation failed"}) {
			t.Fatalf("transaction failure reported incorrectly: %+v", result)
		}
	}
}
