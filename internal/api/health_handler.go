package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler 健康检查。
type HealthHandler struct{}

// NewHealthHandler 创建 HealthHandler。
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Ping  GET /api/health
func (h *HealthHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
