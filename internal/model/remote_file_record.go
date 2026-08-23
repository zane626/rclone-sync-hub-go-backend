package model

import "time"

// RemoteFileRecord is the current persisted result of an rclone lsjson scan.
// Successful complete scans delete entries that were not seen again. MissingAt
// is retained for schema compatibility and is always excluded from browsing.
type RemoteFileRecord struct {
	ID            uint64 `gorm:"primaryKey"`
	RemoteRouteID uint   `gorm:"not null;uniqueIndex:idx_remote_route_path,priority:1;index:idx_remote_parent,priority:1"`
	PathHash      string `gorm:"size:64;not null;uniqueIndex:idx_remote_route_path,priority:2"`
	Path          string `gorm:"size:2048;not null"`
	ParentHash    string `gorm:"size:64;not null;index:idx_remote_parent,priority:2"`
	ParentPath    string `gorm:"size:2048;not null"`
	Name          string `gorm:"size:512;not null"`
	IsDir         bool   `gorm:"not null;default:false"`
	Size          int64  `gorm:"default:0"`
	ModTime       *time.Time
	MimeType      string     `gorm:"size:255"`
	LastSeenAt    *time.Time `gorm:"index"`
	MissingAt     *time.Time `gorm:"index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (RemoteFileRecord) TableName() string { return "remote_file_records" }
