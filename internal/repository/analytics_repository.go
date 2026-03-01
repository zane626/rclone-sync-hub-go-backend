// Package repository 数据分析相关数据访问，基于 upload_tasks 与 watch_folders 聚合查询。
package repository

import (
	"fmt"
	"time"

	"rclone-sync-hub/internal/model"

	"gorm.io/gorm"
)

// StatusCount 按状态聚合的一条记录。
type StatusCount struct {
	Status string
	Count  int64
}

// WatchFolderStat 按监听文件夹聚合的统计。
type WatchFolderStat struct {
	WatchFolderID   uint
	WatchFolderName string
	TaskCount       int64
	SuccessCount    int64
	FailedCount     int64
	PendingCount    int64
	RunningCount    int64
	PausedCount     int64
	UploadedBytes   int64
	UploadedFiles   int64
}

// DateStat 按日期聚合的统计（用于趋势图）。
type DateStat struct {
	Dt             string // 日期 YYYY-MM-DD
	CompletedCount int64
	FailedCount    int64
	UploadedBytes  int64
}

// OverviewCounts 概览数字（仪表盘卡片）。
type OverviewCounts struct {
	TaskTotal        int64
	TaskPending      int64
	TaskRunning      int64
	TaskSuccess      int64
	TaskFailed       int64
	TaskPaused       int64
	UploadedBytes    int64
	UploadedFiles    int64
	WatchFolderCount int64
	Recent24hDone    int64
	Recent24hFailed  int64
}

// AnalyticsRepository 数据分析只读查询。
type AnalyticsRepository interface {
	// Overview 获取概览统计。
	Overview() (OverviewCounts, error)
	// GroupTaskByStatus 按状态分组统计任务数。
	GroupTaskByStatus() ([]StatusCount, error)
	// GroupTaskByWatchFolder 按 watch_folder_id 分组统计。
	GroupTaskByWatchFolder() ([]WatchFolderStat, error)
	// GroupTaskByDate 按完成日期分组，最近 days 天（基于 finished_at）。
	GroupTaskByDate(days int) ([]DateStat, error)
	// RecentTasks 最近完成或失败的任务，limit 条。
	RecentTasks(limit int) ([]model.UploadTask, error)
	// FailedTasks 失败任务列表，可选 watch_folder_id 筛选。
	FailedTasks(limit int, watchFolderID *uint) ([]model.UploadTask, error)
}

type analyticsRepository struct {
	db *gorm.DB
}

// NewAnalyticsRepository 构造 AnalyticsRepository。
func NewAnalyticsRepository(db *gorm.DB) AnalyticsRepository {
	return &analyticsRepository{db: db}
}

func (r *analyticsRepository) Overview() (OverviewCounts, error) {
	var o OverviewCounts
	var taskTotal int64
	if err := r.db.Model(&model.UploadTask{}).Count(&taskTotal).Error; err != nil {
		return o, fmt.Errorf("analytics overview tasks: %w", err)
	}
	o.TaskTotal = taskTotal
	for _, s := range []string{model.TaskStatusPending, model.TaskStatusRunning, model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusPaused} {
		var n int64
		if err := r.db.Model(&model.UploadTask{}).Where("status = ?", s).Count(&n).Error; err != nil {
			continue
		}
		switch s {
		case model.TaskStatusPending:
			o.TaskPending = n
		case model.TaskStatusRunning:
			o.TaskRunning = n
		case model.TaskStatusSuccess:
			o.TaskSuccess = n
		case model.TaskStatusFailed:
			o.TaskFailed = n
		case model.TaskStatusPaused:
			o.TaskPaused = n
		}
	}
	var sumBytes int64
	r.db.Model(&model.UploadTask{}).Where("status = ?", model.TaskStatusSuccess).Select("coalesce(sum(file_size),0)").Scan(&sumBytes)
	o.UploadedBytes = sumBytes
	o.UploadedFiles = o.TaskSuccess
	if err := r.db.Model(&model.WatchFolder{}).Count(&o.WatchFolderCount).Error; err != nil {
		o.WatchFolderCount = 0
	}
	since24h := time.Now().Add(-24 * time.Hour)
	r.db.Model(&model.UploadTask{}).Where("status = ? AND finished_at >= ?", model.TaskStatusSuccess, since24h).Count(&o.Recent24hDone)
	r.db.Model(&model.UploadTask{}).Where("status = ? AND updated_at >= ?", model.TaskStatusFailed, since24h).Count(&o.Recent24hFailed)
	return o, nil
}

