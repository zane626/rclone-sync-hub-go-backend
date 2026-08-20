package api

import (
	"net/http"
	"strconv"

	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

type OperationsHandler struct {
	service service.OperationsService
}

func NewOperationsHandler(service service.OperationsService) *OperationsHandler {
	return &OperationsHandler{service: service}
}

// ListScanRuns returns persisted scanner execution history.
// @Summary      获取扫描历史
// @Tags         operations
// @Produce      json
// @Param        watch_folder_id  query  int  false  "监听目录 ID；0 为全部"
// @Param        page             query  int  false  "页码" default(1)
// @Param        page_size        query  int  false  "每页条数" default(50)
// @Success      200  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /api/scan-runs [get]
func (h *OperationsHandler) ListScanRuns(c *gin.Context) {
	page, pageSize := operationPage(c)
	folderID, parseErr := strconv.ParseUint(c.DefaultQuery("watch_folder_id", "0"), 10, 32)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid watch_folder_id", "code": "validation_error"})
		return
	}
	items, total, err := h.service.ListScanRuns(c.Request.Context(), uint(folderID), page, pageSize)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// ListAuditLogs returns mutation audit records and requires an administrator.
// @Summary      获取操作审计日志
// @Tags         operations
// @Produce      json
// @Param        page       query  int  false  "页码" default(1)
// @Param        page_size  query  int  false  "每页条数" default(50)
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/audit-logs [get]
func (h *OperationsHandler) ListAuditLogs(c *gin.Context) {
	page, pageSize := operationPage(c)
	items, total, err := h.service.ListAuditLogs(c.Request.Context(), page, pageSize)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func operationPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page <= 0 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
