package api

import (
	"net/http"
	"strconv"

	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

type FileIndexHandler struct{ svc service.FileIndexService }

func NewFileIndexHandler(svc service.FileIndexService) *FileIndexHandler {
	return &FileIndexHandler{svc: svc}
}

// BrowseWatchFolder returns the persisted local scan index as a directory tree.
// @Summary      浏览监控目录文件索引
// @Tags         watch-folders
// @Produce      json
// @Param        id         path   int     true   "监控目录 ID"
// @Param        path       query  string  false  "相对目录路径"
// @Param        page       query  int     false  "页码" default(1)
// @Param        page_size  query  int     false  "每页条数" default(100)
// @Success      200        {object} service.FileBrowseResult
// @Failure      404        {object} map[string]string
// @Security     BearerAuth
// @Router       /api/watch-folders/{id}/files [get]
func (h *FileIndexHandler) BrowseWatchFolder(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id", "code": "validation_error"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	result, err := h.svc.BrowseWatchFolder(c.Request.Context(), uint(id), c.Query("path"), page, pageSize)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
