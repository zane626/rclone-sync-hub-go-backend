// Package config 负责从 yaml 加载配置，对外提供只读配置结构。
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 应用根配置。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Scan     ScanConfig     `yaml:"scan"`
	Worker   WorkerConfig   `yaml:"worker"`
	Rclone   RcloneConfig   `yaml:"rclone"`
	Log      LogConfig      `yaml:"log"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Port          int    `yaml:"port"`
	Mode          string `yaml:"mode"` // debug / release
	EmbedFrontend bool   `yaml:"embed_frontend"`
	// EnableSwagger 是否启用 Swagger 文档（建议仅开发环境 true，生产 false）
	EnableSwagger bool `yaml:"enable_swagger"`
}

// DatabaseConfig 数据库配置（当前为 MySQL；支持后期增加 driver 字段切换）。
type DatabaseConfig struct {
	Host                 string `yaml:"host"`
	Port                 int    `yaml:"port"`
	User                 string `yaml:"user"`
	Password             string `yaml:"password"`
	DBName               string `yaml:"dbname"`
	Charset              string `yaml:"charset"`
	MaxOpenConns         int    `yaml:"max_open_conns"`
	MaxIdleConns         int    `yaml:"max_idle_conns"`
	ConnMaxIdleTimeMins  int    `yaml:"conn_max_idle_time_mins"`
}

// ScanConfig 定时扫描配置。上传目标使用任务表 upload_tasks 的 remote_name/remote_path，不再从此处读取。
type ScanConfig struct {
	LocalPath string `yaml:"local_path"` // 要扫描的本地目录
	// CronSchedule 与 Enabled、IntervalSeconds 用于本地目录扫描与 watch_folders 扫描
	CronSchedule    string `yaml:"cron_schedule"`    // 如 "*/5 * * * *" 每 5 分钟
	Enabled         bool   `yaml:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds"` // 扫描间隔（秒），0 或未配置时默认 300
}

// WorkerConfig 任务队列与并发配置。
type WorkerConfig struct {
	MaxConcurrent int  `yaml:"max_concurrent"`
	MaxRetry      int  `yaml:"max_retry"`
	QueueSize     int  `yaml:"queue_size"`
}

// RcloneConfig rclone 可执行路径等。
type RcloneConfig struct {
	BinPath string `yaml:"bin_path"` // 默认 "rclone"
}

// LogConfig 日志配置。
type LogConfig struct {
	Level  string `yaml:"level"`  // debug / info / warn / error
	Format string `yaml:"format"` // json / console
}

// Load 从 path 加载 yaml 到 Config，失败则返回错误。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	applyDefaults(&c)
	return &c, nil
}

func applyDefaults(c *Config) {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "release"
	}
	// embed_frontend: 生产设为 true 可嵌入 Vue 静态资源
	if c.Worker.MaxConcurrent <= 0 {
		c.Worker.MaxConcurrent = 3
	}
	if c.Worker.MaxRetry <= 0 {
		c.Worker.MaxRetry = 3
	}
	if c.Worker.QueueSize <= 0 {
		c.Worker.QueueSize = 1000
	}
	if c.Rclone.BinPath == "" {
		c.Rclone.BinPath = "rclone"
	}
	if c.Scan.CronSchedule == "" {
		c.Scan.CronSchedule = "*/5 * * * *"
	}
	if c.Scan.IntervalSeconds <= 0 {
		c.Scan.IntervalSeconds = 300
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}
	if c.Database.Port == 0 {
		c.Database.Port = 3306
	}
	if c.Database.Charset == "" {
		c.Database.Charset = "utf8mb4"
	}
	if c.Database.MaxOpenConns <= 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns <= 0 {
		c.Database.MaxIdleConns = 10
	}
	if c.Database.ConnMaxIdleTimeMins <= 0 {
		c.Database.ConnMaxIdleTimeMins = 10
	}
}
