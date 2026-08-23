package api

import (
	"net/http"
	"strconv"
	"strings"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

type RemoteRouteHandler struct {
	svc            service.RemoteRouteService
	moveOperations *remoteMoveOperationManager
}

// Keep the model reachable for swag's type resolver.
var _ = model.RemoteRoute{}

func NewRemoteRouteHandler(svc service.RemoteRouteService) *RemoteRouteHandler {
	return &RemoteRouteHandler{svc: svc, moveOperations: newRemoteMoveOperationManager(svc)}
}

type RemoteRouteCreateReq struct {
	Name                string `json:"name" binding:"required"`
	RemoteName          string `json:"remote_name" binding:"required"`
	RemotePath          string `json:"remote_path" binding:"required"`
	ScanIntervalSeconds int    `json:"scan_interval_seconds"`
	Enabled             *bool  `json:"enabled"`
}

type RemoteRouteUpdateReq struct {
	Name                *string `json:"name"`
	RemoteName          *string `json:"remote_name"`
	RemotePath          *string `json:"remote_path"`
	ScanIntervalSeconds *int    `json:"scan_interval_seconds"`
	Enabled             *bool   `json:"enabled"`
}

type RemoteFolderCreateReq struct {
	ParentPath string `json:"parent_path"`
	Name       string `json:"name" binding:"required"`
}

type RemoteFolderRenameReq struct {
	Path    string `json:"path" binding:"required"`
	NewName string `json:"new_name" binding:"required"`
}

type RemoteFilesMoveReq struct {
	SourcePaths  []string `json:"source_paths" binding:"required,min=1,max=100,dive,required"`
	TargetFolder string   `json:"target_folder"`
}

func remoteRouteID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id", "code": "validation_error"})
		return 0, false
	}
	return uint(id), true
}

// Create adds a reusable remote destination.
// @Summary      创建远端路由
// @Tags         remote-routes
// @Accept       json
// @Produce      json
// @Param        body  body      RemoteRouteCreateReq  true  "远端路由配置"
// @Success      201   {object}  model.RemoteRoute
// @Failure      400   {object}  map[string]string
// @Failure      409   {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes [post]
func (h *RemoteRouteHandler) Create(c *gin.Context) {
	var req RemoteRouteCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	route, err := h.svc.Create(c.Request.Context(), service.CreateRemoteRouteInput{
		Name: req.Name, RemoteName: req.RemoteName, RemotePath: req.RemotePath,
		ScanIntervalSeconds: req.ScanIntervalSeconds, Enabled: req.Enabled,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, route)
}

// List returns remote routes and their persisted scan state.
// @Summary      列出远端路由
// @Tags         remote-routes
// @Produce      json
// @Param        keyword    query  string  false  "名称或远端路径关键词"
// @Param        status     query  string  false  "pending|scanning|ready|error|disabled"
// @Param        page       query  int     false  "页码" default(1)
// @Param        page_size  query  int     false  "每页条数" default(20)
// @Success      200        {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /api/remote-routes [get]
func (h *RemoteRouteHandler) List(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	if len(keyword) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyword is too long", "code": "validation_error"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.svc.List(c.Request.Context(), keyword, c.Query("status"), page, pageSize)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// Get returns one remote route.
// @Summary      获取远端路由
// @Tags         remote-routes
// @Produce      json
// @Param        id  path  int  true  "路由 ID"
// @Success      200 {object} model.RemoteRoute
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id} [get]
func (h *RemoteRouteHandler) Get(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	route, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, route)
}

// Update changes a reusable route and safely invalidates linked destinations.
// @Summary      更新远端路由
// @Tags         remote-routes
// @Accept       json
// @Produce      json
// @Param        id    path  int                   true  "路由 ID"
// @Param        body  body  RemoteRouteUpdateReq  true  "远端路由配置"
// @Success      200   {object} model.RemoteRoute
// @Failure      409   {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id} [put]
func (h *RemoteRouteHandler) Update(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	var req RemoteRouteUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	route, err := h.svc.Update(c.Request.Context(), id, service.UpdateRemoteRouteInput{
		Name: req.Name, RemoteName: req.RemoteName, RemotePath: req.RemotePath,
		ScanIntervalSeconds: req.ScanIntervalSeconds, Enabled: req.Enabled,
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, route)
}

// Delete removes an unreferenced route and its index.
// @Summary      删除远端路由
// @Tags         remote-routes
// @Param        id  path  int  true  "路由 ID"
// @Success      200 {object} map[string]bool
// @Failure      409 {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id} [delete]
func (h *RemoteRouteHandler) Delete(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Scan schedules one route for asynchronous indexing.
// @Summary      立即扫描远端路由
// @Tags         remote-routes
// @Param        id  path  int  true  "路由 ID"
// @Success      202 {object} map[string]bool
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/scan [post]
func (h *RemoteRouteHandler) Scan(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	if err := h.svc.ScheduleScan(c.Request.Context(), id); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"scheduled": true})
}

