// Package service 数据分析服务：聚合 watch_folders 与 upload_tasks，供仪表盘与图表使用。
package service

import (
	"context"

	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
)

// AnalyticsService 数据分析只读服务。
type AnalyticsService interface {
	// GetDashboard 返回数据分析页所需的全量数据：概览、按状态/按文件夹/按时间、最近与失败任务列表。
	GetDashboard(ctx context.Context, days int) (DashboardData, error)
}

// DashboardData 数据分析页单次接口返回结构。
type DashboardData struct {
	// Overview 概览数字（卡片）
	Overview OverviewVO `json:"overview"`
	// ByStatus 按状态分布（饼图/柱状图）
	ByStatus []StatusItemVO `json:"by_status"`
	// ByWatchFolder 按监听文件夹分布（表格+柱状图）
	ByWatchFolder []WatchFolderItemVO `json:"by_watch_folder"`
	// ByTime 按日趋势（折线图）
	ByTime []TimeItemVO `json:"by_time"`
	// Items 列表类数据
	Items DashboardItemsVO `json:"items"`
}

// OverviewVO 概览。
type OverviewVO struct {
	TaskTotal         int64 `json:"task_total"`
	TaskPending       int64 `json:"task_pending"`
	TaskRunning       int64 `json:"task_running"`
	TaskSuccess       int64 `json:"task_success"`
	TaskFailed        int64 `json:"task_failed"`
	TaskPaused        int64 `json:"task_paused"`
	UploadedBytes     int64 `json:"uploaded_bytes_total"`
	UploadedFiles     int64 `json:"uploaded_files_total"`
	WatchFolderCount  int64 `json:"watch_folder_count"`
	Recent24hCompleted int64 `json:"recent_24h_completed"`
	Recent24hFailed   int64 `json:"recent_24h_failed"`
}

// StatusItemVO 按状态一项（图表用）。
type StatusItemVO struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
	Label  string `json:"label"`
}

// WatchFolderItemVO 按监听文件夹一项（表格+图表）。
type WatchFolderItemVO struct {
	WatchFolderID   uint   `json:"watch_folder_id"`
	WatchFolderName string `json:"watch_folder_name"`
	TaskCount       int64  `json:"task_count"`
	SuccessCount    int64  `json:"success_count"`
	FailedCount     int64  `json:"failed_count"`
	PendingCount    int64  `json:"pending_count"`
	RunningCount    int64  `json:"running_count"`
	PausedCount     int64  `json:"paused_count"`
	UploadedBytes   int64  `json:"uploaded_bytes"`
	UploadedFiles   int64  `json:"uploaded_files"`
}

// TimeItemVO 按日一项（趋势图）。
type TimeItemVO struct {
	Date            string `json:"date"`
	CompletedCount  int64  `json:"completed_count"`
	FailedCount     int64  `json:"failed_count"`
	UploadedBytes   int64  `json:"uploaded_bytes"`
}

// DashboardItemsVO 列表数据。
type DashboardItemsVO struct {
	RecentTasks []model.UploadTask `json:"recent_tasks"`
	FailedTasks []model.UploadTask `json:"failed_tasks"`
}

var statusLabels = map[string]string{
	model.TaskStatusPending: "待上传",
	model.TaskStatusRunning:  "上传中",
	model.TaskStatusSuccess:  "上传完成",
	model.TaskStatusFailed:   "上传失败",
	model.TaskStatusPaused:   "已暂停",
}

type analyticsService struct {
	repo repository.AnalyticsRepository
}

// NewAnalyticsService 创建数据分析服务。
func NewAnalyticsService(repo repository.AnalyticsRepository) AnalyticsService {
	return &analyticsService{repo: repo}
}

func (s *analyticsService) GetDashboard(ctx context.Context, days int) (DashboardData, error) {
	var out DashboardData
	if days <= 0 {
		days = 7
	}

	overview, err := s.repo.Overview()
	if err != nil {
		return out, err
	}
	out.Overview = OverviewVO{
		TaskTotal:          overview.TaskTotal,
		TaskPending:         overview.TaskPending,
		TaskRunning:         overview.TaskRunning,
		TaskSuccess:         overview.TaskSuccess,
		TaskFailed:          overview.TaskFailed,
		TaskPaused:          overview.TaskPaused,
		UploadedBytes:       overview.UploadedBytes,
		UploadedFiles:       overview.UploadedFiles,
		WatchFolderCount:    overview.WatchFolderCount,
		Recent24hCompleted:  overview.Recent24hDone,
		Recent24hFailed:     overview.Recent24hFailed,
	}

	byStatus, err := s.repo.GroupTaskByStatus()
	if err != nil {
		return out, err
	}
	out.ByStatus = make([]StatusItemVO, 0, len(byStatus))
	for _, r := range byStatus {
		label := statusLabels[r.Status]
		if label == "" {
			label = r.Status
		}
		out.ByStatus = append(out.ByStatus, StatusItemVO{Status: r.Status, Count: r.Count, Label: label})
	}

	byWF, err := s.repo.GroupTaskByWatchFolder()
	if err != nil {
		return out, err
	}
	out.ByWatchFolder = make([]WatchFolderItemVO, 0, len(byWF))
	for _, r := range byWF {
		out.ByWatchFolder = append(out.ByWatchFolder, WatchFolderItemVO{
			WatchFolderID:   r.WatchFolderID,
			WatchFolderName: r.WatchFolderName,
			TaskCount:       r.TaskCount,
			SuccessCount:    r.SuccessCount,
			FailedCount:     r.FailedCount,
			PendingCount:    r.PendingCount,
			RunningCount:    r.RunningCount,
			PausedCount:     r.PausedCount,
			UploadedBytes:   r.UploadedBytes,
			UploadedFiles:   r.UploadedFiles,
		})
	}

	byTime, err := s.repo.GroupTaskByDate(days)
	if err != nil {
		return out, err
	}
	out.ByTime = make([]TimeItemVO, 0, len(byTime))
	for _, r := range byTime {
		out.ByTime = append(out.ByTime, TimeItemVO{
			Date:           r.Dt,
			CompletedCount: r.CompletedCount,
			FailedCount:    r.FailedCount,
			UploadedBytes:  r.UploadedBytes,
		})
	}

	recent, _ := s.repo.RecentTasks(10)
	failed, _ := s.repo.FailedTasks(20, nil)
	out.Items = DashboardItemsVO{RecentTasks: recent, FailedTasks: failed}
	return out, nil
}
