package repository

import (
	"fmt"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

// WatchFolderRepository 监听文件夹数据访问接口。
type WatchFolderRepository interface {
	Create(f *model.WatchFolder) error
	GetByID(id uint) (*model.WatchFolder, error)
	Update(f *model.WatchFolder) error
	Delete(id uint) error
	// List 按状态分页查询，status 为空则不过滤。
	List(status string, offset, limit int) ([]model.WatchFolder, int64, error)
}

type watchFolderRepository struct {
	db *gorm.DB
}

// NewWatchFolderRepository 构造 WatchFolderRepository。
func NewWatchFolderRepository(db *gorm.DB) WatchFolderRepository {
	return &watchFolderRepository{db: db}
}

func (r *watchFolderRepository) Create(f *model.WatchFolder) error {
	if err := r.db.Create(f).Error; err != nil {
		return fmt.Errorf("watch_folder create: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) GetByID(id uint) (*model.WatchFolder, error) {
	var f model.WatchFolder
	if err := r.db.First(&f, id).Error; err != nil {
		return nil, fmt.Errorf("watch_folder get by id: %w", err)
	}
	return &f, nil
}

func (r *watchFolderRepository) Update(f *model.WatchFolder) error {
	if err := r.db.Save(f).Error; err != nil {
		return fmt.Errorf("watch_folder update: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.WatchFolder{}, id).Error; err != nil {
		return fmt.Errorf("watch_folder delete: %w", err)
	}
	return nil
}

func (r *watchFolderRepository) List(status string, offset, limit int) ([]model.WatchFolder, int64, error) {
	var list []model.WatchFolder
	var total int64

	q := r.db.Model(&model.WatchFolder{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("watch_folder count: %w", err)
	}

	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("watch_folder list: %w", err)
	}
	return list, total, nil
}

