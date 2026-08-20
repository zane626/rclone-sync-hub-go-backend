package model

import "time"

// FileRecord 文件记录表 file_records，用于判断是否已上传。
// MySQL InnoDB 单列索引最大 3072 字节（utf8mb4 约 768 字符），故索引列不超过 768。
type FileRecord struct {
	ID            uint       `gorm:"primaryKey"`
	WatchFolderID uint       `gorm:"not null;default:0;index"`
	LocalPath     string     `gorm:"size:768;not null;uniqueIndex:idx_local_path"`
	RelativePath  string     `gorm:"size:768;not null"` // 相对扫描根目录的路径
	RemoteName    string     `gorm:"size:255"`
	RemotePath    string     `gorm:"size:768;not null"`
	FileSize      int64      `gorm:"default:0"`
	FileModTime   time.Time  `gorm:""`
	Fingerprint   string     `gorm:"size:64"`       // size + mtime 的快速元数据指纹
	FileHash      string     `gorm:"size:64;index"` // 可选，内容哈希用于强校验
	LastSeenAt    *time.Time `gorm:""`
	MissingAt     *time.Time `gorm:"index"` // 最近一次完整扫描确认文件已不存在
	UploadedAt    *time.Time `gorm:""`
	CreatedAt     time.Time  `gorm:""`
	UpdatedAt     time.Time  `gorm:""`
}

// TableName 指定表名。
func (FileRecord) TableName() string {
	return "file_records"
}
