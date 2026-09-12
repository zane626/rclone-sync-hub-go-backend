package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

func createDeletionTasks(t *testing.T, db *gorm.DB, count int) []model.UploadTask {
	t.Helper()
	file := newFileRecordFixture("/data/delete.bin", "delete.bin", "delete.bin")
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	statuses := []string{model.TaskStatusPending, model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusPaused, model.TaskStatusCanceled}
	tasks := make([]model.UploadTask, count)
	for i := range tasks {
		tasks[i] = model.UploadTask{FileRecordID: file.ID, Status: statuses[i%len(statuses)]}
	}
	if err := db.CreateInBatches(&tasks, 100).Error; err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestDeleteManyUsesBulkQueriesAndPreservesRunningTasks(t *testing.T) {
	db := openIntegrationDatabase(t)
	tasks := createDeletionTasks(t, db, 998)
	running := tasks[0]
	if err := db.Model(&running).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	ids := make([]uint, 0, 1000)
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	missingID := tasks[len(tasks)-1].ID + 1
	ids = append(ids, missingID, tasks[1].ID)
	queries, deletes := 0, 0
	if err := db.Callback().Query().After("gorm:query").Register("test:count_delete_queries", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:count_delete_queries")
	if err := db.Callback().Delete().After("gorm:delete").Register("test:count_deletes", func(*gorm.DB) { deletes++ }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Delete().Remove("test:count_deletes")

	failed, err := NewTaskRepository(db).DeleteMany(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if queries != 1 || deletes != 1 {
		t.Fatalf("1000 IDs required %d queries and %d deletes, want 1 each", queries, deletes)
	}
	if len(failed) != 2 || !errors.Is(failed[running.ID], ErrConflict) || !errors.Is(failed[missingID], ErrNotFound) {
		t.Fatalf("unexpected rejections: %v", failed)
	}
	var remaining []model.UploadTask
	if err := db.Find(&remaining).Error; err != nil || len(remaining) != 1 || remaining[0].ID != running.ID {
		t.Fatalf("remaining tasks=%v err=%v", remaining, err)
	}
	var files int64
	if err := db.Model(&model.FileRecord{}).Count(&files).Error; err != nil || files != 1 {
		t.Fatalf("file snapshot was removed: count=%d err=%v", files, err)
	}
}

func TestDeleteManyRollsBackOnFailure(t *testing.T) {
	db := openIntegrationDatabase(t)
	tasks := createDeletionTasks(t, db, 2)
	injected := errors.New("failure after DELETE but before COMMIT")
	if err := db.Callback().Delete().After("gorm:delete").Register("test:fail_delete", func(tx *gorm.DB) {
		tx.AddError(injected)
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Delete().Remove("test:fail_delete")
	failed, err := NewTaskRepository(db).DeleteMany(context.Background(), []uint{tasks[0].ID, tasks[1].ID})
	if !errors.Is(err, injected) || failed != nil {
		t.Fatalf("failed=%v err=%v, want transaction error", failed, err)
	}
	var remaining int64
	if err := db.Model(&model.UploadTask{}).Count(&remaining).Error; err != nil || remaining != 2 {
		t.Fatalf("rollback left %d tasks: %v", remaining, err)
	}
}

func TestDeleteManySeesConcurrentRunningTransition(t *testing.T) {
	db := openIntegrationDatabase(t)
	tasks := createDeletionTasks(t, db, 1)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Model(&model.UploadTask{}).Where("id = ?", tasks[0].ID).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	queryStarted := make(chan struct{}, 1)
	if err := db.Callback().Query().Before("gorm:query").Register("test:delete_query_started", func(*gorm.DB) {
		select {
		case queryStarted <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:delete_query_started")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		failed map[uint]error
		err    error
	}
	done := make(chan result, 1)
	go func() {
		failed, err := NewTaskRepository(db).DeleteMany(ctx, []uint{tasks[0].ID})
		done <- result{failed, err}
	}()
	select {
	case <-queryStarted:
	case <-ctx.Done():
		t.Fatal("delete did not start")
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || !errors.Is(got.failed[tasks[0].ID], ErrConflict) {
		t.Fatalf("concurrent claim was not protected: %+v", got)
	}
	var remaining model.UploadTask
	if err := db.First(&remaining, tasks[0].ID).Error; err != nil || remaining.Status != model.TaskStatusRunning {
		t.Fatalf("claimed task was removed: task=%+v err=%v", remaining, err)
	}
}
