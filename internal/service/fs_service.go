package service

import (
	"context"
	"os"
	"path/filepath"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/security"
)

// FSDir 表示本地文件系统中的一个目录。
type FSDir struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	HasSubDirs bool   `json:"has_sub_dirs"`
}

// FSService 文件系统相关服务，仅封装本地目录的读取逻辑。
type FSService interface {
	// ListSubDirs 返回给定路径下的一级子目录列表。
	ListSubDirs(ctx context.Context, root string) ([]FSDir, error)
}

type fsService struct {
	policy *security.ResourcePolicy
}

// NewFSService 创建 FSService 实例。
func NewFSService(policy *security.ResourcePolicy) FSService {
	return &fsService{policy: policy}
}

func (s *fsService) ListSubDirs(ctx context.Context, root string) ([]FSDir, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	absRoot, err := s.policy.ValidateLocalDirectory(root)
	if err != nil {
		return nil, apperror.Validation("directory is invalid, inaccessible, or outside the allowlist", err)
	}
	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, apperror.Validation("directory cannot be read", err)
	}
	var dirs []FSDir
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return dirs, ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		if len(dirs) >= 5000 {
			return nil, apperror.Validation("directory contains too many subdirectories to list", nil)
		}
		name := entry.Name()
		fullPath := filepath.Join(absRoot, name)
		hasSub := hasSubDirs(fullPath)
		dirs = append(dirs, FSDir{
			Name:       name,
			Path:       fullPath,
			HasSubDirs: hasSub,
		})
	}
	return dirs, nil
}

// hasSubDirs 判断目录下是否存在子目录（仅检查一层）。
func hasSubDirs(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}
