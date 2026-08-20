// Package api 路由注册与中间件，/api 前缀由调用方挂载。
package api

import "github.com/gin-gonic/gin"

// Router 注册 API 路由到 gin 的 /api 组。
func Router(
	r *gin.Engine,
	task *TaskHandler,
	health *HealthHandler,
	rcloneHandler *RcloneHandler,
	watchHandler *WatchFolderHandler,
	fsHandler *FSHandler,
	analyticsHandler *AnalyticsHandler,
	authHandler *AuthHandler,
	operationsHandler *OperationsHandler,
	eventHandler *EventHandler,
	securityMiddleware *SecurityMiddleware,
) {
	api := r.Group("/api")
	{
		api.GET("/health", health.Ping)
		api.GET("/health/live", health.Live)
		api.GET("/health/ready", health.Ready)
		api.GET("/auth/config", authHandler.Config)
		api.POST("/auth/login", authHandler.Login)
	}

	protected := api.Group("")
	protected.Use(securityMiddleware.Authenticate())
	protected.GET("/auth/me", authHandler.Me)
	protected.GET("/analytics/dashboard", analyticsHandler.GetDashboard)
	protected.GET("/stats", task.GetStats)
	protected.GET("/tasks", task.ListTasks)
	protected.GET("/tasks/:id", task.GetTask)
	protected.GET("/tasks/:id/logs", task.GetTaskLogs)
	protected.GET("/rclone/configs", rcloneHandler.ListConfigs)
	protected.GET("/watch-folders", watchHandler.List)
	protected.GET("/watch-folders/:id", watchHandler.Get)
	protected.GET("/fs/subdirs", fsHandler.ListSubDirs)
	protected.GET("/scan-runs", operationsHandler.ListScanRuns)
	protected.GET("/events", eventHandler.Stream)

	admin := protected.Group("")
	admin.Use(securityMiddleware.RequireAdmin())
	admin.POST("/tasks", task.CreateTask)
	admin.POST("/tasks/:id/retry", task.SubmitTask)
	admin.POST("/tasks/:id/pause", task.PauseTask)
	admin.POST("/tasks/:id/cancel", task.CancelTask)
	admin.DELETE("/tasks/:id", task.DeleteTask)
	admin.POST("/tasks/batch/retry", task.BatchRetry)
	admin.POST("/tasks/batch/pause", task.BatchPause)
	admin.POST("/tasks/batch/delete", task.BatchDelete)
	admin.POST("/tasks/batch/cancel", task.BatchCancel)
	admin.POST("/scan", task.TriggerScan)
	admin.POST("/watch-folders", watchHandler.Create)
	admin.PUT("/watch-folders/:id", watchHandler.Update)
	admin.DELETE("/watch-folders/:id", watchHandler.Delete)
	admin.GET("/audit-logs", operationsHandler.ListAuditLogs)
}
