package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"

	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.L = zap.NewNop()
	os.Exit(m.Run())
}

func TestProcessClaimedSuccessUsesConditionalSnapshotMark(t *testing.T) {
	task := testClaimedTask(t)
	taskRepo := &fakeLeaseRepo{}
	watchRepo := &fakeWatchStatsRepo{}
	logRepo := &fakeUploadLogRepo{}
	rcloneClient := &fakeRcloneClient{result: rclone.Result{Success: true}}
	q := newTestQueue(taskRepo, logRepo, watchRepo, rcloneClient)

	q.processClaimed(context.Background(), "instance/worker-0", task)

	if taskRepo.successes != 1 {
		t.Fatalf("success transitions=%d, want 1", taskRepo.successes)
	}
	if taskRepo.markedFingerprint != task.FileFingerprint {
		t.Fatalf("marked fingerprint=%q, want %q", taskRepo.markedFingerprint, task.FileFingerprint)
	}
	if watchRepo.successes != 1 || watchRepo.failures != 0 {
		t.Fatalf("watch stats success=%d failure=%d", watchRepo.successes, watchRepo.failures)
	}
}

func TestProcessClaimedFailureMovesToPersistentRetry(t *testing.T) {
	task := testClaimedTask(t)
	task.RetryCount = 1
	taskRepo := &fakeLeaseRepo{failureStatus: model.TaskStatusPending}
	q := newTestQueue(taskRepo, &fakeUploadLogRepo{}, &fakeWatchStatsRepo{}, &fakeRcloneClient{
		result: rclone.Result{Success: false, Error: "remote unavailable"},
		err:    errors.New("exit status 1"),
	})

	q.processClaimed(context.Background(), "instance/worker-0", task)

	if taskRepo.failures != 1 || taskRepo.lastFailureStatus != model.TaskStatusPending {
		t.Fatalf("failure transitions=%d status=%q", taskRepo.failures, taskRepo.lastFailureStatus)
	}
	if !taskRepo.nextRetryAt.After(time.Now()) {
		t.Fatalf("next retry was not scheduled in the future: %v", taskRepo.nextRetryAt)
	}
}

