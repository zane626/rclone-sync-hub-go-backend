package service

import (
	"context"

	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/security"
)

// RcloneService 负责与 rclone 配置相关的业务（不涉及 HTTP）。
type RcloneService interface {
	// ListConfigs 返回 rclone 中配置的 remote 列表（已清洗掉敏感信息）。
	ListConfigs(ctx context.Context) ([]rclone.Remote, error)
}

type rcloneService struct {
	client rclone.Client
	policy *security.ResourcePolicy
}

// NewRcloneService 创建 RcloneService。
func NewRcloneService(client rclone.Client, policy *security.ResourcePolicy) RcloneService {
	return &rcloneService{client: client, policy: policy}
}

func (s *rcloneService) ListConfigs(ctx context.Context) ([]rclone.Remote, error) {
	remotes, err := s.client.ListRemotes(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]rclone.Remote, 0, len(remotes))
	for _, remote := range remotes {
		if s.policy.AllowsRemote(remote.Name) {
			filtered = append(filtered, remote)
		}
	}
	return filtered, nil
}
