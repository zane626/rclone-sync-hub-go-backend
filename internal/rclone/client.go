// Package rclone 封装 rclone 调用，项目内禁止在其他模块直接使用 exec。
// 支持 --progress，解析标准输出并返回结构化进度数据。
package rclone

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Progress 单次进度快照。兼容 rclone 两种输出：数字格式与 "Transferred: ..." 格式。
type Progress struct {
	Percent    float64 // 0-100
	BytesDone  int64
	BytesTotal int64
	Speed      int64  // bytes/s（可来自数字或解析 human 格式）
	Message    string // 原始行
	ETA        string // 预计剩余时间，如 "2m30s"（仅 Transferred 格式）
	CurrentStr string // 已传输量 human 显示，如 "1.234M"
	TotalStr   string // 总大小 human 显示，如 "10.5G"
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

// RemoteObject is one non-sensitive lsjson entry.
type RemoteObject struct {
	Path     string    `json:"Path"`
	Name     string    `json:"Name"`
	Size     int64     `json:"Size"`
	MimeType string    `json:"MimeType"`
	ModTime  time.Time `json:"ModTime"`
	IsDir    bool      `json:"IsDir"`
}

// Client 封装 rclone 命令行调用。
type Client interface {
	// Copy 使用 copyto 将一个本地文件上传到精确的远端文件路径。
	Copy(ctx context.Context, localPath, remoteName, remotePath string, onProgress func(Progress)) (Result, error)
	// ListRemotes 返回 rclone config 中配置的 remote 列表，仅包含 name 与 type 等非敏感信息。
	ListRemotes(ctx context.Context) ([]Remote, error)
	// WalkRemote streams every file and directory below one route. Streaming
	// avoids buffering a potentially very large remote listing in memory.
	WalkRemote(ctx context.Context, remoteName, remotePath string, visit func(RemoteObject) error) error
}

type client struct {
	binPath string
}

var (
	progressLine    = regexp.MustCompile(`^\s*(\d+)/(\d+),\s*([\d.]+)%`)
	transferredLine = regexp.MustCompile(`Transferred:\s+([\d.]+\s*[A-Za-z]+)\s*/\s*([\d.]+\s*[A-Za-z]+),\s*([\d.]+)%,\s*([\d.]+\s*[A-Za-z]+/s),\s*ETA\s+([^\s]+)`)
)

// NewClient 创建 rclone 客户端，binPath 为可执行文件路径（如 "rclone"）。
func NewClient(binPath string) Client {
	if binPath == "" {
		binPath = "rclone"
	}
	return &client{binPath: binPath}
}

// Copy 执行 rclone copyto --progress，解析输出并回调 onProgress。
func (c *client) Copy(ctx context.Context, localPath, remoteName, remotePath string, onProgress func(Progress)) (Result, error) {
	// copyto 的目标是精确文件路径，避免 copy 将文件名再解释为目录。
	dest := fmt.Sprintf("%s:%s", remoteName, strings.TrimPrefix(remotePath, "/"))
	cmd := exec.CommandContext(ctx, c.binPath, "copyto", localPath, dest,
		"--progress",
		"--stats=1s",
		"--use-server-modtime",
		"--no-traverse",
		"--timeout=4h",
		"--contimeout=10m",
		"--expect-continue-timeout=10m",
		"--low-level-retries=3",
		"--retries=1", // durable retries are managed by the MySQL task state machine
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

	errMsg := cappedBuffer{max: 64 * 1024}
	var errMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	// 两种进度格式：
	// 1) 数字格式：1234/5678, 22%, 1234, 12345/s, 0:00:30, ETA
	// 2) Transferred: 1.234M / 10.5G, 1%, 2.5 MB/s, ETA 2m30s
	emitProgress := func(raw string) bool {
		progress, ok := parseProgressLine(raw)
		if !ok {
			return false
		}
		if onProgress != nil {
			onProgress(progress)
		}
		return true
	}
	scanStream := func(reader io.Reader, captureError bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			isProgress := emitProgress(line)
			if captureError && !isProgress {
				errMu.Lock()
				errMsg.Append(line + "\n")
				errMu.Unlock()
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			errMu.Lock()
			errMsg.Append("read rclone output: " + scanErr.Error() + "\n")
			errMu.Unlock()
		}
	}

	go scanStream(stdout, false)
	go scanStream(stderr, true)

	wg.Wait()
	waitErr := cmd.Wait()
	if waitErr != nil {
		message := strings.TrimSpace(errMsg.String())
		if message == "" && ctx.Err() != nil {
			message = ctx.Err().Error()
		}
		return Result{Success: false, Error: message}, fmt.Errorf("rclone copyto: %w", waitErr)
	}
	return Result{Success: true}, nil
}

func parseProgressLine(raw string) (Progress, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return Progress{}, false
	}
	if match := transferredLine.FindStringSubmatch(line); len(match) >= 6 {
		percent, _ := strconv.ParseFloat(match[3], 64)
		done, _ := parseHumanBytes(match[1])
		total, _ := parseHumanBytes(match[2])
		speed, _ := parseHumanBytes(strings.TrimSuffix(match[4], "/s"))
		return Progress{
			Percent:    percent,
			BytesDone:  done,
			BytesTotal: total,
			Speed:      speed,
			Message:    line,
			ETA:        match[5],
			CurrentStr: strings.TrimSpace(match[1]),
			TotalStr:   strings.TrimSpace(match[2]),
		}, true
	}
	if match := progressLine.FindStringSubmatch(line); len(match) >= 4 {
		done, _ := strconv.ParseInt(match[1], 10, 64)
		total, _ := strconv.ParseInt(match[2], 10, 64)
		percent, _ := strconv.ParseFloat(match[3], 64)
		return Progress{Percent: percent, BytesDone: done, BytesTotal: total, Message: line}, true
	}
	return Progress{}, false
}

func parseHumanBytes(value string) (int64, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 {
		return 0, false
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || number < 0 {
		return 0, false
	}
	multipliers := map[string]float64{
		"B":  1,
		"KB": 1000, "MB": 1000 * 1000, "GB": 1000 * 1000 * 1000, "TB": 1000 * 1000 * 1000 * 1000,
		"KIB": 1 << 10, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
	}
	multiplier, ok := multipliers[strings.ToUpper(fields[1])]
	if !ok || number > float64(math.MaxInt64)/multiplier {
		return 0, false
	}
	return int64(number * multiplier), true
}

type cappedBuffer struct {
	data []byte
	max  int
}

func (b *cappedBuffer) Append(value string) {
	if b.max <= 0 || value == "" {
		return
	}
	incoming := []byte(value)
	if len(incoming) >= b.max {
		b.data = append(b.data[:0], incoming[len(incoming)-b.max:]...)
		return
	}
	if overflow := len(b.data) + len(incoming) - b.max; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, incoming...)
}

func (b *cappedBuffer) String() string {
	return string(b.data)
}

// ListRemotes uses listremotes --long so credentials never enter this process's output buffer.
func (c *client) ListRemotes(ctx context.Context) ([]Remote, error) {
	cmd := exec.CommandContext(ctx, c.binPath, "listremotes", "--long")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rclone listremotes: %w", err)
	}
	return parseListRemotesLong(string(out)), nil
}