func TestProcessClaimedDoesNotMarkChangedFileUploaded(t *testing.T) {
	task := testClaimedTask(t)
	taskRepo := &fakeLeaseRepo{}
	rcloneClient := &fakeRcloneClient{
		result: rclone.Result{Success: true},
		afterCopy: func() {
			if err := os.WriteFile(task.FileRecord.LocalPath, []byte("changed while uploading"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	q := newTestQueue(taskRepo, &fakeUploadLogRepo{}, &fakeWatchStatsRepo{}, rcloneClient)

	q.processClaimed(context.Background(), "instance/worker-0", task)

	if taskRepo.successes != 0 || taskRepo.canceled != 1 {
		t.Fatalf("success transitions=%d canceled transitions=%d, want 0/1", taskRepo.successes, taskRepo.canceled)
	}
}

func TestCancelStopsLocalRcloneAndFinalizesCanceled(t *testing.T) {
	task := testClaimedTask(t)
	taskRepo := &fakeLeaseRepo{cancelRequested: true}
	rcloneClient := &fakeRcloneClient{started: make(chan struct{}), blockUntilCanceled: true}
	q := newTestQueue(taskRepo, &fakeUploadLogRepo{}, &fakeWatchStatsRepo{}, rcloneClient)
	done := make(chan struct{})
	go func() {
		q.processClaimed(context.Background(), "instance/worker-0", task)
		close(done)
	}()

	select {
	case <-rcloneClient.started:
	case <-time.After(time.Second):
		t.Fatal("rclone did not start")
	}
	if err := q.Cancel(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled task did not stop")
	}
	if taskRepo.canceled != 1 {
		t.Fatalf("canceled transitions=%d, want 1", taskRepo.canceled)
	}
}

func TestRenewalRetryMustFinishBeforeLeaseDeadline(t *testing.T) {
	now := time.Now()
	if !renewalRetryWouldExceedLease(now, now.Add(25*time.Second), 20*time.Second, 5*time.Second) {
		t.Fatal("retry ending exactly at the lease deadline must be rejected")
	}
	if renewalRetryWouldExceedLease(now, now.Add(26*time.Second), 20*time.Second, 5*time.Second) {
		t.Fatal("retry with time remaining before the lease deadline should be allowed")
	}
	if !renewalRetryWouldExceedLease(now, time.Time{}, time.Second, time.Second) {
		t.Fatal("missing lease deadline must fail closed")
	}
}

func newTestQueue(
	taskRepo *fakeLeaseRepo,
	logRepo *fakeUploadLogRepo,
	watchRepo *fakeWatchStatsRepo,
	rcloneClient *fakeRcloneClient,
) *queue {
	return NewQueue(taskRepo, logRepo, watchRepo, rcloneClient, Config{
		MaxConcurrent:           1,
		MaxRetry:                2,
		PollInterval:            10 * time.Millisecond,
		LeaseDuration:           time.Minute,
		HeartbeatInterval:       10 * time.Second,
		TaskTimeout:             time.Minute,
		RetryBaseDelay:          time.Second,
		RetryMaxDelay:           time.Minute,
		ProgressPersistInterval: time.Millisecond,
		InstanceID:              "test",
	}).(*queue)
}

func testClaimedTask(t *testing.T) *model.UploadTask {
	t.Helper()
	root := t.TempDir()
	localPath := filepath.Join(root, "file.bin")
	contents := []byte("payload")
	if err := os.WriteFile(localPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	stableAt := time.Now().Add(-time.Hour)
	if err := os.Chtimes(localPath, stableAt, stableAt); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := fileMetadataFingerprint(info.Size(), info.ModTime())
	return &model.UploadTask{
		ID:              1,
		FileRecordID:    1,
		WatchFolderID:   1,
		RemoteName:      "remote",
		RemotePath:      "backup/file.bin",
		Status:          model.TaskStatusRunning,
		RetryCount:      1,
		FileSize:        info.Size(),
		FileFingerprint: fingerprint,
		FileRecord: &model.FileRecord{
			ID:          1,
			LocalPath:   localPath,
			FileSize:    info.Size(),
			Fingerprint: fingerprint,
		},
	}
}

type fakeLeaseRepo struct {
	mu                sync.Mutex
	successes         int
	failures          int
	canceled          int
	releases          int
	failureStatus     string
	lastFailureStatus string
	nextRetryAt       time.Time
	cancelRequested   bool
	markedFingerprint string
}

func (r *fakeLeaseRepo) ClaimNext(context.Context, string, time.Duration, int) (*model.UploadTask, error) {
	return nil, nil
}

func (r *fakeLeaseRepo) RenewLease(context.Context, uint, string, time.Duration) (bool, error) {
	return true, nil
}

func (r *fakeLeaseRepo) UpdateProgress(context.Context, uint, string, float64, int64, time.Time) error {
	return nil
}

func (r *fakeLeaseRepo) FinishSuccess(_ context.Context, _ uint, _ string, _ uint, fingerprint, _, _ string, _ time.Time, _ int64) (bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.successes++
	r.markedFingerprint = fingerprint
	return true, true, nil
}

func (r *fakeLeaseRepo) FinishCanceled(context.Context, uint, string, time.Time, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceled++
	return true, nil
}

func (r *fakeLeaseRepo) FailOrRetry(_ context.Context, _ uint, _, _ string, nextRetryAt time.Time, _ int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
	r.nextRetryAt = nextRetryAt
	status := r.failureStatus
	if status == "" {
		status = model.TaskStatusFailed
	}
	r.lastFailureStatus = status
	return status, nil
}

func (r *fakeLeaseRepo) ReleaseLease(context.Context, uint, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases++
	return nil
}

func (r *fakeLeaseRepo) IsCancellationRequested(context.Context, uint) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelRequested, nil
}

type fakeWatchStatsRepo struct {
	successes int
	failures  int
}

func (r *fakeWatchStatsRepo) RecordUploadSuccess(context.Context, uint, int64, time.Time) error {
	r.successes++
	return nil
}

func (r *fakeWatchStatsRepo) RecordUploadFailure(context.Context, uint, time.Time) error {
	r.failures++
	return nil
}

type fakeUploadLogRepo struct {
	logs []model.UploadLog
}

func (r *fakeUploadLogRepo) Create(log *model.UploadLog) error {
	r.logs = append(r.logs, *log)
	return nil
}

func (r *fakeUploadLogRepo) ListByTaskID(uint, int) ([]model.UploadLog, error) {
	return r.logs, nil
}

type fakeRcloneClient struct {
	result             rclone.Result
	err                error
	started            chan struct{}
	blockUntilCanceled bool
	afterCopy          func()
}

func (c *fakeRcloneClient) Copy(ctx context.Context, _, _, _ string, onProgress func(rclone.Progress)) (rclone.Result, error) {
	if c.started != nil {
		close(c.started)
	}
	if onProgress != nil {
		onProgress(rclone.Progress{Percent: 50, BytesDone: 1, BytesTotal: 2})
	}
	if c.blockUntilCanceled {
		<-ctx.Done()
		return rclone.Result{Success: false, Error: ctx.Err().Error()}, ctx.Err()
	}
	if c.afterCopy != nil {
		c.afterCopy()
	}
	return c.result, c.err
}

func (c *fakeRcloneClient) ListRemotes(context.Context) ([]rclone.Remote, error) {
	return nil, nil
}

func (c *fakeRcloneClient) WalkRemote(context.Context, string, string, func(rclone.RemoteObject) error) error {
	return nil
}
