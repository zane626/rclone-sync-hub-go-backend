// Package config 负责从 yaml 加载配置，对外提供只读配置结构。
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Config 应用根配置。
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Scan        ScanConfig        `yaml:"scan"`
	Worker      WorkerConfig      `yaml:"worker"`
	Rclone      RcloneConfig      `yaml:"rclone"`
	Log         LogConfig         `yaml:"log"`
	Security    SecurityConfig    `yaml:"security"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Port             int      `yaml:"port"`
	Mode             string   `yaml:"mode"` // debug / release
	EmbedFrontend    bool     `yaml:"embed_frontend"`
	WriteTimeoutSecs int      `yaml:"write_timeout_seconds"`
	TrustedProxies   []string `yaml:"trusted_proxies"` // CIDRs explicitly trusted for forwarded client IPs
	// EnableSwagger 是否启用 Swagger 文档（建议仅开发环境 true，生产 false）
	EnableSwagger bool `yaml:"enable_swagger"`
}

// DatabaseConfig 数据库配置（当前为 MySQL；支持后期增加 driver 字段切换）。
type DatabaseConfig struct {
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	User                string `yaml:"user"`
	Password            string `yaml:"password"`
	DBName              string `yaml:"dbname"`
	Charset             string `yaml:"charset"`
	MaxOpenConns        int    `yaml:"max_open_conns"`
	MaxIdleConns        int    `yaml:"max_idle_conns"`
	ConnMaxIdleTimeMins int    `yaml:"conn_max_idle_time_mins"`
	ConnMaxLifetimeMins int    `yaml:"conn_max_lifetime_mins"`
	ConnectTimeoutSecs  int    `yaml:"connect_timeout_seconds"`
	ReadTimeoutSecs     int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSecs    int    `yaml:"write_timeout_seconds"`
	TLS                 string `yaml:"tls"`
}

// ScanConfig 定时扫描配置。上传目标使用任务表 upload_tasks 的 remote_name/remote_path，不再从此处读取。
type ScanConfig struct {
	IntervalSeconds          int `yaml:"interval_seconds"`            // 目录未单独配置时的默认扫描间隔
	WatchPollIntervalSeconds int `yaml:"watch_poll_interval_seconds"` // 检查到期监听目录的频率
	FolderTimeoutSeconds     int `yaml:"folder_timeout_seconds"`      // 单个目录扫描硬超时
	FileStableSeconds        int `yaml:"file_stable_seconds"`         // 文件最后修改后至少稳定多久才建任务
	MaxConcurrentFolders     int `yaml:"max_concurrent_folders"`      // 同时扫描的独立监听目录数
	BatchSize                int `yaml:"batch_size"`                  // 快照和任务批量写入大小
	LeaseSeconds             int `yaml:"lease_seconds"`               // 分布式扫描租约
	HeartbeatSeconds         int `yaml:"heartbeat_seconds"`           // 扫描租约续期频率
}

// WorkerConfig 任务队列与并发配置。
type WorkerConfig struct {
	MaxConcurrent            int    `yaml:"max_concurrent"`
	MaxRetry                 int    `yaml:"max_retry"`
	QueueSize                int    `yaml:"queue_size"` // 已弃用，仅为兼容旧配置保留；持久化任务队列不使用此值
	PollIntervalMilliseconds int    `yaml:"poll_interval_milliseconds"`
	LeaseSeconds             int    `yaml:"lease_seconds"`
	HeartbeatSeconds         int    `yaml:"heartbeat_seconds"`
	TaskTimeoutSeconds       int    `yaml:"task_timeout_seconds"`
	RetryBaseSeconds         int    `yaml:"retry_base_seconds"`
	RetryMaxSeconds          int    `yaml:"retry_max_seconds"`
	ProgressPersistSeconds   int    `yaml:"progress_persist_seconds"`
	InstanceID               string `yaml:"instance_id"`
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

// SecurityConfig controls authentication and resource allowlists.
type SecurityConfig struct {
	Enabled                bool     `yaml:"enabled"`
	AdminUsername          string   `yaml:"admin_username"`
	AdminPassword          string   `yaml:"admin_password"`
	ViewerUsername         string   `yaml:"viewer_username"`
	ViewerPassword         string   `yaml:"viewer_password"`
	TokenSecret            string   `yaml:"token_secret"`
	TokenTTLMinutes        int      `yaml:"token_ttl_minutes"`
	AllowedLocalRoots      []string `yaml:"allowed_local_roots"`
	AllowedRemotes         []string `yaml:"allowed_remotes"`
	MaxRequestBodyBytes    int64    `yaml:"max_request_body_bytes"`
	LoginAttemptsPerMinute int      `yaml:"login_attempts_per_minute"`
	MetricsToken           string   `yaml:"metrics_token"`
}

type MaintenanceConfig struct {
	IntervalHours          int `yaml:"interval_hours"`
	UploadLogRetentionDays int `yaml:"upload_log_retention_days"`
	TaskRetentionDays      int `yaml:"task_retention_days"`
	ScanRunRetentionDays   int `yaml:"scan_run_retention_days"`
	AuditLogRetentionDays  int `yaml:"audit_log_retention_days"`
	DeleteBatchSize        int `yaml:"delete_batch_size"`
}

// Load 从 path 加载 yaml 到 Config；path 为空或文件不存在时改为从环境变量加载（便于 Docker 部署无挂载 config）。
func Load(path string) (*Config, error) {
	if err := validateEnvironment(); err != nil {
		return nil, err
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var c Config
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			decoder.KnownFields(true)
			if err := decoder.Decode(&c); err != nil {
				return nil, fmt.Errorf("decode config %s: %w", path, err)
			}
			applyDefaults(&c)
			applyEnvOverrides(&c)
			return &c, c.Validate()
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	// 无配置文件时完全由环境变量 + 默认值构建
	return LoadFromEnv()
}

// LoadFromEnv 仅从环境变量与默认值构建 Config，不读任何文件。用于 Docker Compose 等纯 env 部署。
func LoadFromEnv() (*Config, error) {
	if err := validateEnvironment(); err != nil {
		return nil, err
	}
	c := &Config{}
	applyDefaults(c)
	applyEnvOverrides(c)
	return c, c.Validate()
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
	if v := os.Getenv("DB_CONN_MAX_LIFETIME_MINS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.ConnMaxLifetimeMins = n
		}
	}
	if v := os.Getenv("DB_CONNECT_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.ConnectTimeoutSecs = n
		}
	}
	if v := os.Getenv("DB_READ_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.ReadTimeoutSecs = n
		}
	}
	if v := os.Getenv("DB_WRITE_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.WriteTimeoutSecs = n
		}
	}
	if v := os.Getenv("DB_TLS"); v != "" {
		c.Database.TLS = v
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
	if v := os.Getenv("SERVER_WRITE_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.WriteTimeoutSecs = n
		}
	}
	if v := os.Getenv("EMBED_FRONTEND"); v != "" {
		c.Server.EmbedFrontend = parseBool(v)
	}
	if v := os.Getenv("ENABLE_SWAGGER"); v != "" {
		c.Server.EnableSwagger = parseBool(v)
	}
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		c.Server.TrustedProxies = splitCSV(v)
	}
	// Scan
	if v := os.Getenv("SCAN_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.IntervalSeconds = n
		}
	}
	if v := os.Getenv("SCAN_WATCH_POLL_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.WatchPollIntervalSeconds = n
		}
	}
	if v := os.Getenv("SCAN_FOLDER_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.FolderTimeoutSeconds = n
		}
	}
	if v := os.Getenv("SCAN_FILE_STABLE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.FileStableSeconds = n
		}
	}
	if v := os.Getenv("SCAN_MAX_CONCURRENT_FOLDERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.MaxConcurrentFolders = n
		}
	}
	if v := os.Getenv("SCAN_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.BatchSize = n
		}
	}
	if v := os.Getenv("SCAN_LEASE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.LeaseSeconds = n
		}
	}
	if v := os.Getenv("SCAN_HEARTBEAT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scan.HeartbeatSeconds = n
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
	if v := os.Getenv("WORKER_POLL_INTERVAL_MILLISECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.PollIntervalMilliseconds = n
		}
	}
	if v := os.Getenv("WORKER_LEASE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.LeaseSeconds = n
		}
	}
	if v := os.Getenv("WORKER_HEARTBEAT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.HeartbeatSeconds = n
		}
	}
	if v := os.Getenv("WORKER_TASK_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.TaskTimeoutSeconds = n
		}
	}
	if v := os.Getenv("WORKER_RETRY_BASE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.RetryBaseSeconds = n
		}
	}
	if v := os.Getenv("WORKER_RETRY_MAX_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.RetryMaxSeconds = n
		}
	}
	if v := os.Getenv("WORKER_PROGRESS_PERSIST_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Worker.ProgressPersistSeconds = n
		}
	}
	if v := os.Getenv("WORKER_INSTANCE_ID"); v != "" {
		c.Worker.InstanceID = v
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
	// Security
	if v := os.Getenv("AUTH_ENABLED"); v != "" {
		c.Security.Enabled = parseBool(v)
	}
	if v := os.Getenv("AUTH_ADMIN_USERNAME"); v != "" {
		c.Security.AdminUsername = v
	}
	if v := os.Getenv("AUTH_ADMIN_PASSWORD"); v != "" {
		c.Security.AdminPassword = v
	}
	if v := os.Getenv("AUTH_VIEWER_USERNAME"); v != "" {
		c.Security.ViewerUsername = v
	}
	if v := os.Getenv("AUTH_VIEWER_PASSWORD"); v != "" {
		c.Security.ViewerPassword = v
	}
	if v := os.Getenv("AUTH_TOKEN_SECRET"); v != "" {
		c.Security.TokenSecret = v
	}
	if v := os.Getenv("AUTH_TOKEN_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Security.TokenTTLMinutes = n
		}
	}
	if v := os.Getenv("ALLOWED_LOCAL_ROOTS"); v != "" {
		c.Security.AllowedLocalRoots = splitCSV(v)
	}
	if v := os.Getenv("ALLOWED_RCLONE_REMOTES"); v != "" {
		c.Security.AllowedRemotes = splitCSV(v)
	}
	if v := os.Getenv("MAX_REQUEST_BODY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.Security.MaxRequestBodyBytes = n
		}
	}
	if v := os.Getenv("LOGIN_ATTEMPTS_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Security.LoginAttemptsPerMinute = n
		}
	}
	if v := os.Getenv("METRICS_BEARER_TOKEN"); v != "" {
		c.Security.MetricsToken = v
	}
	if v := os.Getenv("MAINTENANCE_INTERVAL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.IntervalHours = n
		}
	}
	if v := os.Getenv("UPLOAD_LOG_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.UploadLogRetentionDays = n
		}
	}
	if v := os.Getenv("TASK_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.TaskRetentionDays = n
		}
	}
	if v := os.Getenv("SCAN_RUN_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.ScanRunRetentionDays = n
		}
	}
	if v := os.Getenv("AUDIT_LOG_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.AuditLogRetentionDays = n
		}
	}
	if v := os.Getenv("MAINTENANCE_DELETE_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Maintenance.DeleteBatchSize = n
		}
	}
}

// Validate rejects unsafe or internally inconsistent startup configuration.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Server.Mode != "debug" && c.Server.Mode != "release" && c.Server.Mode != "test" {
		return fmt.Errorf("server.mode must be debug, release, or test")
	}
	if c.Server.WriteTimeoutSecs < 1 || c.Server.WriteTimeoutSecs > 3600 {
		return fmt.Errorf("server.write_timeout_seconds must be between 1 and 3600")
	}
	if strings.TrimSpace(c.Database.Host) == "" || strings.TrimSpace(c.Database.User) == "" || strings.TrimSpace(c.Database.DBName) == "" {
		return fmt.Errorf("database host, user, and dbname are required")
	}
	if c.Server.Mode == "release" && c.Database.Password == "" {
		return fmt.Errorf("database.password is required in release mode")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("database.max_idle_conns cannot exceed max_open_conns")
	}
	if c.Database.MaxOpenConns < 2 {
		return fmt.Errorf("database.max_open_conns must be at least 2 so initial table creation can hold a dedicated advisory-lock connection")
	}
	if c.Database.MaxOpenConns > 500 {
		return fmt.Errorf("database.max_open_conns cannot exceed 500")
	}
	if c.Database.ConnectTimeoutSecs > 3600 || c.Database.ReadTimeoutSecs > 3600 || c.Database.WriteTimeoutSecs > 3600 {
		return fmt.Errorf("database connect/read/write timeouts cannot exceed 3600 seconds")
	}
	if c.Maintenance.TaskRetentionDays <= c.Maintenance.UploadLogRetentionDays {
		return fmt.Errorf("maintenance.task_retention_days must be longer than upload_log_retention_days")
	}
	if c.Maintenance.IntervalHours > 24*7 || c.Maintenance.UploadLogRetentionDays > 36500 || c.Maintenance.TaskRetentionDays > 36500 || c.Maintenance.ScanRunRetentionDays > 36500 || c.Maintenance.AuditLogRetentionDays > 36500 || c.Maintenance.DeleteBatchSize > 100000 {
		return fmt.Errorf("maintenance interval, retention, or batch size exceeds the supported limit")
	}
	if c.Worker.MaxConcurrent > 128 {
		return fmt.Errorf("worker.max_concurrent cannot exceed 128")
	}
	if c.Worker.LeaseSeconds > 86400 || c.Worker.HeartbeatSeconds > 86400 || c.Worker.TaskTimeoutSeconds > 7*24*60*60 || c.Worker.PollIntervalMilliseconds > 60000 || c.Worker.ProgressPersistSeconds > 300 {
		return fmt.Errorf("worker timing value exceeds the supported limit")
	}
	if c.Worker.HeartbeatSeconds <= 0 || c.Worker.HeartbeatSeconds*2 >= c.Worker.LeaseSeconds {
		return fmt.Errorf("worker.heartbeat_seconds must be less than half of lease_seconds")
	}
	if c.Worker.RetryMaxSeconds < c.Worker.RetryBaseSeconds {
		return fmt.Errorf("worker.retry_max_seconds cannot be less than retry_base_seconds")
	}
	if c.Worker.RetryMaxSeconds > 86400 || c.Worker.MaxRetry > 100 {
		return fmt.Errorf("worker retry configuration exceeds the supported limit")
	}
	if c.Scan.WatchPollIntervalSeconds > c.Scan.FolderTimeoutSeconds {
		return fmt.Errorf("scan.watch_poll_interval_seconds cannot exceed folder_timeout_seconds")
	}
	if c.Scan.IntervalSeconds > 7*24*60*60 || c.Scan.WatchPollIntervalSeconds > 3600 || c.Scan.FolderTimeoutSeconds > 7*24*60*60 || c.Scan.FileStableSeconds < 0 || c.Scan.FileStableSeconds > 86400 || c.Scan.LeaseSeconds > 86400 || c.Scan.HeartbeatSeconds > 86400 {
		return fmt.Errorf("scan timing value exceeds the supported limit")
	}
	if c.Scan.HeartbeatSeconds <= 0 || c.Scan.HeartbeatSeconds*2 >= c.Scan.LeaseSeconds {
		return fmt.Errorf("scan.heartbeat_seconds must be less than half of lease_seconds")
	}
	if c.Scan.MaxConcurrentFolders > 16 {
		return fmt.Errorf("scan.max_concurrent_folders cannot exceed 16")
	}
	if c.Scan.BatchSize > 5000 {
		return fmt.Errorf("scan.batch_size cannot exceed 5000")
	}
	if c.Log.Level != "debug" && c.Log.Level != "info" && c.Log.Level != "warn" && c.Log.Level != "error" {
		return fmt.Errorf("log.level must be debug, info, warn, or error")
	}
	if c.Log.Format != "json" && c.Log.Format != "console" {
		return fmt.Errorf("log.format must be json or console")
	}
	if c.Security.MetricsToken != "" && len(c.Security.MetricsToken) < 32 {
		return fmt.Errorf("METRICS_BEARER_TOKEN must contain at least 32 characters when configured")
	}
	if c.Security.TokenTTLMinutes > 30*24*60 || c.Security.MaxRequestBodyBytes > 10*1024*1024 || c.Security.LoginAttemptsPerMinute > 1000 {
		return fmt.Errorf("security token TTL, request body limit, or login rate limit exceeds the supported maximum")
	}
	if c.Server.Mode == "release" {
		if !c.Security.Enabled {
			return fmt.Errorf("authentication must be enabled in release mode")
		}
		if utf8.RuneCountInString(c.Security.AdminPassword) < 8 || len(c.Security.TokenSecret) < 32 {
			return fmt.Errorf("release mode requires an admin password of at least 8 characters and a token secret of at least 32 characters")
		}
	}
	return nil
}

func validateEnvironment() error {
	booleanNames := []string{"EMBED_FRONTEND", "ENABLE_SWAGGER", "AUTH_ENABLED"}
	for _, name := range booleanNames {
		value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
		if value == "" {
			continue
		}
		switch value {
		case "1", "0", "true", "false", "yes", "no":
		default:
			return fmt.Errorf("%s must be a boolean value", name)
		}
	}
	integerNames := []string{
		"DB_PORT", "DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_IDLE_TIME_MINS", "DB_CONN_MAX_LIFETIME_MINS",
		"DB_CONNECT_TIMEOUT_SECONDS", "DB_READ_TIMEOUT_SECONDS", "DB_WRITE_TIMEOUT_SECONDS", "SERVER_PORT", "SERVER_WRITE_TIMEOUT_SECONDS",
		"SCAN_INTERVAL_SECONDS", "SCAN_WATCH_POLL_INTERVAL_SECONDS", "SCAN_FOLDER_TIMEOUT_SECONDS", "SCAN_FILE_STABLE_SECONDS",
		"SCAN_MAX_CONCURRENT_FOLDERS", "SCAN_BATCH_SIZE", "SCAN_LEASE_SECONDS", "SCAN_HEARTBEAT_SECONDS",
		"WORKER_MAX_CONCURRENT", "WORKER_MAX_RETRY", "WORKER_QUEUE_SIZE", "WORKER_POLL_INTERVAL_MILLISECONDS",
		"WORKER_LEASE_SECONDS", "WORKER_HEARTBEAT_SECONDS", "WORKER_TASK_TIMEOUT_SECONDS", "WORKER_RETRY_BASE_SECONDS",
		"WORKER_RETRY_MAX_SECONDS", "WORKER_PROGRESS_PERSIST_SECONDS", "AUTH_TOKEN_TTL_MINUTES", "MAX_REQUEST_BODY_BYTES",
		"LOGIN_ATTEMPTS_PER_MINUTE", "MAINTENANCE_INTERVAL_HOURS", "UPLOAD_LOG_RETENTION_DAYS", "TASK_RETENTION_DAYS", "SCAN_RUN_RETENTION_DAYS",
		"AUDIT_LOG_RETENTION_DAYS", "MAINTENANCE_DELETE_BATCH_SIZE",
	}
	for _, name := range integerNames {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				return fmt.Errorf("%s must be an integer: %w", name, err)
			}
		}
	}
	return nil
}

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func applyDefaults(c *Config) {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "release"
	}
	if c.Server.WriteTimeoutSecs == 0 {
		c.Server.WriteTimeoutSecs = 900
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
	if c.Worker.PollIntervalMilliseconds <= 0 {
		c.Worker.PollIntervalMilliseconds = 1000
	}
	if c.Worker.LeaseSeconds <= 0 {
		c.Worker.LeaseSeconds = 90
	}
	if c.Worker.HeartbeatSeconds <= 0 {
		c.Worker.HeartbeatSeconds = c.Worker.LeaseSeconds / 3
	}
	if c.Worker.HeartbeatSeconds <= 0 {
		c.Worker.HeartbeatSeconds = 1
	}
	if c.Worker.TaskTimeoutSeconds <= 0 {
		c.Worker.TaskTimeoutSeconds = 4 * 60 * 60
	}
	if c.Worker.RetryBaseSeconds <= 0 {
		c.Worker.RetryBaseSeconds = 30
	}
	if c.Worker.RetryMaxSeconds <= 0 {
		c.Worker.RetryMaxSeconds = 30 * 60
	}
	if c.Worker.ProgressPersistSeconds <= 0 {
		c.Worker.ProgressPersistSeconds = 2
	}
	if c.Rclone.BinPath == "" {
		c.Rclone.BinPath = "rclone"
	}
	if c.Scan.IntervalSeconds <= 0 {
		c.Scan.IntervalSeconds = 300
	}
	if c.Scan.WatchPollIntervalSeconds <= 0 {
		c.Scan.WatchPollIntervalSeconds = 30
	}
	if c.Scan.FolderTimeoutSeconds <= 0 {
		c.Scan.FolderTimeoutSeconds = 1800
	}
	if c.Scan.FileStableSeconds == 0 {
		c.Scan.FileStableSeconds = 30
	}
	if c.Scan.MaxConcurrentFolders <= 0 {
		c.Scan.MaxConcurrentFolders = 2
	}
	if c.Scan.BatchSize <= 0 {
		c.Scan.BatchSize = 500
	}
	if c.Scan.LeaseSeconds <= 0 {
		c.Scan.LeaseSeconds = 90
	}
	if c.Scan.HeartbeatSeconds <= 0 {
		c.Scan.HeartbeatSeconds = c.Scan.LeaseSeconds / 3
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
	if c.Database.ConnMaxLifetimeMins <= 0 {
		c.Database.ConnMaxLifetimeMins = 30
	}
	if c.Database.ConnectTimeoutSecs <= 0 {
		c.Database.ConnectTimeoutSecs = 10
	}
	if c.Database.ReadTimeoutSecs <= 0 {
		c.Database.ReadTimeoutSecs = 30
	}
	if c.Database.WriteTimeoutSecs <= 0 {
		c.Database.WriteTimeoutSecs = 30
	}
	if c.Security.AdminUsername == "" {
		c.Security.AdminUsername = "admin"
	}
	if c.Security.TokenTTLMinutes <= 0 {
		c.Security.TokenTTLMinutes = 12 * 60
	}
	if c.Security.MaxRequestBodyBytes <= 0 {
		c.Security.MaxRequestBodyBytes = 1024 * 1024
	}
	if c.Security.LoginAttemptsPerMinute <= 0 {
		c.Security.LoginAttemptsPerMinute = 10
	}
	if c.Maintenance.IntervalHours <= 0 {
		c.Maintenance.IntervalHours = 24
	}
	if c.Maintenance.UploadLogRetentionDays <= 0 {
		c.Maintenance.UploadLogRetentionDays = 30
	}
	if c.Maintenance.TaskRetentionDays <= 0 {
		c.Maintenance.TaskRetentionDays = 365
	}
	if c.Maintenance.ScanRunRetentionDays <= 0 {
		c.Maintenance.ScanRunRetentionDays = 90
	}
	if c.Maintenance.AuditLogRetentionDays <= 0 {
		c.Maintenance.AuditLogRetentionDays = 180
	}
	if c.Maintenance.DeleteBatchSize <= 0 {
		c.Maintenance.DeleteBatchSize = 5000
	}
}
