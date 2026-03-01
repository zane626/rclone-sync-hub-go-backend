package service

import (
	"context"

	"rclone-sync-hub/internal/rclone"
)

// RcloneService 负责与 rclone 配置相关的业务（不涉及 HTTP）。
type RcloneService interface {
	// ListConfigs 返回 rclone 中配置的 remote 列表（已清洗掉敏感信息）。
	ListConfigs(ctx context.Context) ([]rclone.Remote, error)
}

type rcloneService struct {
	client rclone.Client
}

// NewRcloneService 创建 RcloneService。
func NewRcloneService(client rclone.Client) RcloneService {
	return &rcloneService{client: client}
}

func (s *rcloneService) ListConfigs(ctx context.Context) ([]rclone.Remote, error) {
	return s.client.ListRemotes(ctx)
}

