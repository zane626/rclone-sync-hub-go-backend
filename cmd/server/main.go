// 程序入口：加载配置、依赖注入、启动 HTTP、Worker、Scheduler。
// 生产环境时嵌入 Vue3 构建产物 frontend/dist，访问 / 返回 index.html，/api 为 API 路由。
//
// Swagger 文档由 config.server.enable_swagger 控制，仅开发环境建议开启。
//
// @title           Rclone Sync Hub API
// @version         2.0
// @description     面向生产环境的本地文件扫描、持久化上传队列与 rclone 调度 API。
// @contact.name    Project repository
// @contact.url     https://github.com/zane626/rclone-sync-hub-go-backend
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
package main

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"rclone-sync-hub/internal/api"
	"rclone-sync-hub/internal/config"
	"rclone-sync-hub/internal/database"
	"rclone-sync-hub/internal/events"
	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/observability"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/scheduler"
	"rclone-sync-hub/internal/security"
	"rclone-sync-hub/internal/service"
	"rclone-sync-hub/internal/worker"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	_ "rclone-sync-hub/cmd/server/docs" // 由 swag init 生成，用于 Swagger UI
)

//go:embed frontend/dist/*
var frontendFS embed.FS

// Set by release builds through -ldflags. Defaults remain useful for local runs.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// 1. 加载配置：CONFIG_PATH 为空或文件不存在时仅从环境变量加载（便于 Docker 无挂载 config 部署）
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		panic("load config: " + err.Error())
	}

	// 2. 初始化日志
	if err := logger.Init(cfg.Log.Level, cfg.Log.Format); err != nil {
		panic("init logger: " + err.Error())
	}
	defer logger.Sync()
	logger.L.Info("starting rclone sync hub",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_date", buildDate),
	)
	authSvc, err := security.NewAuthService(security.AuthConfig{
		Enabled:        cfg.Security.Enabled,
		AdminUsername:  cfg.Security.AdminUsername,
		AdminPassword:  cfg.Security.AdminPassword,
		ViewerUsername: cfg.Security.ViewerUsername,
		ViewerPassword: cfg.Security.ViewerPassword,
		TokenSecret:    cfg.Security.TokenSecret,
		TokenTTL:       time.Duration(cfg.Security.TokenTTLMinutes) * time.Minute,
	})
	if err != nil {
		logger.L.Fatal("invalid authentication configuration", zap.Error(err))
	}
	resourcePolicy, err := security.NewResourcePolicy(cfg.Security.Enabled, cfg.Security.AllowedLocalRoots, cfg.Security.AllowedRemotes)
	if err != nil {
		logger.L.Fatal("invalid resource allowlist configuration", zap.Error(err))
	}
	if !cfg.Security.Enabled {
		logger.L.Warn("authentication is disabled; do not expose this instance to an untrusted network")
	}

	gin.SetMode(cfg.Server.Mode)

	// 3. 连接数据库（通过 database 包，支持后期切换 driver）
	dbCfg := database.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		User:            cfg.Database.User,
		Password:        cfg.Database.Password,
		DBName:          cfg.Database.DBName,
		Charset:         cfg.Database.Charset,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxIdleTime: time.Duration(cfg.Database.ConnMaxIdleTimeMins) * time.Minute,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetimeMins) * time.Minute,
		ConnectTimeout:  time.Duration(cfg.Database.ConnectTimeoutSecs) * time.Second,
		ReadTimeout:     time.Duration(cfg.Database.ReadTimeoutSecs) * time.Second,
		WriteTimeout:    time.Duration(cfg.Database.WriteTimeoutSecs) * time.Second,
		TLS:             cfg.Database.TLS,
	}
	db, err := database.OpenMySQL(dbCfg)
	if err != nil {
		logger.L.Fatal("open db failed", zap.Error(err))
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.L.Fatal("get sql db failed", zap.Error(err))
	}
	defer sqlDB.Close()
	metrics := observability.NewMetrics(sqlDB)
	migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer migrationCancel()
	if err := database.RunMigrations(migrationCtx, db, database.DefaultMigrations()); err != nil {
		logger.L.Fatal("migrate failed", zap.Error(err))
	}

	// 3.1 启动状态修复（将上一次异常退出残留的状态修复为安全状态）
	taskInitSvc := service.NewTaskInitService(db)
	if err := taskInitSvc.FixStatusesOnStartup(context.Background()); err != nil {
		logger.L.Warn("startup status fix failed", zap.Error(err))
	}

	// 4. 依赖注入：repository / rclone / worker / scheduler / service / api
	taskRepo := repository.NewTaskRepository(db)
	fileRepo := repository.NewFileRecordRepository(db)
	logRepo := repository.NewUploadLogRepository(db)
	watchFolderRepo := repository.NewWatchFolderRepository(db)
	scanRunRepo := repository.NewScanRunRepository(db)
	analyticsRepo := repository.NewAnalyticsRepository(db)
	auditRepo := repository.NewAuditLogRepository(db)
	topologyCtx, topologyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := service.ValidateWatchFolderPathTopology(topologyCtx, watchFolderRepo); err != nil {
		topologyCancel()
		logger.L.Fatal("invalid watch folder topology", zap.Error(err))
	}
	topologyCancel()

	rc := rclone.NewClient(cfg.Rclone.BinPath)
	rcloneCtx, rcloneCancel := context.WithTimeout(context.Background(), 10*time.Second)
	configuredRemotes, err := rc.ListRemotes(rcloneCtx)
	rcloneCancel()
	if err != nil {
		logger.L.Fatal("rclone preflight failed", zap.Error(err))
	}
	remoteSet := make(map[string]struct{}, len(configuredRemotes))
	for _, remote := range configuredRemotes {
		remoteSet[remote.Name] = struct{}{}
	}
	missingRemotes := make([]string, 0)
	for _, allowed := range cfg.Security.AllowedRemotes {
		if _, exists := remoteSet[allowed]; !exists {
			missingRemotes = append(missingRemotes, allowed)
		}
	}
	if len(missingRemotes) > 0 {
		logger.L.Fatal("allowed rclone remotes are missing from rclone.conf", zap.Strings("missing_remotes", missingRemotes))
	}
	eventHub := events.NewHub(100)
	q := worker.NewQueue(taskRepo, logRepo, watchFolderRepo, rc, worker.Config{
		MaxConcurrent:           cfg.Worker.MaxConcurrent,
		MaxRetry:                cfg.Worker.MaxRetry,
		PollInterval:            time.Duration(cfg.Worker.PollIntervalMilliseconds) * time.Millisecond,
		LeaseDuration:           time.Duration(cfg.Worker.LeaseSeconds) * time.Second,
		HeartbeatInterval:       time.Duration(cfg.Worker.HeartbeatSeconds) * time.Second,
		TaskTimeout:             time.Duration(cfg.Worker.TaskTimeoutSeconds) * time.Second,
		RetryBaseDelay:          time.Duration(cfg.Worker.RetryBaseSeconds) * time.Second,
		RetryMaxDelay:           time.Duration(cfg.Worker.RetryMaxSeconds) * time.Second,
		ProgressPersistInterval: time.Duration(cfg.Worker.ProgressPersistSeconds) * time.Second,
		InstanceID:              cfg.Worker.InstanceID,
		ResourcePolicy:          resourcePolicy,
		Metrics:                 metrics,
	}, worker.WithProgressCallback(func(taskID uint, percent float64, bytesDone, bytesTotal, speed int64, message string) {
		eventHub.Publish(events.Event{Type: "task_progress", TaskID: taskID, Percent: percent, BytesDone: bytesDone, BytesTotal: bytesTotal, Speed: speed, Message: message})
	}), worker.WithStatusCallback(func(taskID uint, status, message string) {
		eventHub.Publish(events.Event{Type: "task_status", TaskID: taskID, Status: status, Message: message})
	}))

	watchFolderScanner := scheduler.NewWatchFolderScanner(watchFolderRepo, fileRepo, taskRepo, scanRunRepo, scheduler.WatchFolderScannerConfig{
		PollInterval:     time.Duration(cfg.Scan.WatchPollIntervalSeconds) * time.Second,
		DefaultInterval:  time.Duration(cfg.Scan.IntervalSeconds) * time.Second,
		FolderTimeout:    time.Duration(cfg.Scan.FolderTimeoutSeconds) * time.Second,
		FileStablePeriod: time.Duration(cfg.Scan.FileStableSeconds) * time.Second,
		MaxConcurrent:    cfg.Scan.MaxConcurrentFolders,
		BatchSize:        cfg.Scan.BatchSize,
		InstanceID:       cfg.Worker.InstanceID,
		LeaseDuration:    time.Duration(cfg.Scan.LeaseSeconds) * time.Second,
		Heartbeat:        time.Duration(cfg.Scan.HeartbeatSeconds) * time.Second,
		ResourcePolicy:   resourcePolicy,
		Metrics:          metrics,
	})

	uploadSvc := service.NewUploadService(taskRepo, fileRepo, logRepo, watchFolderScanner, q, resourcePolicy)
	rcloneSvc := service.NewRcloneService(rc, resourcePolicy)
	watchFolderSvc := service.NewWatchFolderService(watchFolderRepo, resourcePolicy)
	analyticsSvc := service.NewAnalyticsService(analyticsRepo)
	fsSvc := service.NewFSService(resourcePolicy)
	taskHandler := api.NewTaskHandler(uploadSvc)
	healthHandler := api.NewHealthHandler(sqlDB)
	rcloneHandler := api.NewRcloneHandler(rcloneSvc)
	watchFolderHandler := api.NewWatchFolderHandler(watchFolderSvc)
	analyticsHandler := api.NewAnalyticsHandler(analyticsSvc)
	fsHandler := api.NewFSHandler(fsSvc)
	authHandler := api.NewAuthHandler(authSvc, cfg.Security.LoginAttemptsPerMinute)
	securityMiddleware := api.NewSecurityMiddleware(authSvc, auditRepo, cfg.Security.MaxRequestBodyBytes, cfg.Security.MetricsToken)
	maintenanceSvc := service.NewMaintenanceService(db, service.MaintenanceConfig{
		Interval:           time.Duration(cfg.Maintenance.IntervalHours) * time.Hour,
		UploadLogRetention: time.Duration(cfg.Maintenance.UploadLogRetentionDays) * 24 * time.Hour,
		TaskRetention:      time.Duration(cfg.Maintenance.TaskRetentionDays) * 24 * time.Hour,
		ScanRunRetention:   time.Duration(cfg.Maintenance.ScanRunRetentionDays) * 24 * time.Hour,
		AuditLogRetention:  time.Duration(cfg.Maintenance.AuditLogRetentionDays) * 24 * time.Hour,
		DeleteBatchSize:    cfg.Maintenance.DeleteBatchSize,
	})
	operationsSvc := service.NewOperationsService(scanRunRepo, auditRepo)
	operationsHandler := api.NewOperationsHandler(operationsSvc)
	eventHandler := api.NewEventHandler(eventHub)

	// 5. 路由：/api 为 API，其余为前端静态 + fallback
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		logger.L.Fatal("invalid trusted proxy configuration", zap.Error(err))
	}
	r.Use(securityMiddleware.Base(), metrics.HTTPMiddleware(), securityMiddleware.AuditMutations(), gin.Recovery())
	api.Router(r, taskHandler, healthHandler, rcloneHandler, watchFolderHandler, fsHandler, analyticsHandler, authHandler, operationsHandler, eventHandler, securityMiddleware)
	if cfg.Security.MetricsToken != "" {
		r.GET("/metrics", securityMiddleware.MetricsAuthenticate(), gin.WrapH(metrics.Handler()))
	} else {
		r.GET("/metrics", securityMiddleware.Authenticate(), securityMiddleware.RequireAdmin(), gin.WrapH(metrics.Handler()))
	}

	// Swagger 文档：仅当配置开启时注册，生产环境务必关闭
	if cfg.Server.EnableSwagger {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	}

	// 嵌入前端：仅当配置开启时提供静态与 fallback
	if cfg.Server.EmbedFrontend {
		sub, _ := fs.Sub(frontendFS, "frontend/dist")
		r.NoRoute(serveFrontend(sub))
	}

	// 6. 启动 Worker 与 Scheduler（后台）
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var background sync.WaitGroup
	background.Add(3)
	go func() { defer background.Done(); q.Run(ctx) }()
	go func() { defer background.Done(); watchFolderScanner.Run(ctx) }()
	go func() { defer background.Done(); maintenanceSvc.Run(ctx) }()

	// 7. HTTP 服务
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L.Fatal("http serve failed", zap.Error(err))
		}
	}()

	// 8. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.L.Warn("server shutdown", zap.Error(err))
	}
	backgroundDone := make(chan struct{})
	go func() {
		background.Wait()
		close(backgroundDone)
	}()
	select {
	case <-backgroundDone:
	case <-time.After(30 * time.Second):
		logger.L.Warn("background services did not stop before shutdown deadline")
	}
	logger.L.Info("server exited")
}

