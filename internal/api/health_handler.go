package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	db        *sql.DB
	startedAt time.Time
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db, startedAt: time.Now()}
}

// Live reports process liveness without touching dependencies.
// @Summary      存活检查
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /api/health/live [get]
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "check": "liveness", "uptime_seconds": int64(time.Since(h.startedAt).Seconds())})
}

// Ready reports whether the database is reachable.
// @Summary      就绪检查
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      503  {object}  map[string]string
// @Router       /api/health/ready [get]
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if h.db == nil || h.db.PingContext(ctx) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "check": "database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "check": "readiness"})
}

// Ping is the backward-compatible readiness endpoint.
// @Summary      兼容健康检查
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      503  {object}  map[string]string
// @Router       /api/health [get]
func (h *HealthHandler) Ping(c *gin.Context) {
	h.Ready(c)
}
