package config

import (
	"os"
	"strconv"
)

// GetMaxConcurrentUploads 从环境变量 MAX_CONCURRENT_UPLOADS 读取最大并发上传数。
// 未设置或非法时返回默认值 5。
func GetMaxConcurrentUploads() int {
	v := os.Getenv("MAX_CONCURRENT_UPLOADS")
	if v == "" {
		return 5
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 5
	}
	return n
}

