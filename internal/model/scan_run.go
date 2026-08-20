package model

import "time"

const (
	ScanRunStatusRunning  = "running"
	ScanRunStatusSuccess  = "success"
	ScanRunStatusFailed   = "failed"
	ScanRunStatusCanceled = "canceled"
)

// ScanRun 保存每次监听目录扫描的审计与性能统计。
type ScanRun struct {
	ID              uint64 `gorm:"primaryKey"`
	WatchFolderID   uint   `gorm:"not null;index:idx_scan_run_folder_started,priority:1"`
	WatchFolderName string `gorm:"size:255"`
	Status          string `gorm:"size:20;not null;index"`

	StartedAt            time.Time `gorm:"index:idx_scan_started_at;index:idx_scan_run_folder_started,priority:2"`
	FinishedAt           *time.Time
	DurationMilliseconds int64 `gorm:"default:0"`

	FilesSeen          int64  `gorm:"default:0"`
	TotalBytes         int64  `gorm:"default:0"`
	FilesUnchanged     int64  `gorm:"default:0"`
	FilesStableSkipped int64  `gorm:"default:0"`
	FilesMissing       int64  `gorm:"default:0"`
	TasksCreated       int64  `gorm:"default:0"`
	ScanErrors         int64  `gorm:"default:0"`
	ErrorMessage       string `gorm:"type:text"`

	CreatedAt time.Time `gorm:"index:idx_scan_created_at"`
	UpdatedAt time.Time
}

func (ScanRun) TableName() string {
	return "scan_runs"
}
