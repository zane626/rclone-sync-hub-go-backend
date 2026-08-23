package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
)

type remoteMutationRepo struct {
	route         model.RemoteRoute
	scheduleCalls int
}

func (r *remoteMutationRepo) Create(context.Context, *model.RemoteRoute) error { return nil }
func (r *remoteMutationRepo) GetByID(_ context.Context, id uint) (*model.RemoteRoute, error) {
	if id != r.route.ID {
		return nil, errors.New("not found")
	}
	copy := r.route
	return &copy, nil
}
func (r *remoteMutationRepo) List(context.Context, string, string, int, int) ([]model.RemoteRoute, int64, error) {
	return nil, 0, nil
}
func (r *remoteMutationRepo) Update(context.Context, *model.RemoteRoute, bool) error { return nil }
func (r *remoteMutationRepo) Delete(context.Context, uint) error                     { return nil }
func (r *remoteMutationRepo) ListEnabledForScan(context.Context, time.Time) ([]model.RemoteRoute, error) {
	return nil, nil
}
func (r *remoteMutationRepo) ClaimForScan(context.Context, uint, string, time.Time, time.Time, time.Duration) (bool, error) {
	return false, nil
}
func (r *remoteMutationRepo) RenewScanLease(context.Context, uint, string, time.Duration) (bool, error) {
	return false, nil
}
func (r *remoteMutationRepo) FinishScan(context.Context, uint, string, map[string]interface{}) error {
	return nil
}
func (r *remoteMutationRepo) ScheduleScan(context.Context, uint) error {
	r.scheduleCalls++
	return nil
}
func (r *remoteMutationRepo) ScheduleAllScans(context.Context) (int64, error) { return 0, nil }
func (r *remoteMutationRepo) UpsertFileRecords(context.Context, []*model.RemoteFileRecord, int) error {
	return nil
}
func (r *remoteMutationRepo) DeleteUnseenFileRecords(context.Context, uint, time.Time) (int64, error) {
	return 0, nil
}
func (r *remoteMutationRepo) ListFileRecords(context.Context, uint, string, int, int) ([]model.RemoteFileRecord, int64, error) {
	return nil, 0, nil
}

type remoteMutationWaker struct{ calls int }

func (w *remoteMutationWaker) Wake() { w.calls++ }

type remoteMutationOperator struct {
	objects      map[string]rclone.RemoteObject
	makeDirError error
}

func newRemoteMutationOperator(paths map[string]rclone.RemoteObject) *remoteMutationOperator {
	return &remoteMutationOperator{objects: paths}
}

func (o *remoteMutationOperator) StatRemote(_ context.Context, _ string, remotePath string) (rclone.RemoteObject, bool, error) {
	object, exists := o.objects[remotePath]
	return object, exists, nil
}

func (o *remoteMutationOperator) MakeRemoteDirectory(_ context.Context, _ string, remotePath string) error {
	if o.makeDirError != nil {
		return o.makeDirError
	}
	o.objects[remotePath] = rclone.RemoteObject{Name: remotePath, Path: remotePath, IsDir: true}
	return nil
}

func (o *remoteMutationOperator) MoveRemoteObject(_ context.Context, _ string, sourcePath, destinationPath string) error {
	object, exists := o.objects[sourcePath]
	if !exists {
		return errors.New("source missing")
	}
	if _, exists := o.objects[destinationPath]; exists {
		return errors.New("destination exists")
	}
	delete(o.objects, sourcePath)
	object.Path = destinationPath
	o.objects[destinationPath] = object
	if object.IsDir {
		prefix := sourcePath + "/"
		for path, child := range o.objects {
			if strings.HasPrefix(path, prefix) {
				delete(o.objects, path)
				movedPath := destinationPath + strings.TrimPrefix(path, sourcePath)
				child.Path = movedPath
				o.objects[movedPath] = child
			}
		}
	}
	return nil
}

func newRemoteMutationService() (*remoteRouteService, *remoteMutationRepo, *remoteMutationOperator, *remoteMutationWaker) {
	repo := &remoteMutationRepo{route: model.RemoteRoute{
		ID: 7, Name: "archive", RemoteName: "drive", RemotePath: "backup", Enabled: true, Status: model.RemoteRouteStatusReady,
	}}
	operator := newRemoteMutationOperator(map[string]rclone.RemoteObject{
		"backup": {Name: "backup", Path: "backup", IsDir: true},
	})
	waker := &remoteMutationWaker{}
	service := NewRemoteRouteService(repo, nil, waker, operator).(*remoteRouteService)
	return service, repo, operator, waker
}

func TestRemoteRouteCreateAndRenameFolder(t *testing.T) {
	service, repo, operator, waker := newRemoteMutationService()
	created, err := service.CreateFolder(context.Background(), 7, "", "season-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != "season-1" || !created.RefreshScheduled || !operator.objects["backup/season-1"].IsDir {
		t.Fatalf("unexpected create result=%+v objects=%+v", created, operator.objects)
	}
	operator.objects["backup/season-1/episode.mp4"] = rclone.RemoteObject{Name: "episode.mp4", Path: "backup/season-1/episode.mp4"}
	renamed, err := service.RenameFolder(context.Background(), 7, "season-1", "season-a")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Path != "season-a" || !operator.objects["backup/season-a"].IsDir {
		t.Fatalf("unexpected rename result=%+v objects=%+v", renamed, operator.objects)
	}
	if _, exists := operator.objects["backup/season-a/episode.mp4"]; !exists {
		t.Fatalf("rename did not preserve nested objects: %+v", operator.objects)
	}
	if repo.scheduleCalls != 2 || waker.calls != 2 {
		t.Fatalf("mutations did not schedule index refresh: schedules=%d wakes=%d", repo.scheduleCalls, waker.calls)
	}
}

