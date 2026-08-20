// Package model 定义领域模型与数据库表结构。
package model

import "time"

// TaskStatus 任务状态。
const (
	TaskStatusPending  = "pending"  // 待上传
	TaskStatusRunning  = "running"  // 上传中
	TaskStatusSuccess  = "success"  // 上传完成
	TaskStatusFailed   = "failed"   // 上传失败
	TaskStatusPaused   = "paused"   // 暂停上传
	TaskStatusCanceled = "canceled" // 已取消
)

// UploadTask 上传任务表 upload_tasks。
type UploadTask struct {
	ID           uint `gorm:"primaryKey"`
	FileRecordID uint `gorm:"not null;index"`

	// 归属与标识
	WatchFolderID   uint   `gorm:"index;index:idx_task_watch_status,priority:1"` // 所属监听文件夹 ID
	WatchFolderName string `gorm:"size:255"`                                     // 所属监听文件夹名称快照
	FileName        string `gorm:"size:512"`                                     // 文件名
	LocalPath       string `gorm:"size:1024"`                                    // 文件本地路径
	RemoteName      string `gorm:"size:255"`                                     // 上传网盘（remote 名称）
	RemotePath      string `gorm:"size:1024"`                                    // 上传路径

	// 状态与进度
	Status     string  `gorm:"size:20;not null;index;index:idx_task_watch_status,priority:2;index:idx_task_claim,priority:1;index:idx_task_lease,priority:1;index:idx_task_status_created,priority:1;index:idx_task_status_finished,priority:1;index:idx_task_status_updated,priority:1"` // pending / running / success / failed / paused / canceled
	Progress   float64 `gorm:"type:decimal(5,2);default:0"`                                                                                                                                                                                                                               // 上传进度（0-100）
	Speed      int64   `gorm:"default:0"`                                                                                                                                                                                                                                                 // 最近一次上报的速度（bytes/s）
	RetryCount int     `gorm:"default:0"`                                                                                                                                                                                                                                                 // 已领取执行次数（包含首次）
	Priority   int     `gorm:"default:0;index:idx_task_claim,priority:3"`                                                                                                                                                                                                                 // 数值越大优先级越高
	ErrorMsg   string  `gorm:"column:error_message;type:text"`                                                                                                                                                                                                                            // 最后一次错误信息

	// 时间维度
	DurationSeconds   int64      `gorm:"default:0"`                                                            // 任务完成耗时（秒）
	StartedAt         *time.Time `gorm:""`                                                                     // 上传开始时间
	FinishedAt        *time.Time `gorm:"index:idx_task_finished_at;index:idx_task_status_finished,priority:2"` // 上传结束时间
	LastStatusAt      *time.Time `gorm:""`                                                                     // 最近一次状态变更时间
	LastProgressAt    *time.Time `gorm:""`                                                                     // 最近一次进度上报时间
	NextRetryAt       *time.Time `gorm:"index:idx_task_claim,priority:2"`                                      // 下次允许领取时间
	LeaseOwner        string     `gorm:"size:160;index"`                                                       // 当前 worker 实例
	LeaseExpiresAt    *time.Time `gorm:"index:idx_task_lease,priority:2"`                                      // 租约到期时间
	HeartbeatAt       *time.Time `gorm:""`                                                                     // 最近一次租约心跳
	CancelRequestedAt *time.Time `gorm:"index"`                                                                // 运行中任务的取消请求
	CanceledAt        *time.Time `gorm:""`

	// 文件信息
	FileSize        int64   `gorm:"default:0"`           // 文件大小（字节）
	FileFingerprint string  `gorm:"size:64"`             // 创建任务时的文件元数据指纹
	IdempotencyKey  *string `gorm:"size:64;uniqueIndex"` // nullable，兼容旧数据并防止重复建任务

	// 日志与分析
	Log                 string `gorm:"type:text"` // 任务日志（简要汇总，可选）
	AccumulatedFailures int64  `gorm:"default:0"` // 累计失败次数（便于分析）

	CreatedAt time.Time `gorm:"index:idx_task_created_at;index:idx_task_status_created,priority:2"`
	UpdatedAt time.Time `gorm:"index:idx_task_status_updated,priority:2"`

	// 关联（不参与表结构，仅查询用）
	FileRecord *FileRecord `gorm:"foreignKey:FileRecordID"`
}

// TableName 指定表名。
func (UploadTask) TableName() string {
	return "upload_tasks"
}
