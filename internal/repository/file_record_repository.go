package repository

import (
	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

// FileRecordRepository 文件记录数据访问接口。
type FileRecordRepository interface {
	Create(f *model.FileRecord) error
	GetByID(id uint) (*model.FileRecord, error)
	GetByLocalPath(localPath string) (*model.FileRecord, error)
	Update(f *model.FileRecord) error
	ExistsByLocalPath(localPath string) (bool, error)
}

type fileRecordRepository struct {
	db *gorm.DB
}

// NewFileRecordRepository 构造 FileRecordRepository。
func NewFileRecordRepository(db *gorm.DB) FileRecordRepository {
	return &fileRecordRepository{db: db}
}

func (r *fileRecordRepository) Create(f *model.FileRecord) error {
	return r.db.Create(f).Error
}

func (r *fileRecordRepository) GetByID(id uint) (*model.FileRecord, error) {
	var f model.FileRecord
	err := r.db.First(&f, id).Error
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *fileRecordRepository) GetByLocalPath(localPath string) (*model.FileRecord, error) {
	var f model.FileRecord
	err := r.db.Where("local_path = ?", localPath).First(&f).Error
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *fileRecordRepository) Update(f *model.FileRecord) error {
	return r.db.Save(f).Error
}

func (r *fileRecordRepository) ExistsByLocalPath(localPath string) (bool, error) {
	var n int64
	err := r.db.Model(&model.FileRecord{}).Where("local_path = ?", localPath).Count(&n).Error
	return n > 0, err
}