func TestRemoteRouteBatchMoveDoesNotOverwrite(t *testing.T) {
	service, repo, operator, _ := newRemoteMutationService()
	operator.objects["backup/source-a"] = rclone.RemoteObject{Name: "source-a", Path: "backup/source-a", IsDir: true}
	operator.objects["backup/source-b"] = rclone.RemoteObject{Name: "source-b", Path: "backup/source-b", IsDir: true}
	operator.objects["backup/target"] = rclone.RemoteObject{Name: "target", Path: "backup/target", IsDir: true}
	operator.objects["backup/source-a/video.mp4"] = rclone.RemoteObject{Name: "video.mp4", Path: "backup/source-a/video.mp4", Size: 10}
	operator.objects["backup/source-b/video.mp4"] = rclone.RemoteObject{Name: "video.mp4", Path: "backup/source-b/video.mp4", Size: 20}
	operator.objects["backup/source-a/existing.mp4"] = rclone.RemoteObject{Name: "existing.mp4", Path: "backup/source-a/existing.mp4", Size: 30}
	operator.objects["backup/target/existing.mp4"] = rclone.RemoteObject{Name: "existing.mp4", Path: "backup/target/existing.mp4", Size: 40}

	result, err := service.MoveFiles(context.Background(), 7, []string{
		"source-a/video.mp4",
		"source-b/video.mp4",
		"source-a/existing.mp4",
	}, "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Moved) != 1 || result.Moved[0].DestinationPath != "target/video.mp4" {
		t.Fatalf("unexpected moved items: %+v", result)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("expected duplicate-name and existing-target failures: %+v", result)
	}
	if _, exists := operator.objects["backup/source-a/video.mp4"]; exists {
		t.Fatal("successfully moved source still exists")
	}
	if operator.objects["backup/target/existing.mp4"].Size != 40 {
		t.Fatal("existing destination was overwritten")
	}
	if repo.scheduleCalls != 1 || !result.RefreshScheduled {
		t.Fatalf("successful batch did not schedule one refresh: %+v", result)
	}
}

func TestRemoteRouteBatchMoveReportsPerFileProgress(t *testing.T) {
	service, _, operator, _ := newRemoteMutationService()
	operator.objects["backup/source"] = rclone.RemoteObject{Name: "source", Path: "backup/source", IsDir: true}
	operator.objects["backup/target"] = rclone.RemoteObject{Name: "target", Path: "backup/target", IsDir: true}
	operator.objects["backup/source/one.mp4"] = rclone.RemoteObject{Name: "one.mp4", Path: "backup/source/one.mp4", Size: 10}
	operator.objects["backup/source/two.mp4"] = rclone.RemoteObject{Name: "two.mp4", Path: "backup/source/two.mp4", Size: 20}

	var snapshots []RemoteMoveProgress
	result, err := service.MoveFilesWithProgress(context.Background(), 7, []string{
		"source/one.mp4", "source/two.mp4",
	}, "target", func(progress RemoteMoveProgress) {
		snapshots = append(snapshots, progress)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Moved) != 2 || len(result.Failed) != 0 {
		t.Fatalf("unexpected move result: %+v", result)
	}
	if len(snapshots) < 5 {
		t.Fatalf("expected preparation, validation, move, and completion snapshots: %+v", snapshots)
	}
	last := snapshots[len(snapshots)-1]
	if last.Phase != "completed" || last.Processed != 2 || last.Total != 2 || len(last.Result.Moved) != 2 {
		t.Fatalf("unexpected final progress snapshot: %+v", last)
	}
	foundCurrentFile := false
	for _, snapshot := range snapshots {
		if snapshot.Phase == "moving" && snapshot.CurrentPath == "source/one.mp4" {
			foundCurrentFile = true
			break
		}
	}
	if !foundCurrentFile {
		t.Fatalf("move progress did not expose the current file: %+v", snapshots)
	}
}

func TestRemoteRouteMutationsRejectTraversalAndRootRename(t *testing.T) {
	service, _, _, _ := newRemoteMutationService()
	if _, err := service.CreateFolder(context.Background(), 7, "../escape", "unsafe"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if _, err := service.RenameFolder(context.Background(), 7, "", "renamed-root"); err == nil {
		t.Fatal("route root rename must be rejected")
	}
	tooMany := make([]string, maxRemoteMoveBatch+1)
	for i := range tooMany {
		tooMany[i] = "file"
	}
	if _, err := service.MoveFiles(context.Background(), 7, tooMany, ""); err == nil {
		t.Fatal("oversized batch must be rejected")
	}
}

func TestRemoteRouteCreateFolderReturnsActionableBackendConflict(t *testing.T) {
	service, _, operator, _ := newRemoteMutationService()
	operator.makeDirError = rclone.ErrRemoteDirectoryNotCreated

	_, err := service.CreateFolder(context.Background(), 7, "", "season-2")
	if err == nil {
		t.Fatal("expected rejected remote mkdir")
	}
	publicError, ok := apperror.As(err)
	if !ok || publicError.Status != 409 {
		t.Fatalf("expected public conflict, got %#v", err)
	}
	if !strings.Contains(publicError.Message, "OpenList 115 v4.2.2") {
		t.Fatalf("expected actionable OpenList upgrade guidance, got %q", publicError.Message)
	}
}
