package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"

	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.L = zap.NewNop()
	os.Exit(m.Run())
}

func TestWatchFolderScannerCreatesOnceAndDetectsChangedVersion(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "a.txt")
	writeStableFile(t, filePath, []byte("v1"), time.Now().Add(-time.Hour))

	watchRepo := newFakeWatchRepo(root)
	fileRepo := newFakeFileRepo()
	taskRepo := &fakeTaskRepo{}
	runRepo := &fakeScanRunRepo{}
	scanner := newTestWatchScanner(watchRepo, fileRepo, taskRepo, runRepo, 0)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if created != 1 || len(taskRepo.tasks) != 1 {
		t.Fatalf("first scan created=%d tasks=%d, want 1", created, len(taskRepo.tasks))
	}

	watchRepo.makeDue()
	created, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if created != 0 || len(taskRepo.tasks) != 1 {
		t.Fatalf("unchanged scan created=%d tasks=%d, want 0/1", created, len(taskRepo.tasks))
	}

	writeStableFile(t, filePath, []byte("version-2"), time.Now().Add(-30*time.Minute))
	watchRepo.makeDue()
	created, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("changed scan: %v", err)
	}
	if created != 1 || len(taskRepo.tasks) != 2 {
		t.Fatalf("changed scan created=%d tasks=%d, want 1/2", created, len(taskRepo.tasks))
	}
	if taskRepo.tasks[0].FileFingerprint == taskRepo.tasks[1].FileFingerprint {
		t.Fatal("changed file versions must have different fingerprints")
	}
}

func TestWatchFolderScannerSkipsUnstableFileUntilLaterScan(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "copying.bin")
	if err := os.WriteFile(filePath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	watchRepo := newFakeWatchRepo(root)
	fileRepo := newFakeFileRepo()
	taskRepo := &fakeTaskRepo{}
	runRepo := &fakeScanRunRepo{}
	scanner := newTestWatchScanner(watchRepo, fileRepo, taskRepo, runRepo, time.Hour)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("unstable scan: %v", err)
	}
	if created != 0 || len(taskRepo.tasks) != 0 {
		t.Fatalf("unstable file created=%d tasks=%d, want 0", created, len(taskRepo.tasks))
	}
	if len(runRepo.runs) != 1 || runRepo.runs[0].FilesStableSkipped != 1 {
		t.Fatalf("stable skip metric not recorded: %+v", runRepo.runs)
	}

	stableAt := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filePath, stableAt, stableAt); err != nil {
		t.Fatal(err)
	}
	watchRepo.makeDue()
	created, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("stable scan: %v", err)
	}
	if created != 1 || len(taskRepo.tasks) != 1 {
		t.Fatalf("stable file created=%d tasks=%d, want 1", created, len(taskRepo.tasks))
	}
}

func TestWatchFolderScannerRoutesFileThroughRegexPipeline(t *testing.T) {
	root := t.TempDir()
	fileName := `[Zz1tai]-直播回放-[2025-11-19_21_01_27].mp4`
	filePath := filepath.Join(root, fileName)
	writeStableFile(t, filePath, []byte("video"), time.Now().Add(-time.Hour))

	watchRepo := newFakeWatchRepo(root)
	watchRepo.folders[0].RemotePath = "backup/Zz1tai"
	watchRepo.folders[0].PathPipeline = []model.UploadPathPipelineStep{{
		Type:    model.UploadPathPipelineStepRegexExtract,
		Pattern: `\[(\d{4}-\d{2})-\d{2}_`,
		Group:   1,
	}}
	taskRepo := &fakeTaskRepo{}
	scanner := newTestWatchScanner(watchRepo, newFakeFileRepo(), taskRepo, &fakeScanRunRepo{}, 0)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(taskRepo.tasks) != 1 {
		t.Fatalf("created=%d tasks=%d, want 1/1", created, len(taskRepo.tasks))
	}
	want := "backup/Zz1tai/2025-11/" + fileName
	if got := taskRepo.tasks[0].RemotePath; got != want {
		t.Fatalf("remote path=%q, want %q", got, want)
	}
}