// ScanAll schedules every enabled route for asynchronous indexing.
// @Summary      立即扫描全部远端路由
// @Tags         remote-routes
// @Success      202 {object} map[string]int64
// @Security     BearerAuth
// @Router       /api/remote-routes/scan [post]
func (h *RemoteRouteHandler) ScanAll(c *gin.Context) {
	count, err := h.svc.ScheduleAllScans(c.Request.Context())
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"scheduled": count})
}

// BrowseFiles returns one direct-child page from the persisted remote index.
// @Summary      浏览远端文件索引
// @Tags         remote-routes
// @Produce      json
// @Param        id         path   int     true   "路由 ID"
// @Param        path       query  string  false  "相对目录路径"
// @Param        page       query  int     false  "页码" default(1)
// @Param        page_size  query  int     false  "每页条数" default(100)
// @Success      200        {object} service.FileBrowseResult
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/files [get]
func (h *RemoteRouteHandler) BrowseFiles(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	result, err := h.svc.BrowseFiles(c.Request.Context(), id, c.Query("path"), page, pageSize)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateFolder creates a directory below one configured route root.
// @Summary      创建远端文件夹
// @Tags         remote-routes
// @Accept       json
// @Produce      json
// @Param        id    path  int                    true  "路由 ID"
// @Param        body  body  RemoteFolderCreateReq  true  "父目录与文件夹名称"
// @Success      201   {object} service.RemoteFolderMutationResult
// @Failure      400   {object} map[string]string
// @Failure      409   {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/folders [post]
func (h *RemoteRouteHandler) CreateFolder(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	var req RemoteFolderCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	result, err := h.svc.CreateFolder(c.Request.Context(), id, req.ParentPath, req.Name)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// RenameFolder renames one directory without allowing it to escape the route root.
// @Summary      重命名远端文件夹
// @Tags         remote-routes
// @Accept       json
// @Produce      json
// @Param        id    path  int                    true  "路由 ID"
// @Param        body  body  RemoteFolderRenameReq  true  "目录路径与新名称"
// @Success      200   {object} service.RemoteFolderMutationResult
// @Failure      400   {object} map[string]string
// @Failure      409   {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/folders [put]
func (h *RemoteRouteHandler) RenameFolder(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	var req RemoteFolderRenameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	result, err := h.svc.RenameFolder(c.Request.Context(), id, req.Path, req.NewName)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// MoveFiles starts an asynchronous move for up to 100 indexed remote files.
// @Summary      启动批量移动远端文件任务
// @Tags         remote-routes
// @Accept       json
// @Produce      json
// @Param        id    path  int                 true  "路由 ID"
// @Param        body  body  RemoteFilesMoveReq  true  "源文件路径与目标目录"
// @Success      202   {object} RemoteMoveOperation
// @Failure      400   {object} map[string]string
// @Failure      409   {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/files/move [post]
func (h *RemoteRouteHandler) MoveFiles(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	var req RemoteFilesMoveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	if _, err := h.svc.Get(c.Request.Context(), id); err != nil {
		writeAPIError(c, err)
		return
	}
	operation, err := h.moveOperations.Start(id, req.SourcePaths, req.TargetFolder)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, operation)
}

// GetMoveOperation returns the latest progress snapshot for one batch move.
// @Summary      查询批量移动远端文件进度
// @Tags         remote-routes
// @Produce      json
// @Param        id            path  int     true  "路由 ID"
// @Param        operation_id  path  string  true  "移动任务 ID"
// @Success      200  {object} RemoteMoveOperation
// @Failure      404  {object} map[string]string
// @Security     BearerAuth
// @Router       /api/remote-routes/{id}/files/move/{operation_id} [get]
func (h *RemoteRouteHandler) GetMoveOperation(c *gin.Context) {
	id, ok := remoteRouteID(c)
	if !ok {
		return
	}
	operationID := strings.TrimSpace(c.Param("operation_id"))
	if len(operationID) != 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid operation id", "code": "validation_error"})
		return
	}
	operation, err := h.moveOperations.Get(id, operationID)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, operation)
}