// serveFrontend 返回 Gin 的 NoRoute 处理，兼容 Vue Router history 模式：
// - 存在对应静态文件（如 /assets/xxx.js）则直接返回该文件；
// - 不存在则统一返回 index.html，由前端路由处理（/dashboard、/tasks 等）。
// 根路径 / 与上述 fallback 均直接输出 index 内容，不发起重定向，避免 ERR_TOO_MANY_REDIRECTS。
func serveFrontend(staticFS fs.FS) gin.HandlerFunc {
	fileServer := http.FileServer(http.FS(staticFS))
	var indexHTML []byte
	if rfs, ok := staticFS.(fs.ReadFileFS); ok {
		indexHTML, _ = rfs.ReadFile("index.html")
	}
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "api endpoint not found", "code": "not_found"})
			return
		}
		p := strings.TrimPrefix(path, "/")
		// 根路径：直接返回 index.html
		if p == "" || p == "/" {
			if len(indexHTML) > 0 {
				c.Header("Cache-Control", "no-cache")
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
				return
			}
			p = "index.html"
		}
		// 尝试作为静态文件打开（如 /assets/index-xxx.js、favicon.ico）
		f, err := staticFS.Open(p)
		if err != nil {
			if strings.HasPrefix(p, "assets/") || pathpkg.Ext(p) != "" {
				c.Status(http.StatusNotFound)
				return
			}
			// Vue history 模式 fallback：任意未匹配路径（如 /dashboard、/tasks）均返回 index.html，由前端路由接管
			if len(indexHTML) > 0 {
				c.Header("Cache-Control", "no-cache")
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
				return
			}
			c.Request.URL.Path = "/index.html"
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		_ = f.Close()
		if strings.HasPrefix(p, "assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else if p == "index.html" {
			c.Header("Cache-Control", "no-cache")
		}
		c.Request.URL.Path = "/" + p
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
