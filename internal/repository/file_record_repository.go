package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FileRecordRepository 文件记录数据访问接口。
type FileRecordRepository interface {
	CreateSnapshot(ctx context.Context, f *model.FileRecord) error
	CreateSnapshots(ctx context.Context, records []*model.FileRecord, batchSize int) error
	UpdateSnapshots(ctx context.Context, records []*model.FileRecord, clearUploadedIDs []uint, batchSize int) error
	GetByLocalPath(localPath string) (*model.FileRecord, error)
	ListForWatchFolder(ctx context.Context, watchFolderID uint, root string) ([]model.FileRecord, error)
	UpdateSnapshot(ctx context.Context, f *model.FileRecord, clearUploaded bool) error
	MarkMissing(ctx context.Context, ids []uint, at time.Time, batchSize int) error
}

type fileRecordRepository struct {
	db *gorm.DB
}

// NewFileRecordRepository 构造 FileRecordRepository。
func NewFileRecordRepository(db *gorm.DB) FileRecordRepository {
	return &fileRecordRepository{db: db}
}

func (r *fileRecordRepository) CreateSnapshot(ctx context.Context, f *model.FileRecord) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(f).Error
}

// CreateSnapshots inserts newly observed files in bounded batches. The unique
// local_path index plus DO NOTHING keeps concurrent scanner instances idempotent.
func (r *fileRecordRepository) CreateSnapshots(ctx context.Context, records []*model.FileRecord, batchSize int) error {
	if len(records) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(records, batchSize).Error
}

// UpdateSnapshots persists changed existing snapshots in bounded transactions.
// uploaded_at is cleared before the metadata change while the same rows are
// locked, so an upload completing concurrently can only mark the new version.
func (r *fileRecordRepository) UpdateSnapshots(ctx context.Context, records []*model.FileRecord, clearUploadedIDs []uint, batchSize int) error {
	if len(records) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	clearSet := make(map[uint]struct{}, len(clearUploadedIDs))
	for _, id := range clearUploadedIDs {
		clearSet[id] = struct{}{}
	}
	updateColumns := []string{
		"watch_folder_id", "relative_path", "remote_name", "remote_path",
		"file_size", "file_mod_time", "fingerprint", "last_seen_at",
		"missing_at", "updated_at",
	}
	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[start:end]
		now := time.Now()
		clearBatch := make([]uint, 0)
		for _, record := range batch {
			record.UpdatedAt = now
			if _, clear := clearSet[record.ID]; clear {
				clearBatch = append(clearBatch, record.ID)
			}
		}
		tx := r.db.WithContext(ctx).Begin()
		if tx.Error != nil {
			return fmt.Errorf("begin batch snapshot update: %w", tx.Error)
		}
		if len(clearBatch) > 0 {
			if err := tx.Model(&model.FileRecord{}).Where("id IN ?", clearBatch).Update("uploaded_at", nil).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("clear uploaded snapshot versions: %w", err)
			}
		}
		if err := tx.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns(updateColumns)}).
			CreateInBatches(batch, len(batch)).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("batch update file snapshots: %w", err)
		}
		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("commit batch snapshot update: %w", err)
		}
	}
	return nil
}

func (r *fileRecordRepository) GetByLocalPath(localPath string) (*model.FileRecord, error) {
	var f model.FileRecord
	err := r.db.Where("local_path = ?", localPath).First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("file record local path: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("file record get by local path: %w", err)
	}
	return &f, nil
}

// ListForWatchFolder 一次性读取目录已有快照，避免扫描过程中逐文件查询数据库。
// watch_folder_id 条件覆盖新数据；只有 watch_folder_id=0 的旧数据才按路径前缀兼容认领。
func (r *fileRecordRepository) ListForWatchFolder(ctx context.Context, watchFolderID uint, root string) ([]model.FileRecord, error) {
	var list []model.FileRecord
	root = filepath.Clean(root)
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	escapedPrefix := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(prefix)
	err := r.db.WithContext(ctx).
		Where("watch_folder_id = ? OR (watch_folder_id = 0 AND (local_path = ? OR local_path LIKE ? ESCAPE '!'))", watchFolderID, root, escapedPrefix+"%").
		Find(&list).Error
	return list, err
}

// UpdateSnapshot 只更新扫描器拥有的快照字段，避免覆盖 worker 并发写入的 uploaded_at。
func (r *fileRecordRepository) UpdateSnapshot(ctx context.Context, f *model.FileRecord, clearUploaded bool) error {
	updates := map[string]interface{}{
		"watch_folder_id": f.WatchFolderID,
		"relative_path":   f.RelativePath,
		"remote_name":     f.RemoteName,
		"remote_path":     f.RemotePath,
		"file_size":       f.FileSize,
		"file_mod_time":   f.FileModTime,
		"fingerprint":     f.Fingerprint,
		"last_seen_at":    f.LastSeenAt,
		"missing_at":      nil,
	}
	if clearUploaded {
		updates["uploaded_at"] = nil
	}
	return r.db.WithContext(ctx).Model(&model.FileRecord{}).Where("id = ?", f.ID).Updates(updates).Error
}

func (r *fileRecordRepository) MarkMissing(ctx context.Context, ids []uint, at time.Time, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 500
	}
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := r.db.WithContext(ctx).Model(&model.FileRecord{}).
			Where("id IN ? AND missing_at IS NULL", ids[start:end]).
			Update("missing_at", at).Error; err != nil {
			return err
		}
	}
	return nil
}
