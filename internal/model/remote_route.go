package model

import "time"

const (
	RemoteRouteStatusPending  = "pending"
	RemoteRouteStatusScanning = "scanning"
	RemoteRouteStatusReady    = "ready"
	RemoteRouteStatusError    = "error"
	RemoteRouteStatusDisabled = "disabled"
)

// RemoteRoute is a reusable rclone destination selected by watch folders.
// Scan state is durable so multiple application instances can safely share it.
type RemoteRoute struct {
	ID uint `gorm:"primaryKey"`

	Name       string `gorm:"size:255;not null;uniqueIndex"`
	RouteKey   string `gorm:"size:64;not null;uniqueIndex"`
	RemoteName string `gorm:"size:255;not null;index"`
	RemotePath string `gorm:"size:1024;not null"`
	Enabled    bool   `gorm:"not null;default:true;index:idx_remote_route_due,priority:1"`

	Status              string `gorm:"size:24;not null;index"`
	ScanIntervalSeconds int    `gorm:"not null;default:3600"`
	LastError           string `gorm:"type:text"`
	LastScanStartedAt   *time.Time
	LastScanFinishedAt  *time.Time
	LastScanSuccessAt   *time.Time
	LastScanDurationMs  int64      `gorm:"default:0"`
	NextScanAt          *time.Time `gorm:"index:idx_remote_route_due,priority:2"`
	ScanLeaseOwner      string     `gorm:"size:160;index"`
	ScanLeaseExpiresAt  *time.Time `gorm:"index;index:idx_remote_route_due,priority:3"`

	TotalFileCount int64 `gorm:"default:0"`
	TotalFileSize  int64 `gorm:"default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time

	WatchFolderCount int64 `gorm:"-"`
}

func (RemoteRoute) TableName() string { return "remote_routes" }
