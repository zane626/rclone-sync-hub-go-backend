package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
)

type fakeRemoteRouteRepo struct {
	route             model.RemoteRoute
	records           map[string]model.RemoteFileRecord
	finished          map[string]interface{}
	markMissingCalled bool
	markMissingBefore time.Time
}

func newFakeRemoteRouteRepo() *fakeRemoteRouteRepo {
	now := time.Now().Add(-time.Minute)
	return &fakeRemoteRouteRepo{
		route:   model.RemoteRoute{ID: 7, Name: "archive", RemoteName: "drive", RemotePath: "backup", Enabled: true, Status: model.RemoteRouteStatusPending, ScanIntervalSeconds: 300, NextScanAt: &now, UpdatedAt: now},
		records: make(map[string]model.RemoteFileRecord),
	}
}

func (r *fakeRemoteRouteRepo) ListEnabledForScan(context.Context, time.Time) ([]model.RemoteRoute, error) {
	return []model.RemoteRoute{r.route}, nil
}
func (r *fakeRemoteRouteRepo) ClaimForScan(_ context.Context, id uint, owner string, _ time.Time, started time.Time, lease time.Duration) (bool, error) {
	if id != r.route.ID {
		return false, nil
	}
	r.route.Status = model.RemoteRouteStatusScanning
	r.route.ScanLeaseOwner = owner
	expires := started.Add(lease)
	r.route.ScanLeaseExpiresAt = &expires
	return true, nil
}
func (r *fakeRemoteRouteRepo) RenewScanLease(context.Context, uint, string, time.Duration) (bool, error) {
	return true, nil
}
func (r *fakeRemoteRouteRepo) FinishScan(_ context.Context, _ uint, _ string, updates map[string]interface{}) error {
	r.finished = updates
	return nil
}
func (r *fakeRemoteRouteRepo) UpsertFileRecords(_ context.Context, records []*model.RemoteFileRecord, _ int) error {
	for _, record := range records {
		r.records[record.Path] = *record
	}
	return nil
}
func (r *fakeRemoteRouteRepo) MarkUnseenMissing(_ context.Context, _ uint, before time.Time, _ time.Time) (int64, error) {
	r.markMissingCalled = true
	r.markMissingBefore = before
	return 0, nil
}

type fakeRemoteWalker struct {
	objects []rclone.RemoteObject
	err     error
}

func (f *fakeRemoteWalker) Copy(context.Context, string, string, string, func(rclone.Progress)) (rclone.Result, error) {
	return rclone.Result{Success: true}, nil
}
func (f *fakeRemoteWalker) ListRemotes(context.Context) ([]rclone.Remote, error) { return nil, nil }
func (f *fakeRemoteWalker) WalkRemote(_ context.Context, _, _ string, visit func(rclone.RemoteObject) error) error {
	if f.err != nil {
		return f.err
	}
	for _, object := range f.objects {
		if err := visit(object); err != nil {
			return err
		}
	}
	return nil
}

func TestRemoteRouteScannerPersistsFilesAndDirectoryTree(t *testing.T) {
	repo := newFakeRemoteRouteRepo()
	walker := &fakeRemoteWalker{objects: []rclone.RemoteObject{
		{Path: "shows/episode-1.mp4", Name: "episode-1.mp4", Size: 100},
		{Path: "shows/season-2/episode-2.mp4", Name: "episode-2.mp4", Size: 250},
		{Path: "empty", Name: "empty", IsDir: true},
	}}
	scanner := NewRemoteRouteScanner(repo, walker, RemoteRouteScannerConfig{BatchSize: 2, RouteTimeout: time.Second, LeaseDuration: time.Minute, Heartbeat: 10 * time.Second})
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"shows", "shows/episode-1.mp4", "shows/season-2", "shows/season-2/episode-2.mp4", "empty"} {
		if _, ok := repo.records[expected]; !ok {
			t.Fatalf("missing indexed path %q: %+v", expected, repo.records)
		}
	}
	if !repo.records["shows"].IsDir || repo.records["shows/episode-1.mp4"].IsDir {
		t.Fatal("directory/file types were not persisted correctly")
	}
	if repo.finished["status"] != model.RemoteRouteStatusReady || repo.finished["total_file_count"] != int64(2) || repo.finished["total_file_size"] != int64(350) {
		t.Fatalf("unexpected final scan state: %+v", repo.finished)
	}
	if !repo.markMissingCalled {
		t.Fatal("successful complete scan must mark unseen historical entries missing")
	}
	seenAt := repo.records["shows/episode-1.mp4"].LastSeenAt
	if seenAt == nil || !seenAt.Equal(repo.markMissingBefore) || seenAt.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("scan marker must use persisted millisecond precision: seen=%v missing-before=%v", seenAt, repo.markMissingBefore)
	}
}

func TestRemoteRouteScannerDoesNotMarkMissingAfterPartialFailure(t *testing.T) {
	repo := newFakeRemoteRouteRepo()
	walker := &fakeRemoteWalker{err: errors.New("remote unavailable")}
	scanner := NewRemoteRouteScanner(repo, walker, RemoteRouteScannerConfig{RouteTimeout: time.Second, LeaseDuration: time.Minute, Heartbeat: 10 * time.Second})
	if err := scanner.ScanOnce(context.Background()); err == nil {
		t.Fatal("failed remote listing must be reported")
	}
	if repo.markMissingCalled {
		t.Fatal("partial or failed scans must not mark prior records missing")
	}
	if repo.finished["status"] != model.RemoteRouteStatusError {
		t.Fatalf("failure status was not persisted: %+v", repo.finished)
	}
}

func TestCleanRemoteObjectPathRejectsTraversal(t *testing.T) {
	if _, err := cleanRemoteObjectPath("../secret"); err == nil {
		t.Fatal("remote path traversal must be rejected")
	}
	if got, err := cleanRemoteObjectPath("folder\\nested//file.txt"); err != nil || got != "folder/nested/file.txt" {
		t.Fatalf("normalized path=%q err=%v", got, err)
	}
}
