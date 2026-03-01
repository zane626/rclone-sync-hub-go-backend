package service

import (
	"context"
	"os"
	"path/filepath"
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

type fsService struct{}

// NewFSService 创建 FSService 实例。
func NewFSService() FSService {
	return &fsService{}
}

func (s *fsService) ListSubDirs(ctx context.Context, root string) ([]FSDir, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, err
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

