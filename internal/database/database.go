// Package database 封装数据库连接与迁移，仅在此处依赖具体 driver；main 通过依赖注入获得 *gorm.DB。
// 支持后期通过 config.Driver 切换数据库（如 mysql / postgres）。
package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config 数据库配置（与 internal/config 解耦，由调用方传入所需字段，避免循环依赖）。
type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	Charset         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
}

// DSN 返回 MySQL DSN。
func DSN(c Config) string {
	charset := c.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.DBName, charset)
}

// OpenMySQL 使用 Gorm MySQL driver 打开连接并配置连接池；错误使用 %w 包装。
func OpenMySQL(cfg Config) (*gorm.DB, error) {
	dsn := DSN(cfg)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}

// Migrate 对 models 执行 AutoMigrate；错误使用 %w 包装。
func Migrate(db *gorm.DB, models ...interface{}) error {
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}