func (r *analyticsRepository) GroupTaskByStatus() ([]StatusCount, error) {
	var res []StatusCount
	err := r.db.Model(&model.UploadTask{}).
		Select("status", "count(*) as count").
		Group("status").
		Find(&res).Error
	if err != nil {
		return nil, fmt.Errorf("analytics group by status: %w", err)
	}
	return res, nil
}

func (r *analyticsRepository) GroupTaskByWatchFolder() ([]WatchFolderStat, error) {
	type row struct {
		WatchFolderID   uint
		WatchFolderName string
		TaskCount       int64
		SuccessCount    int64
		FailedCount     int64
		PendingCount    int64
		RunningCount    int64
		PausedCount     int64
		UploadedBytes   int64
		UploadedFiles   int64
	}
	var rows []row
	// watch_folder_id = 0 表示任务未关联文件夹，用 COALESCE 保留
	err := r.db.Model(&model.UploadTask{}).
		Select(`
			watch_folder_id as watch_folder_id,
			COALESCE(NULLIF(watch_folder_name,''), '(未关联)') as watch_folder_name,
			count(*) as task_count,
			sum(case when status = ? then 1 else 0 end) as success_count,
			sum(case when status = ? then 1 else 0 end) as failed_count,
			sum(case when status = ? then 1 else 0 end) as pending_count,
			sum(case when status = ? then 1 else 0 end) as running_count,
			sum(case when status = ? then 1 else 0 end) as paused_count,
			sum(case when status = ? then file_size else 0 end) as uploaded_bytes,
			sum(case when status = ? then 1 else 0 end) as uploaded_files
		`,
			model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusPending,
			model.TaskStatusRunning, model.TaskStatusPaused, model.TaskStatusSuccess, model.TaskStatusSuccess,
		).
		Group("watch_folder_id, watch_folder_name").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("analytics group by watch_folder: %w", err)
	}
	out := make([]WatchFolderStat, len(rows))
	for i := range rows {
		out[i] = WatchFolderStat{
			WatchFolderID:   rows[i].WatchFolderID,
			WatchFolderName: rows[i].WatchFolderName,
			TaskCount:       rows[i].TaskCount,
			SuccessCount:    rows[i].SuccessCount,
			FailedCount:     rows[i].FailedCount,
			PendingCount:    rows[i].PendingCount,
			RunningCount:    rows[i].RunningCount,
			PausedCount:     rows[i].PausedCount,
			UploadedBytes:   rows[i].UploadedBytes,
			UploadedFiles:   rows[i].UploadedFiles,
		}
	}
	return out, nil
}

func (r *analyticsRepository) GroupTaskByDate(days int) ([]DateStat, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)
	var res []DateStat
	err := r.db.Model(&model.UploadTask{}).
		Select(`
			date(finished_at) as dt,
			sum(case when status = ? then 1 else 0 end) as completed_count,
			sum(case when status = ? then 1 else 0 end) as failed_count,
			sum(case when status = ? then file_size else 0 end) as uploaded_bytes
		`, model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusSuccess).
		Where("finished_at >= ?", since).
		Group("date(finished_at)").
		Order("dt ASC").
		Find(&res).Error
	if err != nil {
		return nil, fmt.Errorf("analytics group by date: %w", err)
	}
	return res, nil
}

func (r *analyticsRepository) RecentTasks(limit int) ([]model.UploadTask, error) {
	if limit <= 0 {
		limit = 10
	}
	var list []model.UploadTask
	err := r.db.Model(&model.UploadTask{}).
		Where("status IN ?", []string{model.TaskStatusSuccess, model.TaskStatusFailed}).
		Order("finished_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *analyticsRepository) FailedTasks(limit int, watchFolderID *uint) ([]model.UploadTask, error) {
	if limit <= 0 {
		limit = 20
	}
	q := r.db.Model(&model.UploadTask{}).Where("status = ?", model.TaskStatusFailed).Order("updated_at DESC").Limit(limit)
	if watchFolderID != nil {
		q = q.Where("watch_folder_id = ?", *watchFolderID)
	}
	var list []model.UploadTask
	err := q.Find(&list).Error
	return list, err
}
