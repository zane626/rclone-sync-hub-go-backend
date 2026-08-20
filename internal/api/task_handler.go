// Package api 仅处理 HTTP 请求与响应，不写业务逻辑，业务由 service 完成。
package api

import (
	"net/http"
	"strconv"
	"strings"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/service"

	"github.com/gin-gonic/gin"
)

// TaskHandler 任务相关 API。
type TaskHandler struct {
	svc service.UploadService
}

// Keep model types reachable for Swagger annotations in this file.
var _ = model.UploadTask{}

// NewTaskHandler 创建 TaskHandler。
func NewTaskHandler(svc service.UploadService) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// ListTasks 分页获取上传任务列表
// @Summary      分页获取上传任务列表
// @Description  按状态筛选并分页返回上传任务，不传 status 时返回全部；keyword 对 watch_folder_name/file_name/local_path/remote_name/remote_path 模糊查询
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        status    query    string  false  "任务状态: pending|running|success|failed|paused|canceled，空为全部"
// @Param        keyword   query    string  false  "关键词：对所属文件夹名/文件名/本地路径/网盘名/上传路径模糊查询"
// @Param        page      query    int     false  "页码，从 1 开始"     default(1)
// @Param        page_size query    int     false  "每页条数"            default(20)
// @Success      200  {object}  map[string]interface{}  "items 为任务列表，total 为总条数"
// @Failure      500  {object}  map[string]string       "error 为错误信息"
// @Security     BearerAuth
// @Router       /api/tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	status := c.DefaultQuery("status", "")
	keyword := strings.TrimSpace(c.DefaultQuery("keyword", ""))
	if len(keyword) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyword is too long", "code": "validation_error"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page <= 0 {
		page = 1
	}
	if page > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page is too large", "code": "validation_error"})
		return
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	list, total, err := h.svc.ListTasks(c.Request.Context(), status, keyword, page, pageSize)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"status":    status,
		"keyword":   keyword,
	})
}

// GetTask returns one upload task.
// @Summary      获取上传任务
// @Tags         tasks
// @Produce      json
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  model.UploadTask
// @Failure      404  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	task, err := h.svc.GetTask(c.Request.Context(), uint(id))
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

// GetTaskLogs 获取指定任务的上传日志（upload_logs 表，按时间倒序）
// @Summary      获取任务上传日志
// @Description  按任务 ID 返回 upload_logs 中的日志列表，支持 limit 限制条数
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id     path    int   true   "任务 ID"
// @Param        limit  query   int   false  "最多返回条数，默认 500"
// @Success      200  {array}  model.UploadLog
// @Failure      400  {object}  map[string]string  "invalid id"
// @Failure      404  {object}  map[string]string  "task not found"
// @Failure      500  {object}  map[string]string  "error"
// @Security     BearerAuth
// @Router       /api/tasks/{id}/logs [get]
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	logs, err := h.svc.GetTaskLogs(c.Request.Context(), uint(id), limit)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, logs)
}

// TriggerScan schedules all enabled folders for an asynchronous scan.
// @Summary      异步触发全部启用目录扫描
// @Tags         scanner
// @Produce      json
// @Success      202  {object}  map[string]int64
// @Security     BearerAuth
// @Router       /api/scan [post]
func (h *TaskHandler) TriggerScan(c *gin.Context) {
	enqueued, err := h.svc.TriggerScan(c.Request.Context())
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"enqueued": enqueued})
}

// GetStats returns task counts for every known status.
// @Summary      获取任务状态统计
// @Tags         tasks
// @Produce      json
// @Success      200  {object}  map[string]int64
// @Security     BearerAuth
// @Router       /api/stats [get]
func (h *TaskHandler) GetStats(c *gin.Context) {
	stats, err := h.svc.GetStats(c.Request.Context())
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, stats)
}

// SubmitTask retries an eligible durable task.
// @Summary      重试上传任务
// @Tags         tasks
// @Produce      json
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  map[string]bool
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/{id}/retry [post]
func (h *TaskHandler) SubmitTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.SubmitTask(c.Request.Context(), uint(id)); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TaskCreateReq 创建任务请求体。
type TaskCreateReq struct {
	WatchFolderID   uint   `json:"watch_folder_id"`
	WatchFolderName string `json:"watch_folder_name"`
	FileName        string `json:"file_name"`
	LocalPath       string `json:"local_path" binding:"required"`
	RemoteName      string `json:"remote_name" binding:"required"`
	RemotePath      string `json:"remote_path" binding:"required"`
	FileSize        int64  `json:"file_size"`
}

