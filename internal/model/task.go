// Package model 定义领域模型与数据库表结构。
package model

import "time"

// TaskStatus 任务状态。
const (
	TaskStatusPending = "pending"  // 待上传
	TaskStatusRunning = "running"  // 上传中
	TaskStatusSuccess = "success"  // 上传完成
	TaskStatusFailed  = "failed"   // 上传失败
	TaskStatusPaused  = "paused"   // 暂停上传
)

// UploadTask 上传任务表 upload_tasks。
type UploadTask struct {
	ID           uint       `gorm:"primaryKey"`
	FileRecordID uint       `gorm:"not null;index"`

	// 归属与标识
	WatchFolderID   uint   `gorm:"index"`            // 所属监听文件夹 ID
	WatchFolderName string `gorm:"size:255"`         // 所属监听文件夹名称快照
	FileName        string `gorm:"size:512"`         // 文件名
	LocalPath       string `gorm:"size:1024"`        // 文件本地路径
	RemoteName      string `gorm:"size:255"`         // 上传网盘（remote 名称）
	RemotePath      string `gorm:"size:1024"`        // 上传路径

	// 状态与进度
	Status     string  `gorm:"size:20;not null;index"`           // pending / running / success / failed / paused
	Progress   float64 `gorm:"type:decimal(5,2);default:0"`      // 上传进度（0-100）
	Speed      int64   `gorm:"default:0"`                        // 最近一次上报的速度（bytes/s）
	RetryCount int     `gorm:"default:0"`                        // 重试次数
	ErrorMsg   string  `gorm:"column:error_message;type:text"`   // 最后一次错误信息

	// 时间维度
	DurationSeconds int64      `gorm:"default:0"` // 任务完成耗时（秒）
	StartedAt       *time.Time `gorm:""`         // 上传开始时间
	FinishedAt      *time.Time `gorm:""`         // 上传结束时间
	LastStatusAt    *time.Time `gorm:""`         // 最近一次状态变更时间
	LastProgressAt  *time.Time `gorm:""`         // 最近一次进度上报时间

	// 文件信息
	FileSize int64 `gorm:"default:0"` // 文件大小（字节）

	// 日志与分析
	Log                 string `gorm:"type:text"`   // 任务日志（简要汇总，可选）
	AccumulatedFailures int64  `gorm:"default:0"`   // 累计失败次数（便于分析）

	CreatedAt time.Time `gorm:""`
	UpdatedAt time.Time `gorm:""`

	// 关联（不参与表结构，仅查询用）
	FileRecord *FileRecord `gorm:"foreignKey:FileRecordID"`
}

// TableName 指定表名。
func (UploadTask) TableName() string {
	return "upload_tasks"
}
