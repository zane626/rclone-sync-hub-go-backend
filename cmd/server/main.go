// 程序入口：加载配置、依赖注入、启动 HTTP、Worker、Scheduler。
// 生产环境时嵌入 Vue3 构建产物 frontend/dist，访问 / 返回 index.html，/api 为 API 路由。
//
// Swagger 文档由 config.server.enable_swagger 控制，仅开发环境建议开启。
//
// @title           Rclone Sync Hub API
// @version         1.0
// @description     文件上传调度系统 REST API：任务列表、扫描、重试、统计等
// @termsOfService  http://swagger.io/terms/
// @contact.name    API Support
// @contact.url     https://github.com/your-org/rclone-sync-hub
// @license.name    Apache 2.0
// @license.url     http://www.apache.org/licenses/LICENSE-2.0.html
// @host            localhost:8080
// @BasePath        /
package main

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rclone-sync-hub/internal/api"
	"rclone-sync-hub/internal/config"
	"rclone-sync-hub/internal/database"
	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/rclone"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/scheduler"
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

func main() {
	// 1. 加载配置
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
	}
	db, err := database.OpenMySQL(dbCfg)
	if err != nil {
		logger.L.Fatal("open db failed", zap.Error(err))
	}
	if err := database.Migrate(db,
		&model.UploadTask{},
		&model.FileRecord{},
		&model.UploadLog{},
		&model.WatchFolder{},
	); err != nil {
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
	analyticsRepo := repository.NewAnalyticsRepository(db)

	rc := rclone.NewClient(cfg.Rclone.BinPath)
	q := worker.NewQueue(taskRepo, logRepo, fileRepo, watchFolderRepo, rc,
		cfg.Worker.MaxConcurrent, cfg.Worker.MaxRetry, cfg.Worker.QueueSize,
	)

	scanCfg := scheduler.ScannerConfig{
		LocalPath:       cfg.Scan.LocalPath,
		CronSchedule:    cfg.Scan.CronSchedule,
		Enabled:         cfg.Scan.Enabled,
		IntervalSeconds: cfg.Scan.IntervalSeconds,
	}
	scanner := scheduler.NewScanner(fileRepo, taskRepo, q, scanCfg)
	watchFolderScanner := scheduler.NewWatchFolderScanner(watchFolderRepo, fileRepo, taskRepo, rc, cfg.Scan.IntervalSeconds)

	// 任务调度器：从数据库中选择 pending 任务，根据最大并发分发到 worker
	maxUploads := config.GetMaxConcurrentUploads()
	taskScheduler := scheduler.NewTaskScheduler(taskRepo, q, maxUploads, 2*time.Second)

	uploadSvc := service.NewUploadService(taskRepo, fileRepo, logRepo, scanner, q)
	rcloneSvc := service.NewRcloneService(rc)
	watchFolderSvc := service.NewWatchFolderService(watchFolderRepo)
	analyticsSvc := service.NewAnalyticsService(analyticsRepo)
	fsSvc := service.NewFSService()
	taskHandler := api.NewTaskHandler(uploadSvc)
	healthHandler := api.NewHealthHandler()
	rcloneHandler := api.NewRcloneHandler(rcloneSvc)
	watchFolderHandler := api.NewWatchFolderHandler(watchFolderSvc)
	analyticsHandler := api.NewAnalyticsHandler(analyticsSvc)
	fsHandler := api.NewFSHandler(fsSvc)

	// 5. 路由：/api 为 API，其余为前端静态 + fallback
	r := gin.New()
	r.Use(gin.Recovery())
	api.Router(r, taskHandler, healthHandler, rcloneHandler, watchFolderHandler, fsHandler, analyticsHandler)

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

	go q.Run(ctx)
	go scanner.Run(ctx)
	go watchFolderScanner.Run(ctx)
	go taskScheduler.Run(ctx)

	// 7. HTTP 服务
	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.Server.Port),
		Handler: r,
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
	logger.L.Info("server exited")
}

// serveFrontend 返回 Gin 的 NoRoute 处理：先尝试静态文件，不存在则回退到 index.html（SPA fallback）。
func serveFrontend(staticFS fs.FS) gin.HandlerFunc {
	fileServer := http.FileServer(http.FS(staticFS))
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		f, err := staticFS.Open(strings.TrimPrefix(p, "/"))
		if err != nil {
			// 未匹配到静态资源，返回 index.html
			c.Request.URL.Path = "/index.html"
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		_ = f.Close()
		c.Request.URL.Path = p
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
