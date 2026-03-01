package model

import "time"

// UploadLog 上传过程日志表 upload_logs，用于记录进度与历史。
type UploadLog struct {
	ID        uint      `gorm:"primaryKey"`
	TaskID    uint      `gorm:"not null;index"`
	Percent   float64   `gorm:"type:decimal(5,2)"`
	BytesDone int64     `gorm:"default:0"`
	BytesTotal int64    `gorm:"default:0"`
	Speed     int64     `gorm:"default:0"` // bytes/s
	Message   string    `gorm:"type:text"`
	CreatedAt time.Time `gorm:""`
}

// TableName 指定表名。
func (UploadLog) TableName() string {
	return "upload_logs"
}
