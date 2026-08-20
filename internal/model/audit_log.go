package model

import "time"

// AuditLog records security-sensitive API operations without request bodies or secrets.
type AuditLog struct {
	ID         uint64    `gorm:"primaryKey"`
	RequestID  string    `gorm:"size:64;index"`
	Actor      string    `gorm:"size:255;index"`
	Role       string    `gorm:"size:32"`
	Action     string    `gorm:"size:32;not null;index"`
	Resource   string    `gorm:"size:255;not null"`
	Method     string    `gorm:"size:16;not null"`
	Path       string    `gorm:"size:1024;not null"`
	StatusCode int       `gorm:"not null"`
	ClientIP   string    `gorm:"size:64"`
	CreatedAt  time.Time `gorm:"index"`
}

func (AuditLog) TableName() string {
	return "audit_logs"
}
