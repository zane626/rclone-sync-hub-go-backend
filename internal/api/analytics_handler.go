// Package api 数据分析页接口：一次返回概览、图表维度与列表数据。
package api

import (
	"net/http"
	"strconv"

	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

// AnalyticsHandler 数据分析 API。
type AnalyticsHandler struct {
	svc service.AnalyticsService
}

// NewAnalyticsHandler 创建 AnalyticsHandler。
func NewAnalyticsHandler(svc service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc}
}

// GetDashboard 获取数据分析仪表盘全量数据
// @Summary      数据分析仪表盘
// @Description  返回概览数字、按状态/按监听文件夹/按时间趋势的图表数据，以及最近任务与失败任务列表，供数据分析页一次拉取
// @Tags         analytics
// @Accept       json
// @Produce      json
// @Param        days  query    int  false  "趋势图天数，默认 7"
// @Success      200  {object}  service.DashboardData
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/analytics/dashboard [get]
func (h *AnalyticsHandler) GetDashboard(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days cannot exceed 365", "code": "validation_error"})
		return
	}
	data, err := h.svc.GetDashboard(c.Request.Context(), days)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}
