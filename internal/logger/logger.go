// Package logger 封装 zap，提供全局 Logger 与初始化。
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// L 全局 Logger，在 Init 后使用。
	L *zap.Logger
)

// Init 根据 level 和 format 初始化全局 Logger。
// level: debug / info / warn / error
// format: json / console
func Init(level, format string) error {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}
	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      false,
		Encoding:         format,
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	if format == "console" {
		cfg.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	}
	var err error
	L, err = cfg.Build()
	if err != nil {
		return err
	}
	return nil
}

// Sync 刷新缓冲，应在程序退出前调用。
func Sync() {
	if L != nil {
		_ = L.Sync()
	}
}
