package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"rclone-sync-hub/internal/database"
	"rclone-sync-hub/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func openIntegrationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.UploadTask{}, &model.FileRecord{}, &model.WatchFolder{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WatchFolder{}, &model.FileRecord{}, &model.UploadTask{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&model.UploadTask{}, &model.FileRecord{}, &model.WatchFolder{})
	})
	return db
}

func newFileRecordFixture(localPath, relativePath, remotePath string) model.FileRecord {
	return model.FileRecord{
		LocalPath:    localPath,
		RelativePath: relativePath,
		RemotePath:   remotePath,
		FileModTime:  time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestFinishSuccessCommitsTaskAndFileVersionAtomically(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/a.bin", "a.bin", "backup/a.bin")
	file.Fingerprint = "fingerprint-a"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	task := model.UploadTask{FileRecordID: file.ID, Status: model.TaskStatusRunning, LeaseOwner: "instance/worker-0", FileFingerprint: file.Fingerprint, RemoteName: file.RemoteName, RemotePath: file.RemotePath}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewTaskRepository(db)
	transitioned, marked, err := repo.FinishSuccess(context.Background(), task.ID, task.LeaseOwner, file.ID, file.Fingerprint, task.RemoteName, task.RemotePath, time.Now(), 3)
	if err != nil || !transitioned || !marked {
		t.Fatalf("transitioned=%v marked=%v err=%v", transitioned, marked, err)
	}
	var persistedTask model.UploadTask
	var persistedFile model.FileRecord
	if err := db.First(&persistedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&persistedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedTask.Status != model.TaskStatusSuccess || persistedFile.UploadedAt == nil {
		t.Fatalf("task/file not committed together: task=%+v file=%+v", persistedTask, persistedFile)
	}
}

func TestFinishSuccessDoesNotMarkSupersededFileVersion(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/b.bin", "b.bin", "backup/b.bin")
	file.Fingerprint = "new-version"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	task := model.UploadTask{FileRecordID: file.ID, Status: model.TaskStatusRunning, LeaseOwner: "instance/worker-0", FileFingerprint: "old-version", RemoteName: file.RemoteName, RemotePath: file.RemotePath}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	transitioned, marked, err := NewTaskRepository(db).FinishSuccess(context.Background(), task.ID, task.LeaseOwner, file.ID, task.FileFingerprint, task.RemoteName, task.RemotePath, time.Now(), 3)
	if err != nil || !transitioned || marked {
		t.Fatalf("transitioned=%v marked=%v err=%v", transitioned, marked, err)
	}
	var persisted model.FileRecord
	if err := db.First(&persisted, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.UploadedAt != nil {
		t.Fatal("superseded file version was marked uploaded")
	}
}

func TestFinishSuccessDoesNotMarkChangedRemoteTarget(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/target.bin", "target.bin", "new/target.bin")
	file.RemoteName = "new-remote"
	file.Fingerprint = "same-version"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	task := model.UploadTask{FileRecordID: file.ID, Status: model.TaskStatusRunning, LeaseOwner: "instance/worker-0", FileFingerprint: file.Fingerprint, RemoteName: "old-remote", RemotePath: "old/target.bin"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	transitioned, marked, err := NewTaskRepository(db).FinishSuccess(context.Background(), task.ID, task.LeaseOwner, file.ID, task.FileFingerprint, task.RemoteName, task.RemotePath, time.Now(), 3)
	if err != nil || !transitioned || marked {
		t.Fatalf("transitioned=%v marked=%v err=%v", transitioned, marked, err)
	}
	var persisted model.FileRecord
	if err := db.First(&persisted, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.UploadedAt != nil {
		t.Fatal("old remote target marked the current target uploaded")
	}
}

func TestFinishSuccessCannotOverwriteCancellationRequest(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/cancel.bin", "cancel.bin", "cancel.bin")
	file.RemoteName = "backup"
	file.Fingerprint = "cancel-version"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	canceledAt := time.Now()
	task := model.UploadTask{
		FileRecordID:      file.ID,
		Status:            model.TaskStatusRunning,
		LeaseOwner:        "instance/worker-0",
		FileFingerprint:   file.Fingerprint,
		RemoteName:        file.RemoteName,
		RemotePath:        file.RemotePath,
		CancelRequestedAt: &canceledAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	transitioned, marked, err := NewTaskRepository(db).FinishSuccess(context.Background(), task.ID, task.LeaseOwner, file.ID, task.FileFingerprint, task.RemoteName, task.RemotePath, time.Now(), 3)
	if err != nil || transitioned || marked {
		t.Fatalf("transitioned=%v marked=%v err=%v", transitioned, marked, err)
	}
	var persistedTask model.UploadTask
	var persistedFile model.FileRecord
	if err := db.First(&persistedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&persistedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedTask.Status != model.TaskStatusRunning || persistedTask.CancelRequestedAt == nil || persistedFile.UploadedAt != nil {
		t.Fatalf("cancellation was overwritten: task=%+v file=%+v", persistedTask, persistedFile)
	}
}

func TestReleaseLeaseFinalizesConcurrentCancellation(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/release.bin", "release.bin", "release.bin")
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	cancelRequestedAt := time.Now()
	task := model.UploadTask{
		FileRecordID:      file.ID,
		Status:            model.TaskStatusRunning,
		LeaseOwner:        "instance/worker-0",
		RetryCount:        1,
		CancelRequestedAt: &cancelRequestedAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewTaskRepository(db).ReleaseLease(context.Background(), task.ID, task.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusCanceled || task.CancelRequestedAt != nil || task.CanceledAt == nil {
		t.Fatalf("canceled task became unclaimable instead of finalized: %+v", task)
	}
}

func TestDeleteCannotRemoveRunningTask(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/running-delete.bin", "running-delete.bin", "running-delete.bin")
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	task := model.UploadTask{FileRecordID: file.ID, Status: model.TaskStatusRunning, LeaseOwner: "instance/worker-0"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	err := NewTaskRepository(db).Delete(context.Background(), task.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected running delete conflict, got %v", err)
	}
	if err := db.First(&task, task.ID).Error; err != nil {
		t.Fatalf("running task was deleted: %v", err)
	}
}

func TestBatchTaskCreationIsIdempotent(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/c.bin", "c.bin", "backup/c.bin")
	file.Fingerprint = "fingerprint-c"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	key := "8cbd2a1a11a38f760f49c018342665c3aa674bffe47c9b5504260a5fbdea10aa"
	makeTask := func() *model.UploadTask {
		return &model.UploadTask{FileRecordID: file.ID, Status: model.TaskStatusPending, IdempotencyKey: &key, FileFingerprint: file.Fingerprint}
	}
	repo := NewTaskRepository(db)
	created, err := repo.CreateManyIfAbsent(context.Background(), []*model.UploadTask{makeTask()}, 500)
	if err != nil || created != 1 {
		t.Fatalf("first batch created=%d err=%v", created, err)
	}
	created, err = repo.CreateManyIfAbsent(context.Background(), []*model.UploadTask{makeTask()}, 500)
	if err != nil || created != 0 {
		t.Fatalf("duplicate batch created=%d err=%v", created, err)
	}
}

func TestBatchSnapshotUpdateClearsOnlySupersededUploads(t *testing.T) {
	db := openIntegrationDatabase(t)
	uploadedAt := time.Now().Add(-time.Minute)
	first := newFileRecordFixture("/data/changed.bin", "changed.bin", "backup/changed.bin")
	first.Fingerprint = "old"
	first.UploadedAt = &uploadedAt
	second := newFileRecordFixture("/data/same.bin", "same.bin", "backup/same.bin")
	second.Fingerprint = "same"
	second.UploadedAt = &uploadedAt
	if err := db.Create(&[]*model.FileRecord{&first, &second}).Error; err != nil {
		t.Fatal(err)
	}
	first.Fingerprint = "new"
	first.FileSize = 42
	second.RelativePath = "renamed-display-value.bin"
	if err := NewFileRecordRepository(db).UpdateSnapshots(context.Background(), []*model.FileRecord{&first, &second}, []uint{first.ID}, 500); err != nil {
		t.Fatal(err)
	}
	var persisted []model.FileRecord
	if err := db.Order("id ASC").Find(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 || persisted[0].Fingerprint != "new" || persisted[0].FileSize != 42 || persisted[0].UploadedAt != nil {
		t.Fatalf("superseded snapshot not updated safely: %+v", persisted)
	}
	if persisted[1].RelativePath != second.RelativePath || persisted[1].UploadedAt == nil {
		t.Fatalf("unchanged upload marker was not preserved: %+v", persisted[1])
	}
}

func TestDeleteWatchFolderCancelsWorkAndDetachesSnapshots(t *testing.T) {
	db := openIntegrationDatabase(t)
	folder := model.WatchFolder{Name: "to-delete", LocalPath: "/data/delete", RemoteName: "backup", RemotePath: "delete", SyncType: model.WatchFolderSyncTypeLocalToRemote, Status: model.WatchFolderStatusWatching, Enabled: true}
	if err := db.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	file := newFileRecordFixture("/data/delete/file.bin", "file.bin", "delete/file.bin")
	file.WatchFolderID = folder.ID
	file.RemoteName = "backup"
	file.Fingerprint = "version"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.UploadTask{WatchFolderID: folder.ID, FileRecordID: file.ID, Status: model.TaskStatusPending}
	running := model.UploadTask{WatchFolderID: folder.ID, FileRecordID: file.ID, Status: model.TaskStatusRunning, LeaseOwner: "instance/worker-0"}
	if err := db.Create(&[]*model.UploadTask{&pending, &running}).Error; err != nil {
		t.Fatal(err)
	}

	if err := NewWatchFolderRepository(db).Delete(context.Background(), folder.ID); err != nil {
		t.Fatal(err)
	}
	var persistedFile model.FileRecord
	if err := db.First(&persistedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedFile.WatchFolderID != 0 {
		t.Fatalf("snapshot remained attached to deleted folder: %+v", persistedFile)
	}
	var tasks []model.UploadTask
	if err := db.Order("id ASC").Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].Status != model.TaskStatusCanceled || tasks[1].Status != model.TaskStatusRunning || tasks[1].CancelRequestedAt == nil {
		t.Fatalf("tasks were not safely canceled: %+v", tasks)
	}
}

func TestOrphanReconciliationMigration(t *testing.T) {
	db := openIntegrationDatabase(t)
	file := newFileRecordFixture("/data/orphan/file.bin", "file.bin", "orphan/file.bin")
	file.WatchFolderID = 999999
	file.Fingerprint = "version"
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	task := model.UploadTask{WatchFolderID: 999999, FileRecordID: file.ID, Status: model.TaskStatusPending}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	migrations := database.DefaultMigrations()
	if len(migrations) < 3 {
		t.Fatal("orphan reconciliation migration is missing")
	}
	if err := migrations[2].Up(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.WatchFolderID != 0 || task.Status != model.TaskStatusCanceled || task.CanceledAt == nil {
		t.Fatalf("orphan data was not reconciled: file=%+v task=%+v", file, task)
	}
}
