package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromEnvRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_NAME", "test")
	t.Setenv("AUTH_ENABLED", "treu")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected invalid AUTH_ENABLED to fail")
	}
}

func TestLoadFromEnvAppliesProductionDefaults(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_NAME", "test")
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("SERVER_MODE", "debug")
	config, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Scan.BatchSize != 500 || config.Scan.LeaseSeconds != 90 || config.Scan.HeartbeatSeconds != 30 {
		t.Fatalf("unexpected scan defaults: %+v", config.Scan)
	}
	if config.Database.ConnMaxLifetimeMins != 30 || config.Database.ConnectTimeoutSecs != 10 {
		t.Fatalf("unexpected database defaults: %+v", config.Database)
	}
	if config.Server.WriteTimeoutSecs != 900 {
		t.Fatalf("unexpected server write timeout: %d", config.Server.WriteTimeoutSecs)
	}
	if config.Maintenance.TaskRetentionDays != 365 {
		t.Fatalf("unexpected task retention default: %+v", config.Maintenance)
	}
}

func TestLoadFromEnvOverridesServerWriteTimeout(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_NAME", "test")
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("SERVER_MODE", "debug")
	t.Setenv("SERVER_WRITE_TIMEOUT_SECONDS", "1200")
	config, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Server.WriteTimeoutSecs != 1200 {
		t.Fatalf("unexpected server write timeout override: %d", config.Server.WriteTimeoutSecs)
	}
}

func TestValidateRejectsUnsafeConcurrency(t *testing.T) {
	config := &Config{}
	applyDefaults(config)
	config.Database.Host = "localhost"
	config.Database.User = "app"
	config.Database.DBName = "test"
	config.Server.Mode = "debug"
	config.Scan.MaxConcurrentFolders = 17
	if err := config.Validate(); err == nil {
		t.Fatal("expected excessive scan concurrency to fail")
	}
}

func TestValidateRequiresConnectionForSchemaInitializationLock(t *testing.T) {
	config := &Config{}
	applyDefaults(config)
	config.Database.Host = "localhost"
	config.Database.User = "app"
	config.Database.DBName = "test"
	config.Database.MaxOpenConns = 1
	config.Database.MaxIdleConns = 1
	config.Server.Mode = "debug"
	if err := config.Validate(); err == nil {
		t.Fatal("expected a one-connection pool to fail validation")
	}
}

func TestValidateRejectsTaskRetentionShorterThanLogs(t *testing.T) {
	config := &Config{}
	applyDefaults(config)
	config.Database.Host = "localhost"
	config.Database.User = "app"
	config.Database.DBName = "test"
	config.Server.Mode = "debug"
	config.Maintenance.UploadLogRetentionDays = 30
	config.Maintenance.TaskRetentionDays = 29
	if err := config.Validate(); err == nil {
		t.Fatal("expected task retention shorter than upload logs to fail")
	}
}

func TestExplicitUnsafeHeartbeatIsNotSilentlyCorrected(t *testing.T) {
	config := &Config{Worker: WorkerConfig{LeaseSeconds: 90, HeartbeatSeconds: 60}}
	applyDefaults(config)
	config.Database.Host = "localhost"
	config.Database.User = "app"
	config.Database.DBName = "test"
	config.Server.Mode = "debug"
	if config.Worker.HeartbeatSeconds != 60 {
		t.Fatalf("explicit heartbeat was silently changed to %d", config.Worker.HeartbeatSeconds)
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected unsafe explicit heartbeat to fail validation")
	}
}

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n  typo_field: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown YAML field to fail")
	}
}

func TestValidateAcceptsEightCharacterAdminPasswordInReleaseMode(t *testing.T) {
	config := &Config{}
	applyDefaults(config)
	config.Server.Mode = "release"
	config.Database.Host = "mysql"
	config.Database.User = "app"
	config.Database.Password = "database-password"
	config.Database.DBName = "test"
	config.Security.Enabled = true
	config.Security.AdminPassword = "12345678"
	config.Security.TokenSecret = strings.Repeat("s", 32)
	if err := config.Validate(); err != nil {
		t.Fatalf("eight-character admin password was rejected: %v", err)
	}
}

func TestValidateRejectsSevenCharacterAdminPasswordInReleaseMode(t *testing.T) {
	config := &Config{}
	applyDefaults(config)
	config.Server.Mode = "release"
	config.Database.Host = "mysql"
	config.Database.User = "app"
	config.Database.Password = "database-password"
	config.Database.DBName = "test"
	config.Security.Enabled = true
	config.Security.AdminPassword = "1234567"
	config.Security.TokenSecret = strings.Repeat("s", 32)
	if err := config.Validate(); err == nil {
		t.Fatal("seven-character admin password must be rejected")
	}
}
