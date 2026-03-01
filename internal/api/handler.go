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
) {
	api := r.Group("/api")
	{
		api.GET("/health", health.Ping)
		api.GET("/stats", task.GetStats)
		api.GET("/tasks", task.ListTasks)
		api.GET("/tasks/:id", task.GetTask)
		api.POST("/tasks", task.CreateTask)
		api.POST("/tasks/:id/retry", task.SubmitTask)
		api.POST("/tasks/:id/pause", task.PauseTask)
		api.DELETE("/tasks/:id", task.DeleteTask)
		api.POST("/tasks/batch/retry", task.BatchRetry)
		api.POST("/tasks/batch/pause", task.BatchPause)
		api.POST("/tasks/batch/delete", task.BatchDelete)
		api.POST("/scan", task.TriggerScan)
		api.GET("/rclone/configs", rcloneHandler.ListConfigs)

		api.POST("/watch-folders", watchHandler.Create)
		api.GET("/watch-folders", watchHandler.List)
		api.GET("/watch-folders/:id", watchHandler.Get)
		api.PUT("/watch-folders/:id", watchHandler.Update)
		api.DELETE("/watch-folders/:id", watchHandler.Delete)

		api.GET("/fs/subdirs", fsHandler.ListSubDirs)
	}
}
