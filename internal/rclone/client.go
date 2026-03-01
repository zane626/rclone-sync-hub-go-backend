// Package rclone 封装 rclone 调用，项目内禁止在其他模块直接使用 exec。
// 支持 --progress，解析标准输出并返回结构化进度数据。
package rclone

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Progress 单次进度快照。
type Progress struct {
	Percent   float64 // 0-100
	BytesDone int64
	BytesTotal int64
	Speed     int64   // bytes/s
	Message   string
}

// Result 一次 copy 的最终结果。
type Result struct {
	Success bool
	Error   string
}

// Remote 表示 rclone 中的一个 remote 配置（经清洗，不包含敏感字段）。
type Remote struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Client 封装 rclone 命令行调用。
type Client interface {
	// Copy 执行 rclone copy，通过 progress 回调实时上报进度，ctx 取消可终止命令。
	Copy(ctx context.Context, localPath, remoteName, remotePath string, onProgress func(Progress)) (Result, error)
	// ListRemotes 返回 rclone config 中配置的 remote 列表，仅包含 name 与 type 等非敏感信息。
	ListRemotes(ctx context.Context) ([]Remote, error)
	// FileExists 使用 rclone lsf 判断远端文件是否存在。
	FileExists(ctx context.Context, remoteName, remotePath string) (bool, error)
}

type client struct {
	binPath string
}

// NewClient 创建 rclone 客户端，binPath 为可执行文件路径（如 "rclone"）。
func NewClient(binPath string) Client {
	if binPath == "" {
		binPath = "rclone"
	}
	return &client{binPath: binPath}
}

// Copy 执行 rclone copy --progress，解析 stdout 并回调 onProgress。
func (c *client) Copy(ctx context.Context, localPath, remoteName, remotePath string, onProgress func(Progress)) (Result, error) {
	// rclone copy <localPath> <remote>:<remotePath> --progress；每个 flag 单独传参以便正确解析
	dest := fmt.Sprintf("%s:%s", remoteName, strings.TrimPrefix(remotePath, "/"))
	cmd := exec.CommandContext(ctx, c.binPath, "copy", localPath, dest,
		"--progress",
		"--use-server-modtime",
		"--no-traverse",
		"--timeout=4h",
		"--contimeout=10m",
		"--expect-continue-timeout=10m",
		"--low-level-retries=10",
		"--retries=5",
		"--retries-sleep=30s",
		"--max-depth", "-1", // 递归深度限制，-1 表示不限制
		"-v",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Success: false, Error: err.Error()}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{Success: false, Error: err.Error()}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{Success: false, Error: err.Error()}, err
	}

	var errMsg strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	// 解析 --progress 输出格式，例如：
	// 1234/5678, 22%, 1234, 12345/s, 0:00:30, ETA
	progressLine := regexp.MustCompile(`^\s*(\d+)/(\d+),\s*(\d+)%`)

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			if onProgress != nil {
				if m := progressLine.FindStringSubmatch(line); len(m) >= 4 {
					done, _ := strconv.ParseInt(m[1], 10, 64)
					total, _ := strconv.ParseInt(m[2], 10, 64)
					pct, _ := strconv.ParseFloat(m[3], 64)
					onProgress(Progress{
						Percent:   pct,
						BytesDone: done,
						BytesTotal: total,
						Message:   line,
					})
				} else if strings.TrimSpace(line) != "" {
					onProgress(Progress{Message: line})
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			errMsg.WriteString(sc.Text())
			errMsg.WriteString("\n")
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()
	if waitErr != nil {
		return Result{Success: false, Error: strings.TrimSpace(errMsg.String())}, waitErr
	}
	return Result{Success: true}, nil
}

// ListRemotes 调用 `rclone config show` 并解析 remote 名称与 type，过滤掉敏感字段。
func (c *client) ListRemotes(ctx context.Context) ([]Remote, error) {
	// rclone config show
	cmd := exec.CommandContext(ctx, c.binPath, "config", "show")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rclone config show: %w", err)
	}
	return parseConfigShow(string(out)), nil
}

// FileExists 调用 `rclone lsf remote:path` 判断远程文件是否存在。
// remotePath 为 remote 内部的路径（不包含 remoteName），例如 /backup/a.txt。
func (c *client) FileExists(ctx context.Context, remoteName, remotePath string) (bool, error) {
	dest := fmt.Sprintf("%s:%s", remoteName, strings.TrimPrefix(remotePath, "/"))
	cmd := exec.CommandContext(ctx, c.binPath, "lsf", dest, "--files-only")
	out, err := cmd.Output()
	if err != nil {
		// rclone 对于不存在的文件返回非 0，按“文件不存在”处理即可。
		return false, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	return true, nil
}

// parseConfigShow 解析 rclone config show 的输出，只提取 [name] 与 type = xxx。
func parseConfigShow(text string) []Remote {
	var remotes []Remote
	var current *Remote

	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// [remoteName]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			if name == "" {
				continue
			}
			remotes = append(remotes, Remote{Name: name})
			current = &remotes[len(remotes)-1]
			continue
		}
		// type = s3
		if current != nil && strings.HasPrefix(line, "type") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				current.Type = strings.TrimSpace(parts[1])
			}
		}
	}
	return remotes
}
