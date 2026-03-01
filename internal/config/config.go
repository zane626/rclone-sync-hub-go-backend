// Package config 负责从 yaml 加载配置，对外提供只读配置结构。
package config

import (
	"os"
	"strconv"
	"strings"

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
	LocalPath       string `yaml:"local_path"`       // 要扫描的本地目录
	Enabled         bool   `yaml:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds"`  // 扫描间隔（秒），用于本地目录与 watch_folders 扫描，0 或未配置时默认 300
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

// Load 从 path 加载 yaml 到 Config；path 为空或文件不存在时改为从环境变量加载（便于 Docker 部署无挂载 config）。
func Load(path string) (*Config, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var c Config
			if err := yaml.Unmarshal(data, &c); err != nil {
				return nil, err
			}
			applyDefaults(&c)
			applyEnvOverrides(&c)
			return &c, nil
		}
	}
	// 无配置文件时完全由环境变量 + 默认值构建
	return LoadFromEnv(), nil
}

// LoadFromEnv 仅从环境变量与默认值构建 Config，不读任何文件。用于 Docker Compose 等纯 env 部署。
func LoadFromEnv() *Config {
	c := &Config{}
	applyDefaults(c)
	applyEnvOverrides(c)
	return c
}

// applyEnvOverrides 使用环境变量覆盖配置。无配置文件时 LoadFromEnv 依赖此函数填充全部字段。
// 环境变量命名：DB_* 数据库，SERVER_* 服务，SCAN_* 扫描，WORKER_*  worker，RCLONE_* / LOG_* 等。
func applyEnvOverrides(c *Config) {
	// Database
	if v := os.Getenv("DB_HOST"); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Database.Port = p
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		c.Database.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		c.Database.DBName = v
	}
	if v := os.Getenv("DB_CHARSET"); v != "" {
		c.Database.Charset = v
	}
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.MaxOpenConns = n
		}
	}
	if v := os.Getenv("DB_MAX_IDLE_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.MaxIdleConns = n
		}
	}
	if v := os.Getenv("DB_CONN_MAX_IDLE_TIME_MINS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.ConnMaxIdleTimeMins = n
		}
	}
	// Server
	if v := os.Getenv("SERVER_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.Port = n
		}
	}
	if v := os.Getenv("SERVER_MODE"); v != "" {
		c.Server.Mode = v
	}
	if v := os.Getenv("EMBED_FRONTEND"); v != "" {
		c.Server.EmbedFrontend = parseBool(v)
	}
	if v := os.Getenv("ENABLE_SWAGGER"); v != "" {
		c.Server.EnableSwagger = parseBool(v)
	}
	// Scan
	if v := os.Getenv("SCAN_LOCAL_PATH"); v != "" {
		c.Scan.LocalPath = v
	}
	if v := os.Getenv("SCAN_ENABLED"); v != "" {
		c.Scan.Enabled = parseBool(v)
	}
	if v := os.Getenv("SCAN_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.IntervalSeconds = n
		}
	}
	// Worker
	if v := os.Getenv("WORKER_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.MaxConcurrent = n
		}
	}
	if v := os.Getenv("WORKER_MAX_RETRY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.MaxRetry = n
		}
	}
	if v := os.Getenv("WORKER_QUEUE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.QueueSize = n
		}
	}
	// Rclone
	if v := os.Getenv("RCLONE_BIN_PATH"); v != "" {
		c.Rclone.BinPath = v
	}
	// Log
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.Log.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		c.Log.Format = v
	}
}

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes"
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
