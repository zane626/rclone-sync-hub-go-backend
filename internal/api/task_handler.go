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
// @Param        status    query    string  false  "任务状态: pending|running|success|failed，空为全部"
// @Param        keyword   query    string  false  "关键词：对所属文件夹名/文件名/本地路径/网盘名/上传路径模糊查询"
// @Param        page      query    int     false  "页码，从 1 开始"     default(1)
// @Param        page_size query    int     false  "每页条数"            default(20)
// @Success      200  {object}  map[string]interface{}  "items 为任务列表，total 为总条数"
// @Failure      500  {object}  map[string]string       "error 为错误信息"
// @Router       /api/tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	status := c.DefaultQuery("status", "")
	keyword := strings.TrimSpace(c.DefaultQuery("keyword", ""))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.svc.ListTasks(c.Request.Context(), status, keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

// GetTask  GET /api/tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	task, err := h.svc.GetTask(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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
// @Failure      500  {object}  map[string]string  "error"
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, logs)
}

// TriggerScan  POST /api/scan
func (h *TaskHandler) TriggerScan(c *gin.Context) {
	enqueued, err := h.svc.TriggerScan(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enqueued": enqueued})
}

// GetStats  GET /api/stats
func (h *TaskHandler) GetStats(c *gin.Context) {
	pending, running, success, failed, err := h.svc.GetStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		model.TaskStatusPending: pending,
		model.TaskStatusRunning: running,
		model.TaskStatusSuccess: success,
		model.TaskStatusFailed:  failed,
	})
}

// SubmitTask  POST /api/tasks/:id/retry  将任务重新入队
func (h *TaskHandler) SubmitTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.SubmitTask(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	IDs []uint `json:"ids" binding:"required"` // 任务 ID 列表
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
// @Router       /api/tasks [post]
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req TaskCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Router       /api/tasks/{id} [delete]
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.DeleteTask(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Router       /api/tasks/{id}/pause [post]
func (h *TaskHandler) PauseTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.PauseTask(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Router       /api/tasks/batch/retry [post]
func (h *TaskHandler) BatchRetry(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
// @Router       /api/tasks/batch/pause [post]
func (h *TaskHandler) BatchPause(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
// @Router       /api/tasks/batch/delete [post]
func (h *TaskHandler) BatchDelete(c *gin.Context) {
	var req TaskBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res := h.svc.BatchDeleteTasks(c.Request.Context(), req.IDs)
	c.JSON(http.StatusOK, res)
}
