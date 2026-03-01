package api

import (
	"net/http"
	"strconv"
	"strings"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

// WatchFolderHandler 监听文件夹 CRUD 接口，仅做 HTTP 适配。
type WatchFolderHandler struct {
	svc service.WatchFolderService
}

// NewWatchFolderHandler 创建 WatchFolderHandler。
func NewWatchFolderHandler(svc service.WatchFolderService) *WatchFolderHandler {
	return &WatchFolderHandler{svc: svc}
}

// WatchFolderCreateReq 创建监听文件夹的请求体。
type WatchFolderCreateReq struct {
	Name                string `json:"name" binding:"required"`
	LocalPath           string `json:"local_path" binding:"required"`
	RemoteName          string `json:"remote_name" binding:"required"`
	RemotePath          string `json:"remote_path" binding:"required"`
	SyncType            string `json:"sync_type"`              // 可选，默认 local_to_remote
	MaxDepth            int    `json:"max_depth"`              // 可选，0 表示不限制
	FilterKeywords      string `json:"filter_keywords"`       // 可选，多行关键字，换行分隔，扫描时模糊匹配排除
	ScanIntervalSecond  int    `json:"scan_interval_seconds"` // 可选，默认 300
}

// WatchFolderUpdateReq 更新监听文件夹的请求体（全部可选）。
type WatchFolderUpdateReq struct {
	Name               *string `json:"name"`
	LocalPath          *string `json:"local_path"`
	RemoteName         *string `json:"remote_name"`
	RemotePath         *string `json:"remote_path"`
	SyncType           *string `json:"sync_type"`
	MaxDepth           *int    `json:"max_depth"`
	FilterKeywords     *string `json:"filter_keywords"`
	ScanIntervalSecond *int    `json:"scan_interval_seconds"`
	Status             *string `json:"status"`
	Enabled            *bool   `json:"enabled"`
}

// nolint:deadcode,unused
// 让 swag 能正确找到 model.WatchFolder 类型，同时避免编译器报未使用。
var _ = model.WatchFolder{}

// Create 创建监听文件夹。
// @Summary      创建监听文件夹
// @Description  新增一个被监听并同步到指定 rclone remote 的本地目录
// @Tags         watch-folders
// @Accept       json
// @Produce      json
// @Param        body  body      WatchFolderCreateReq  true  "监听文件夹配置"
// @Success      200   {object}  model.WatchFolder
// @Failure      400   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /api/watch-folders [post]
func (h *WatchFolderHandler) Create(c *gin.Context) {
	var req WatchFolderCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := service.CreateWatchFolderInput{
		Name:               req.Name,
		LocalPath:          req.LocalPath,
		RemoteName:         req.RemoteName,
		RemotePath:         req.RemotePath,
		SyncType:           req.SyncType,
		MaxDepth:           req.MaxDepth,
		FilterKeywords:     req.FilterKeywords,
		ScanIntervalSecond: req.ScanIntervalSecond,
	}
	f, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, f)
}

// List 列出监听文件夹。
// @Summary      列出监听文件夹
// @Description  按状态分页获取监听文件夹列表
// @Tags         watch-folders
// @Produce      json
// @Param        status     query    string  false  "状态: detecting|watching|stopped|paused|error"
// @Param        keyword    query    string  false  "关键词：对 name/local_path/remote_name/remote_path 模糊查询"
// @Param        page       query    int     false  "页码，从 1 开始"     default(1)
// @Param        page_size  query    int     false  "每页条数"           default(20)
// @Success      200        {object} map[string]interface{}  "items 为列表，total 为总数"
// @Failure      500        {object} map[string]string
// @Router       /api/watch-folders [get]
func (h *WatchFolderHandler) List(c *gin.Context) {
	status := c.DefaultQuery("status", "")
	keyword := strings.TrimSpace(c.DefaultQuery("keyword", ""))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.svc.List(c.Request.Context(), status, keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"status":    status,
		"keyword":   keyword,
	})
}

// Get 获取单个监听文件夹。
// @Summary      获取监听文件夹详情
// @Tags         watch-folders
// @Produce      json
// @Param        id   path      int  true  "监听文件夹 ID"
// @Success      200  {object}  model.WatchFolder
// @Failure      404  {object}  map[string]string
// @Router       /api/watch-folders/{id} [get]
func (h *WatchFolderHandler) Get(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	f, err := h.svc.Get(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, f)
}

// Update 更新监听文件夹。
// @Summary      更新监听文件夹
// @Tags         watch-folders
// @Accept       json
// @Produce      json
// @Param        id    path      int                  true  "监听文件夹 ID"
// @Param        body  body      WatchFolderUpdateReq true  "更新内容（全部可选）"
// @Success      200   {object}  model.WatchFolder
// @Failure      400   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /api/watch-folders/{id} [put]
func (h *WatchFolderHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req WatchFolderUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := service.UpdateWatchFolderInput{
		Name:               req.Name,
		LocalPath:          req.LocalPath,
		RemoteName:         req.RemoteName,
		RemotePath:         req.RemotePath,
		SyncType:           req.SyncType,
		MaxDepth:           req.MaxDepth,
		FilterKeywords:     req.FilterKeywords,
		ScanIntervalSecond: req.ScanIntervalSecond,
		Status:             req.Status,
		Enabled:            req.Enabled,
	}
	f, err := h.svc.Update(c.Request.Context(), uint(id), in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, f)
}

// Delete 删除监听文件夹。
// @Summary      删除监听文件夹
// @Tags         watch-folders
// @Produce      json
// @Param        id   path      int  true  "监听文件夹 ID"
// @Success      200  {object}  map[string]bool
// @Failure      500  {object}  map[string]string
// @Router       /api/watch-folders/{id} [delete]
func (h *WatchFolderHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
