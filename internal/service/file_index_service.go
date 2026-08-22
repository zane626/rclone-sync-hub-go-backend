package service

import (
	"context"
	"errors"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
)

type IndexedFileEntry struct {
	ID           uint64     `json:"id,omitempty"`
	Type         string     `json:"type"`
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Status       string     `json:"status"`
	Size         int64      `json:"size"`
	FileCount    int64      `json:"file_count,omitempty"`
	ModTime      *time.Time `json:"mod_time,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
	MissingAt    *time.Time `json:"missing_at,omitempty"`
	TaskID       uint       `json:"task_id,omitempty"`
	TaskProgress float64    `json:"task_progress,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

type FileBrowseResult struct {
	CurrentPath string             `json:"current_path"`
	ParentPath  string             `json:"parent_path"`
	Items       []IndexedFileEntry `json:"items"`
	Total       int                `json:"total"`
	Page        int                `json:"page"`
	PageSize    int                `json:"page_size"`
}

type FileIndexService interface {
	BrowseWatchFolder(ctx context.Context, watchFolderID uint, currentPath string, page, pageSize int) (FileBrowseResult, error)
}

type fileIndexService struct {
	watchRepo repository.WatchFolderRepository
	fileRepo  repository.FileRecordRepository
	taskRepo  repository.TaskRepository
}

func NewFileIndexService(watchRepo repository.WatchFolderRepository, fileRepo repository.FileRecordRepository, taskRepo repository.TaskRepository) FileIndexService {
	return &fileIndexService{watchRepo: watchRepo, fileRepo: fileRepo, taskRepo: taskRepo}
}

func normalizeIndexPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return "", nil
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", apperror.Validation("path traversal is not allowed", nil)
	}
	if len(cleaned) > 768 {
		return "", apperror.Validation("path is too long", nil)
	}
	return cleaned, nil
}

func parentIndexPath(current string) string {
	if current == "" {
		return ""
	}
	parent := pathpkg.Dir(current)
	if parent == "." {
		return ""
	}
	return parent
}

func normalizeBrowsePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return page, pageSize
}

func (s *fileIndexService) BrowseWatchFolder(ctx context.Context, watchFolderID uint, currentPath string, page, pageSize int) (FileBrowseResult, error) {
	currentPath, err := normalizeIndexPath(currentPath)
	if err != nil {
		return FileBrowseResult{}, err
	}
	page, pageSize = normalizeBrowsePage(page, pageSize)
	if page > 10000 {
		return FileBrowseResult{}, apperror.Validation("page is too large", nil)
	}
	folder, err := s.watchRepo.GetByID(watchFolderID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return FileBrowseResult{}, apperror.NotFound("watch folder not found", err)
		}
		return FileBrowseResult{}, err
	}
	records, err := s.fileRepo.ListForWatchFolder(ctx, watchFolderID, folder.LocalPath)
	if err != nil {
		return FileBrowseResult{}, err
	}

	type directoryAggregate struct {
		entry      IndexedFileEntry
		allMissing bool
	}
	directories := make(map[string]*directoryAggregate)
	directFiles := make([]model.FileRecord, 0)
	prefix := ""
	if currentPath != "" {
		prefix = currentPath + "/"
	}
	for i := range records {
		relative, normalizeErr := normalizeIndexPath(records[i].RelativePath)
		if normalizeErr != nil || relative == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(relative, prefix) {
			continue
		}
		tail := strings.TrimPrefix(relative, prefix)
		if tail == "" {
			continue
		}
		if slash := strings.IndexByte(tail, '/'); slash >= 0 {
			name := tail[:slash]
			directoryPath := name
			if currentPath != "" {
				directoryPath = currentPath + "/" + name
			}
			aggregate := directories[directoryPath]
			if aggregate == nil {
				aggregate = &directoryAggregate{entry: IndexedFileEntry{Type: "directory", Name: name, Path: directoryPath, Status: "missing"}, allMissing: true}
				directories[directoryPath] = aggregate
			}
			aggregate.entry.FileCount++
			aggregate.entry.Size += records[i].FileSize
			if records[i].MissingAt == nil {
				aggregate.allMissing = false
				aggregate.entry.Status = "available"
			}
			continue
		}
		directFiles = append(directFiles, records[i])
	}

	fileIDs := make([]uint, 0, len(directFiles))
	for i := range directFiles {
		fileIDs = append(fileIDs, directFiles[i].ID)
	}
	tasks, err := s.taskRepo.ListLatestByFileRecordIDs(ctx, fileIDs)
	if err != nil {
		return FileBrowseResult{}, err
	}
	latestTask := make(map[uint]model.UploadTask, len(tasks))
	for i := range tasks {
		latestTask[tasks[i].FileRecordID] = tasks[i]
	}

	entries := make([]IndexedFileEntry, 0, len(directories)+len(directFiles))
	for _, aggregate := range directories {
		if aggregate.allMissing {
			aggregate.entry.Status = "missing"
		}
		entries = append(entries, aggregate.entry)
	}
	for i := range directFiles {
		record := directFiles[i]
		modTime := record.FileModTime
		entry := IndexedFileEntry{
			ID: uint64(record.ID), Type: "file", Name: pathpkg.Base(record.RelativePath), Path: strings.ReplaceAll(record.RelativePath, "\\", "/"),
			Status: "discovered", Size: record.FileSize, ModTime: &modTime, LastSeenAt: record.LastSeenAt,
			UploadedAt: record.UploadedAt, MissingAt: record.MissingAt,
		}
		if record.MissingAt != nil {
			entry.Status = "missing"
		} else if record.UploadedAt != nil {
			entry.Status = "uploaded"
		} else if task, ok := latestTask[record.ID]; ok {
			if task.Status != model.TaskStatusSuccess {
				entry.Status = task.Status
				entry.TaskID = task.ID
				entry.TaskProgress = task.Progress
				entry.ErrorMessage = task.ErrorMsg
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	total := len(entries)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return FileBrowseResult{
		CurrentPath: currentPath, ParentPath: parentIndexPath(currentPath), Items: entries[start:end],
		Total: total, Page: page, PageSize: pageSize,
	}, nil
}