func TestWatchFolderScannerKeepsNormalPathWhenPipelineDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	fileName := "plain.mp4"
	writeStableFile(t, filepath.Join(root, fileName), []byte("video"), time.Now().Add(-time.Hour))

	watchRepo := newFakeWatchRepo(root)
	watchRepo.folders[0].PathPipeline = []model.UploadPathPipelineStep{{
		Type: model.UploadPathPipelineStepRegexExtract, Pattern: `\[(\d{4}-\d{2})-`, Group: 1,
	}}
	taskRepo := &fakeTaskRepo{}
	scanner := newTestWatchScanner(watchRepo, newFakeFileRepo(), taskRepo, &fakeScanRunRepo{}, 0)

	if _, err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := taskRepo.tasks[0].RemotePath, "backup/"+fileName; got != want {
		t.Fatalf("remote path=%q, want %q", got, want)
	}
}

func TestFileVersionStableEventuallyAcceptsFutureMtime(t *testing.T) {
	now := time.Now()
	futureModTime := now.Add(24 * time.Hour)
	firstObserved := now
	if fileVersionStable(now, futureModTime, &firstObserved, 30*time.Second) {
		t.Fatal("newly observed future-mtime file must remain in the stability window")
	}
	later := now.Add(31 * time.Second)
	if !fileVersionStable(later, futureModTime, &firstObserved, 30*time.Second) {
		t.Fatal("unchanged future-mtime file must become stable after the observation window")
	}
}

func TestWatchFolderScannerRecoversAfterFolderError(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "temporarily-missing")
	watchRepo := newFakeWatchRepo(root)
	fileRepo := newFakeFileRepo()
	taskRepo := &fakeTaskRepo{}
	runRepo := &fakeScanRunRepo{}
	scanner := newTestWatchScanner(watchRepo, fileRepo, taskRepo, runRepo, 0)

	if _, err := scanner.ScanOnce(context.Background()); err == nil {
		t.Fatal("missing folder scan must fail")
	}
	if got := watchRepo.folders[0].Status; got != model.WatchFolderStatusError {
		t.Fatalf("status after failure=%q, want error", got)
	}
	if watchRepo.folders[0].LastScanFinishedAt == nil || watchRepo.folders[0].LastError == "" {
		t.Fatal("failure must persist finished timestamp and error")
	}

	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStableFile(t, filepath.Join(root, "new.txt"), []byte("ready"), time.Now().Add(-time.Hour))
	watchRepo.makeDue()
	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("recovery scan: %v", err)
	}
	if created != 1 {
		t.Fatalf("recovery scan created=%d, want 1", created)
	}
	if got := watchRepo.folders[0].Status; got != model.WatchFolderStatusWatching {
		t.Fatalf("status after recovery=%q, want watching", got)
	}
	if watchRepo.folders[0].LastScanSuccessAt == nil || watchRepo.folders[0].LastError != "" {
		t.Fatal("recovery must persist success timestamp and clear error")
	}
}

func TestScanNowSchedulesWithoutWalkingSynchronously(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-until-background-run")
	watchRepo := newFakeWatchRepo(root)
	runRepo := &fakeScanRunRepo{}
	scanner := newTestWatchScanner(watchRepo, newFakeFileRepo(), &fakeTaskRepo{}, runRepo, 0)

	scheduled, err := scanner.ScanNow(context.Background())
	if err != nil || scheduled != 1 {
		t.Fatalf("scheduled=%d err=%v, want 1/nil", scheduled, err)
	}
	if len(runRepo.runs) != 0 {
		t.Fatal("ScanNow must not perform the filesystem walk in the request goroutine")
	}
}