func (c *client) WalkRemote(ctx context.Context, remoteName, remotePath string, visit func(RemoteObject) error) error {
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	destination := fmt.Sprintf("%s:%s", remoteName, strings.TrimPrefix(remotePath, "/"))
	cmd := exec.CommandContext(listCtx, c.binPath, "lsjson", destination, "--recursive")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("rclone lsjson stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("rclone lsjson stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rclone lsjson start: %w", err)
	}
	diagnostics := cappedBuffer{max: 64 * 1024}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			diagnostics.Append(scanner.Text() + "\n")
		}
		if scanErr := scanner.Err(); scanErr != nil {
			diagnostics.Append("read rclone lsjson stderr: " + scanErr.Error())
		}
	}()

	decoder := json.NewDecoder(stdout)
	var walkErr error
	opening, tokenErr := decoder.Token()
	if tokenErr != nil {
		walkErr = fmt.Errorf("decode rclone lsjson opening token: %w", tokenErr)
	} else if delimiter, ok := opening.(json.Delim); !ok || delimiter != '[' {
		walkErr = errors.New("rclone lsjson returned a non-array response")
	}
	for walkErr == nil && decoder.More() {
		var object RemoteObject
		if err := decoder.Decode(&object); err != nil {
			walkErr = fmt.Errorf("decode rclone lsjson entry: %w", err)
			break
		}
		if visit != nil {
			if err := visit(object); err != nil {
				walkErr = err
				break
			}
		}
	}
	if walkErr == nil {
		if _, err := decoder.Token(); err != nil {
			walkErr = fmt.Errorf("decode rclone lsjson closing token: %w", err)
		}
	}
	if walkErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	<-stderrDone
	if walkErr != nil {
		return walkErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(diagnostics.String())
		if message == "" {
			message = waitErr.Error()
		}
		return fmt.Errorf("rclone lsjson: %s", message)
	}
	return nil
}

func parseListRemotesLong(text string) []Remote {
	var remotes []Remote
	for _, raw := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		if name == "" {
			continue
		}
		remote := Remote{Name: name}
		if len(fields) > 1 {
			remote.Type = fields[1]
		}
		remotes = append(remotes, remote)
	}
	return remotes
}