// TaskBatchReq 批量操作请求体。
type TaskBatchReq struct {
	IDs []uint `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"` // 任务 ID 列表
}

// CreateTask 创建上传任务。
// @Summary      创建上传任务
// @Description  新建一个上传任务（默认状态为待上传）
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      TaskCreateReq  true  "任务配置"
// @Success      200   {object}  model.UploadTask
// @Failure      400   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks [post]
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req TaskCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	in := service.CreateTaskInput{
		WatchFolderID:   req.WatchFolderID,
		WatchFolderName: req.WatchFolderName,
		FileName:        req.FileName,
		LocalPath:       req.LocalPath,
		RemoteName:      req.RemoteName,
		RemotePath:      req.RemotePath,
		FileSize:        req.FileSize,
	}
	task, err := h.svc.CreateTask(c.Request.Context(), in)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

// DeleteTask 删除上传任务。
// @Summary      删除上传任务
// @Description  上传中的任务不可删除
// @Tags         tasks
// @Produce      json
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  map[string]bool
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/{id} [delete]
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.DeleteTask(c.Request.Context(), uint(id)); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PauseTask 暂停上传任务。
// @Summary      暂停上传任务
// @Description  上传中的任务不可暂停
// @Tags         tasks
// @Produce      json
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  map[string]bool
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/{id}/pause [post]
func (h *TaskHandler) PauseTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.PauseTask(c.Request.Context(), uint(id)); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// CancelTask 取消等待中或运行中的上传任务。
// @Summary      取消上传任务
// @Description  等待中的任务立即取消；运行中的任务在 worker 心跳后终止 rclone 进程
// @Tags         tasks
// @Produce      json
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  map[string]bool
// @Security     BearerAuth
// @Router       /api/tasks/{id}/cancel [post]
func (h *TaskHandler) CancelTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.CancelTask(c.Request.Context(), uint(id)); err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BatchRetry 批量重试任务（将状态改为待上传并重新入队）。
// @Summary      批量重试上传任务
// @Description  对传入的任务 ID 列表执行重试操作，running 状态的任务会失败并给出原因
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      TaskBatchReq  true  "任务 ID 列表"
// @Success      200   {object}  service.TaskBatchResult
// @Failure      400   {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/batch/retry [post]
func (h *TaskHandler) BatchRetry(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	res := h.svc.BatchSubmitTasks(c.Request.Context(), req.IDs)
	c.JSON(http.StatusOK, res)
}

// BatchPause 批量暂停任务。
// @Summary      批量暂停上传任务
// @Description  对传入的任务 ID 列表执行暂停操作，running 状态的任务会失败并给出原因
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      TaskBatchReq  true  "任务 ID 列表"
// @Success      200   {object}  service.TaskBatchResult
// @Failure      400   {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/batch/pause [post]
func (h *TaskHandler) BatchPause(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	res := h.svc.BatchPauseTasks(c.Request.Context(), req.IDs)
	c.JSON(http.StatusOK, res)
}

// BatchDelete 批量删除任务。
// @Summary      批量删除上传任务
// @Description  对传入的任务 ID 列表执行删除操作，running 状态的任务会失败并给出原因
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      TaskBatchReq  true  "任务 ID 列表"
// @Success      200   {object}  service.TaskBatchResult
// @Failure      400   {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/tasks/batch/delete [post]
func (h *TaskHandler) BatchDelete(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	res := h.svc.BatchDeleteTasks(c.Request.Context(), req.IDs)
	c.JSON(http.StatusOK, res)
}

// BatchCancel 批量取消任务。
// @Summary      批量取消上传任务
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      TaskBatchReq  true  "任务 ID 列表"
// @Success      200   {object}  service.TaskBatchResult
// @Security     BearerAuth
// @Router       /api/tasks/batch/cancel [post]
func (h *TaskHandler) BatchCancel(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "validation_error"})
		return
	}
	c.JSON(http.StatusOK, h.svc.BatchCancelTasks(c.Request.Context(), req.IDs))
}
