package observability

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	scanRuns       *prometheus.CounterVec
	scanDuration   *prometheus.HistogramVec
	scanFiles      prometheus.Counter
	scanTasks      prometheus.Counter
	workerActive   prometheus.Gauge
	workerTasks    *prometheus.CounterVec
	workerDuration *prometheus.HistogramVec
	workerRetries  prometheus.Counter
}

func NewMetrics(db *sql.DB) *Metrics {
	m := &Metrics{
		registry:       prometheus.NewRegistry(),
		httpRequests:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rclone_sync_hub_http_requests_total", Help: "HTTP requests by method, route, and status."}, []string{"method", "route", "status"}),
		httpDuration:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rclone_sync_hub_http_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		scanRuns:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rclone_sync_hub_scan_runs_total", Help: "Completed folder scans by result."}, []string{"status"}),
		scanDuration:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rclone_sync_hub_scan_duration_seconds", Help: "Folder scan duration.", Buckets: []float64{0.1, 0.5, 1, 5, 15, 60, 300, 900, 1800}}, []string{"status"}),
		scanFiles:      prometheus.NewCounter(prometheus.CounterOpts{Name: "rclone_sync_hub_scanned_files_total", Help: "Regular files observed by completed scans."}),
		scanTasks:      prometheus.NewCounter(prometheus.CounterOpts{Name: "rclone_sync_hub_scan_tasks_created_total", Help: "Upload tasks created by scans."}),
		workerActive:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "rclone_sync_hub_worker_active_tasks", Help: "Tasks currently executing in this process."}),
		workerTasks:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rclone_sync_hub_worker_task_results_total", Help: "Worker task attempts by final transition."}, []string{"status"}),
		workerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rclone_sync_hub_worker_task_duration_seconds", Help: "Worker attempt duration.", Buckets: []float64{1, 5, 15, 60, 300, 900, 3600, 14400}}, []string{"status"}),
		workerRetries:  prometheus.NewCounter(prometheus.CounterOpts{Name: "rclone_sync_hub_worker_retries_scheduled_total", Help: "Persistent retries scheduled by workers."}),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests, m.httpDuration,
		m.scanRuns, m.scanDuration, m.scanFiles, m.scanTasks,
		m.workerActive, m.workerTasks, m.workerDuration, m.workerRetries,
	)
	if db != nil {
		m.registerDatabaseMetrics(db)
		m.registerBusinessMetrics(db)
	}
	return m
}

func (m *Metrics) registerBusinessMetrics(db *sql.DB) {
	gauges := []struct {
		name  string
		help  string
		query string
		args  []interface{}
	}{
		{
			name:  "rclone_sync_hub_task_queue_pending",
			help:  "Durable tasks currently waiting or backing off in MySQL.",
			query: "SELECT COUNT(*) FROM upload_tasks WHERE status = ?",
			args:  []interface{}{"pending"},
		},
		{
			name:  "rclone_sync_hub_task_queue_oldest_pending_age_seconds",
			help:  "Age in seconds of the oldest durable pending task.",
			query: "SELECT COALESCE(GREATEST(TIMESTAMPDIFF(SECOND, MIN(created_at), NOW()), 0), 0) FROM upload_tasks WHERE status = ?",
			args:  []interface{}{"pending"},
		},
		{
			name:  "rclone_sync_hub_watch_folders_enabled",
			help:  "Enabled watch folders that should be scheduled.",
			query: "SELECT COUNT(*) FROM watch_folders WHERE enabled = TRUE AND status NOT IN ('stopped', 'paused')",
		},
		{
			name: "rclone_sync_hub_watch_folders_overdue",
			help: "Enabled watch folders due for scanning without an active lease.",
			query: `SELECT COUNT(*) FROM watch_folders
				WHERE enabled = TRUE
				AND status NOT IN ('stopped', 'paused')
				AND (next_scan_at IS NULL OR next_scan_at <= NOW())
				AND (status <> 'detecting' OR scan_lease_expires_at IS NULL OR scan_lease_expires_at <= NOW())`,
		},
	}
	for _, item := range gauges {
		metric := item
		m.registry.MustRegister(prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{Name: metric.name, Help: metric.help},
			func() float64 { return queryScalar(db, metric.query, metric.args...) },
		))
	}
}

func queryScalar(db *sql.DB, query string, args ...interface{}) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var value sql.NullFloat64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil || !value.Valid {
		return math.NaN()
	}
	return value.Float64
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *Metrics) HTTPMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		m.httpRequests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		m.httpDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(started).Seconds())
	}
}

func (m *Metrics) ObserveScan(status string, duration time.Duration, filesSeen, tasksCreated int64) {
	if m == nil {
		return
	}
	m.scanRuns.WithLabelValues(status).Inc()
	m.scanDuration.WithLabelValues(status).Observe(duration.Seconds())
	m.scanFiles.Add(float64(filesSeen))
	m.scanTasks.Add(float64(tasksCreated))
}

func (m *Metrics) WorkerStarted() {
	if m != nil {
		m.workerActive.Inc()
	}
}

func (m *Metrics) WorkerReleased() {
	if m != nil {
		m.workerActive.Dec()
	}
}

func (m *Metrics) ObserveWorkerResult(status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.workerTasks.WithLabelValues(status).Inc()
	m.workerDuration.WithLabelValues(status).Observe(duration.Seconds())
}

func (m *Metrics) RetryScheduled() {
	if m != nil {
		m.workerRetries.Inc()
	}
}

func (m *Metrics) registerDatabaseMetrics(db *sql.DB) {
	m.registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{Name: "rclone_sync_hub_ready", Help: "Whether the service can currently reach MySQL."},
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				return 0
			}
			return 1
		},
	))
	gauges := []struct {
		name  string
		help  string
		value func(sql.DBStats) float64
	}{
		{"rclone_sync_hub_db_open_connections", "Open database connections.", func(s sql.DBStats) float64 { return float64(s.OpenConnections) }},
		{"rclone_sync_hub_db_in_use_connections", "Database connections currently in use.", func(s sql.DBStats) float64 { return float64(s.InUse) }},
		{"rclone_sync_hub_db_idle_connections", "Idle database connections.", func(s sql.DBStats) float64 { return float64(s.Idle) }},
		{"rclone_sync_hub_db_max_open_connections", "Configured maximum open database connections.", func(s sql.DBStats) float64 { return float64(s.MaxOpenConnections) }},
	}
	for _, item := range gauges {
		metric := item
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: metric.name, Help: metric.help}, func() float64 { return metric.value(db.Stats()) }))
	}
	m.registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{Name: "rclone_sync_hub_db_wait_count_total", Help: "Total waits for a database connection."},
		func() float64 { return float64(db.Stats().WaitCount) },
	))
}