func TestWatchFolderScannerPreservesLegacyUploadedRecord(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "already-uploaded.txt")
	writeStableFile(t, filePath, []byte("old"), time.Now().Add(-time.Hour))
	uploadedAt := time.Now().Add(-time.Minute)
	fileRepo := newFakeFileRepo()
	fileRepo.records[filepath.Clean(filePath)] = &model.FileRecord{
		ID:         1,
		LocalPath:  filepath.Clean(filePath),
		UploadedAt: &uploadedAt,
	}
	fileRepo.nextID = 2
	watchRepo := newFakeWatchRepo(root)
	taskRepo := &fakeTaskRepo{}
	runRepo := &fakeScanRunRepo{}
	scanner := newTestWatchScanner(watchRepo, fileRepo, taskRepo, runRepo, 0)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("legacy scan: %v", err)
	}
	if created != 0 || len(taskRepo.tasks) != 0 {
		t.Fatalf("legacy uploaded record created=%d tasks=%d, want 0", created, len(taskRepo.tasks))
	}
	if fileRepo.records[filepath.Clean(filePath)].UploadedAt == nil {
		t.Fatal("legacy uploaded_at must be preserved while fingerprint is backfilled")
	}
}

func TestWatchFolderScannerBatchesFirstDiscovery(t *testing.T) {
	root := t.TempDir()
	stableAt := time.Now().Add(-time.Hour)
	for i := 0; i < 250; i++ {
		name := filepath.Join(root, fmt.Sprintf("file-%03d.bin", i))
		writeStableFile(t, name, []byte("payload"), stableAt)
	}

	watchRepo := newFakeWatchRepo(root)
	fileRepo := newFakeFileRepo()
	taskRepo := &fakeTaskRepo{}
	scanner := newTestWatchScanner(watchRepo, fileRepo, taskRepo, &fakeScanRunRepo{}, 0)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 250 || len(taskRepo.tasks) != 250 {
		t.Fatalf("created=%d tasks=%d, want 250", created, len(taskRepo.tasks))
	}
	if fileRepo.batchCreateCalls != 1 || fileRepo.singleCreateCalls != 0 {
		t.Fatalf("file writes batch=%d single=%d, want 1/0", fileRepo.batchCreateCalls, fileRepo.singleCreateCalls)
	}
	if taskRepo.batchCreateCalls != 1 || taskRepo.singleCreateCalls != 0 {
		t.Fatalf("task writes batch=%d single=%d, want 1/0", taskRepo.batchCreateCalls, taskRepo.singleCreateCalls)
	}

	changedAt := stableAt.Add(time.Minute)
	for i := 0; i < 250; i++ {
		name := filepath.Join(root, fmt.Sprintf("file-%03d.bin", i))
		writeStableFile(t, name, []byte("changed-payload"), changedAt)
	}
	watchRepo.makeDue()
	created, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 250 || len(taskRepo.tasks) != 500 {
		t.Fatalf("changed scan created=%d tasks=%d, want 250/500", created, len(taskRepo.tasks))
	}
	if fileRepo.batchUpdateCalls != 1 || fileRepo.singleUpdateCalls != 0 {
		t.Fatalf("changed snapshot writes batch=%d single=%d, want 1/0", fileRepo.batchUpdateCalls, fileRepo.singleUpdateCalls)
	}
}

func TestWatchFolderScannerRecoversExpiredScanLease(t *testing.T) {
	root := t.TempDir()
	writeStableFile(t, filepath.Join(root, "ready.bin"), []byte("ready"), time.Now().Add(-time.Hour))
	watchRepo := newFakeWatchRepo(root)
	future := time.Now().Add(time.Hour)
	watchRepo.folders[0].ScanLeaseOwner = "other-instance"
	watchRepo.folders[0].ScanLeaseExpiresAt = &future
	scanner := newTestWatchScanner(watchRepo, newFakeFileRepo(), &fakeTaskRepo{}, &fakeScanRunRepo{}, 0)

	created, err := scanner.ScanOnce(context.Background())
	if err != nil || created != 0 {
		t.Fatalf("active lease scan created=%d err=%v, want 0/nil", created, err)
	}

	expired := time.Now().Add(-time.Minute)
	watchRepo.folders[0].ScanLeaseExpiresAt = &expired
	created, err = scanner.ScanOnce(context.Background())
	if err != nil || created != 1 {
		t.Fatalf("expired lease recovery created=%d err=%v, want 1/nil", created, err)
	}
}

