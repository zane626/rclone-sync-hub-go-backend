package api

import (
	"net/http"

	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

// FSHandler 提供本地文件系统相关的只读接口，例如浏览子目录。
type FSHandler struct {
	svc service.FSService
}

// NewFSHandler 创建 FSHandler。
func NewFSHandler(svc service.FSService) *FSHandler {
	return &FSHandler{svc: svc}
}

// FSSubDir 表示返回给前端的子目录信息。
type FSSubDir struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	HasSubDirs bool   `json:"has_sub_dirs"`
}

// FSSubDirListResponse 返回结构。
type FSSubDirListResponse struct {
	Items []FSSubDir `json:"items"`
}

// ListSubDirs 获取某个路径下的所有子目录（仅一层）。
// @Summary      获取指定路径下的子文件夹
// @Description  根据传入的本地路径，返回该路径下所有一级子文件夹（不包含文件）
// @Tags         filesystem
// @Produce      json
// @Param        path  query    string  true  "本地起始路径，如 /volumes"
// @Success      200   {object} FSSubDirListResponse
// @Failure      400   {object} map[string]string
// @Failure      500   {object} map[string]string
// @Router       /api/fs/subdirs [get]
func (h *FSHandler) ListSubDirs(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	dirs, err := h.svc.ListSubDirs(c.Request.Context(), path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]FSSubDir, 0, len(dirs))
	for _, d := range dirs {
		items = append(items, FSSubDir{
			Name:       d.Name,
			Path:       d.Path,
			HasSubDirs: d.HasSubDirs,
		})
	}
	c.JSON(http.StatusOK, FSSubDirListResponse{Items: items})
}
