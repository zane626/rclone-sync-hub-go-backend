package model

import "time"

// WatchFolderStatus 监听文件夹状态。
const (
	WatchFolderStatusDetecting = "detecting" // 校验中
	WatchFolderStatusWatching  = "watching"  // 正在监听（正常工作）
	WatchFolderStatusStopped   = "stopped"   // 手动停止
	WatchFolderStatusPaused    = "paused"    // 暂停上传（仍可扫描）
	WatchFolderStatusError     = "error"     // 出现错误（如路径不可用）
)

// WatchFolderSyncType 同步类型，当前默认 local->remote，预留扩展。
const (
	WatchFolderSyncTypeLocalToRemote = "local_to_remote"
)

// WatchFolder 监听文件夹配置与统计信息。
// 用于管理需要被扫描/监听并同步到网盘的本地目录。
type WatchFolder struct {
	ID uint `gorm:"primaryKey"`

	// 基本配置
	Name string `gorm:"size:255;not null"` // 显示名称
	// 注意：MySQL InnoDB 单列索引最大 3072 字节（utf8mb4 约 768 字符），且 LocalPath 上有唯一索引，因此长度限制为 768。
	LocalPath  string `gorm:"size:768;not null;unique"`                 // 本地路径
	RemoteName string `gorm:"size:255;not null"`                        // rclone remote 名称
	RemotePath string `gorm:"size:1024;not null"`                       // 远端路径
	SyncType   string `gorm:"size:64;not null;default:local_to_remote"` // 同步类型
	MaxDepth   int    `gorm:"default:0"`                                // 最大监听深度，0 表示不限制
	Enabled    bool   `gorm:"not null;default:true"`                    // 是否启用该监听

	// 状态信息
	Status       string     `gorm:"size:32;not null;index"` // detecting / watching / stopped / paused / error
	LastError    string     `gorm:"type:text"`              // 最近一次错误信息（便于排查）
	LastScanAt   *time.Time // 最近一次扫描时间
	LastSyncAt   *time.Time // 最近一次同步时间
	NextScanAt   *time.Time // 预计下一次扫描时间（可选）
	LastActiveAt *time.Time // 最近有文件变更 / 上传的时间

	// 统计维度（便于分析与展示）
	TotalFileCount int64 `gorm:"default:0"` // 当前已知文件总数（最后一次扫描结果）
	TotalFileSize  int64 `gorm:"default:0"` // 当前已知文件总大小（字节）

	UploadedFileCount int64 `gorm:"default:0"` // 累计已成功上传文件数
	UploadedBytes     int64 `gorm:"default:0"` // 累计已上传字节数
	FailedFileCount   int64 `gorm:"default:0"` // 累计上传失败文件数

	// 最近一段时间的窗口统计（可用于展示趋势）
	WindowUploadedFiles int64 `gorm:"default:0"` // 窗口内上传文件数（如最近 24 小时）
	WindowUploadedBytes int64 `gorm:"default:0"` // 窗口内上传字节数

	// 配置相关扩展
	ScanIntervalSeconds int `gorm:"default:300"` // 扫描间隔（秒），未来可用于单独控制每个目录的扫描频率

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (WatchFolder) TableName() string {
	return "watch_folders"
}
