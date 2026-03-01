package api

import (
	"net/http"

	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

// RcloneHandler 处理 rclone 配置相关的 HTTP 请求。
type RcloneHandler struct {
	svc service.RcloneService
}

// NewRcloneHandler 创建 RcloneHandler。
func NewRcloneHandler(svc service.RcloneService) *RcloneHandler {
	return &RcloneHandler{svc: svc}
}

// ListConfigs 获取 rclone 配置的 remote 列表（只包含名称与类型等非敏感数据）。
// @Summary      获取 rclone remote 配置列表
// @Description  调用 `rclone config show` 并解析出 remote 名称与类型，过滤掉敏感字段
// @Tags         rclone
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "items 为 remote 列表"
// @Failure      500  {object}  map[string]string       "error 为错误信息"
// @Router       /api/rclone/configs [get]
func (h *RcloneHandler) ListConfigs(c *gin.Context) {
	ctx := c.Request.Context()
	remotes, err := h.svc.ListConfigs(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": remotes,
	})
}