func TestWatchFolderScannerMarksDeletedFileMissing(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "deleted.bin")
	writeStableFile(t, filePath, []byte("ready"), time.Now().Add(-time.Hour))
	watchRepo := newFakeWatchRepo(root)
	fileRepo := newFakeFileRepo()
	scanner := newTestWatchScanner(watchRepo, fileRepo, &fakeTaskRepo{}, &fakeScanRunRepo{}, 0)
	if _, err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	watchRepo.makeDue()
	if _, err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	record := fileRepo.records[filepath.Clean(filePath)]
	if record.MissingAt == nil {
		t.Fatal("deleted file was not marked missing")
	}
}

func newTestWatchScanner(
	watchRepo *fakeWatchRepo,
	fileRepo *fakeFileRepo,
	taskRepo *fakeTaskRepo,
	runRepo *fakeScanRunRepo,
	stablePeriod time.Duration,
) WatchFolderScanner {
	return NewWatchFolderScanner(watchRepo, fileRepo, taskRepo, runRepo, WatchFolderScannerConfig{
		PollInterval:     time.Second,
		DefaultInterval:  time.Minute,
		FolderTimeout:    time.Minute,
		FileStablePeriod: stablePeriod,
	})
}

func writeStableFile(t *testing.T, path string, contents []byte, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

type fakeWatchRepo struct {
	folders []model.WatchFolder
}

func newFakeWatchRepo(root string) *fakeWatchRepo {
	return &fakeWatchRepo{folders: []model.WatchFolder{{
		ID:                  1,
		Name:                "test",
		LocalPath:           root,
		RemoteName:          "remote",
		RemotePath:          "backup",
		Status:              model.WatchFolderStatusWatching,
		Enabled:             true,
		ScanIntervalSeconds: 60,
	}}}
}

func (r *fakeWatchRepo) ListEnabledForScan(_ context.Context, now time.Time) ([]model.WatchFolder, error) {
	var due []model.WatchFolder
	for i := range r.folders {
		folder := r.folders[i]
		if !folder.Enabled || folder.Status == model.WatchFolderStatusStopped || folder.Status == model.WatchFolderStatusPaused {
			continue
		}
		if folder.NextScanAt == nil || !folder.NextScanAt.After(now) {
			if folder.ScanLeaseExpiresAt != nil && folder.ScanLeaseExpiresAt.After(now) {
				continue
			}
			due = append(due, folder)
		}
	}
	return due, nil
}

func (r *fakeWatchRepo) ClaimForScan(_ context.Context, id uint, owner string, _ time.Time, startedAt time.Time, leaseDuration time.Duration) (bool, error) {
	for i := range r.folders {
		folder := &r.folders[i]
		if folder.ID != id || !folder.Enabled || folder.Status == model.WatchFolderStatusStopped || folder.Status == model.WatchFolderStatusPaused {
			continue
		}
		if folder.NextScanAt != nil && folder.NextScanAt.After(startedAt) {
			return false, nil
		}
		if folder.ScanLeaseExpiresAt != nil && folder.ScanLeaseExpiresAt.After(startedAt) {
			return false, nil
		}
		expiresAt := startedAt.Add(leaseDuration)
		folder.Status = model.WatchFolderStatusDetecting
		folder.LastError = ""
		folder.LastScanAt = &startedAt
		folder.LastScanStartedAt = &startedAt
		folder.ScanLeaseOwner = owner
		folder.ScanLeaseExpiresAt = &expiresAt
		return true, nil
	}
	return false, nil
}

func (r *fakeWatchRepo) RenewScanLease(_ context.Context, id uint, owner string, leaseDuration time.Duration) (bool, error) {
	for i := range r.folders {
		folder := &r.folders[i]
		if folder.ID == id && folder.Status == model.WatchFolderStatusDetecting && folder.ScanLeaseOwner == owner {
			expiresAt := time.Now().Add(leaseDuration)
			folder.ScanLeaseExpiresAt = &expiresAt
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeWatchRepo) FinishScan(_ context.Context, id uint, owner string, updates map[string]interface{}) error {
	for i := range r.folders {
		if r.folders[i].ID != id {
			continue
		}
		f := &r.folders[i]
		if f.ScanLeaseOwner != owner {
			return errors.New("scan lease was lost")
		}
		if value, ok := updates["status"].(string); ok {
			if f.Enabled && f.Status != model.WatchFolderStatusStopped && f.Status != model.WatchFolderStatusPaused {
				f.Status = value
			}
		}
		if value, ok := updates["last_error"].(string); ok {
			f.LastError = value
		}
		if value, ok := updates["last_scan_at"].(time.Time); ok {
			f.LastScanAt = &value
		}
		if value, ok := updates["last_scan_started_at"].(time.Time); ok {
			f.LastScanStartedAt = &value
		}
		if value, ok := updates["last_scan_finished_at"].(time.Time); ok {
			f.LastScanFinishedAt = &value
		}
		if value, ok := updates["last_scan_success_at"].(time.Time); ok {
			f.LastScanSuccessAt = &value
		}
		if value, ok := updates["last_scan_duration_ms"].(int64); ok {
			f.LastScanDurationMs = value
		}
		if value, present := updates["next_scan_at"]; present {
			if value == nil {
				f.NextScanAt = nil
			} else if at, ok := value.(time.Time); ok {
				f.NextScanAt = &at
			}
		}
		if value, ok := updates["scan_lease_owner"].(string); ok {
			f.ScanLeaseOwner = value
		}
		if value, present := updates["scan_lease_expires_at"]; present && value == nil {
			f.ScanLeaseExpiresAt = nil
		}
		if value, ok := updates["total_file_count"].(int64); ok {
			f.TotalFileCount = value
		}
		if value, ok := updates["total_file_size"].(int64); ok {
			f.TotalFileSize = value
		}
		return nil
	}
	return errors.New("watch folder not found")
}

func (r *fakeWatchRepo) makeDue() {
	r.folders[0].NextScanAt = nil
}

func (r *fakeWatchRepo) ScheduleAllEnabledNow(context.Context) (int64, error) {
	r.makeDue()
	return int64(len(r.folders)), nil
}

type fakeFileRepo struct {
	records           map[string]*model.FileRecord
	nextID            uint
	singleCreateCalls int
	batchCreateCalls  int
	singleUpdateCalls int
	batchUpdateCalls  int
}

func newFakeFileRepo() *fakeFileRepo {
	return &fakeFileRepo{records: make(map[string]*model.FileRecord), nextID: 1}
}

func (r *fakeFileRepo) CreateSnapshot(_ context.Context, record *model.FileRecord) error {
	r.singleCreateCalls++
	return r.insertSnapshot(record)
}

func (r *fakeFileRepo) insertSnapshot(record *model.FileRecord) error {
	key := filepath.Clean(record.LocalPath)
	if _, exists := r.records[key]; exists {
		return errors.New("duplicate file record")
	}
	record.ID = r.nextID
	r.nextID++
	copyRecord := *record
	r.records[key] = &copyRecord
	return nil
}

func (r *fakeFileRepo) CreateSnapshots(ctx context.Context, records []*model.FileRecord, _ int) error {
	r.batchCreateCalls++
	for _, record := range records {
		if err := r.insertSnapshot(record); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeFileRepo) ListForWatchFolder(_ context.Context, _ uint, root string) ([]model.FileRecord, error) {
	var list []model.FileRecord
	prefix := filepath.Clean(root) + string(filepath.Separator)
	for path, record := range r.records {
		if path == filepath.Clean(root) || len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			list = append(list, *record)
		}
	}
	return list, nil
}

func (r *fakeFileRepo) UpdateSnapshot(_ context.Context, record *model.FileRecord, clearUploaded bool) error {
	r.singleUpdateCalls++
	return r.updateSnapshot(record, clearUploaded)
}

func (r *fakeFileRepo) updateSnapshot(record *model.FileRecord, clearUploaded bool) error {
	stored, ok := r.records[filepath.Clean(record.LocalPath)]
	if !ok {
		return errors.New("file record not found")
	}
	uploadedAt := stored.UploadedAt
	*stored = *record
	if !clearUploaded {
		stored.UploadedAt = uploadedAt
	}
	stored.MissingAt = nil
	return nil
}

func (r *fakeFileRepo) UpdateSnapshots(_ context.Context, records []*model.FileRecord, clearUploadedIDs []uint, _ int) error {
	if len(records) == 0 {
		return nil
	}
	r.batchUpdateCalls++
	clearSet := make(map[uint]struct{}, len(clearUploadedIDs))
	for _, id := range clearUploadedIDs {
		clearSet[id] = struct{}{}
	}
	for _, record := range records {
		_, clear := clearSet[record.ID]
		if err := r.updateSnapshot(record, clear); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeFileRepo) MarkMissing(_ context.Context, ids []uint, at time.Time, _ int) error {
	for _, id := range ids {
		for _, record := range r.records {
			if record.ID == id && record.MissingAt == nil {
				record.MissingAt = &at
			}
		}
	}
	return nil
}

type fakeTaskRepo struct {
	tasks             []model.UploadTask
	nextID            uint
	singleCreateCalls int
	batchCreateCalls  int
}

func (r *fakeTaskRepo) ListOpenByWatchFolder(_ context.Context, watchFolderID uint) ([]model.UploadTask, error) {
	var list []model.UploadTask
	for i := range r.tasks {
		if r.tasks[i].WatchFolderID == watchFolderID && r.tasks[i].Status != model.TaskStatusSuccess {
			list = append(list, r.tasks[i])
		}
	}
	return list, nil
}

func (r *fakeTaskRepo) CreateIfAbsent(_ context.Context, task *model.UploadTask) (bool, error) {
	r.singleCreateCalls++
	return r.insertIfAbsent(task)
}

func (r *fakeTaskRepo) insertIfAbsent(task *model.UploadTask) (bool, error) {
	for i := range r.tasks {
		if task.IdempotencyKey != nil && r.tasks[i].IdempotencyKey != nil && *task.IdempotencyKey == *r.tasks[i].IdempotencyKey {
			return false, nil
		}
	}
	r.nextID++
	task.ID = r.nextID
	r.tasks = append(r.tasks, *task)
	return true, nil
}

func (r *fakeTaskRepo) CreateManyIfAbsent(ctx context.Context, tasks []*model.UploadTask, _ int) (int64, error) {
	r.batchCreateCalls++
	var created int64
	for _, task := range tasks {
		inserted, err := r.insertIfAbsent(task)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	return created, nil
}

type fakeScanRunRepo struct {
	runs   []model.ScanRun
	nextID uint64
}

func (r *fakeScanRunRepo) Create(_ context.Context, run *model.ScanRun) error {
	r.nextID++
	run.ID = r.nextID
	r.runs = append(r.runs, *run)
	return nil
}

func (r *fakeScanRunRepo) Finish(_ context.Context, run *model.ScanRun) error {
	for i := range r.runs {
		if r.runs[i].ID == run.ID {
			r.runs[i] = *run
			return nil
		}
	}
	return errors.New("scan run not found")
}
