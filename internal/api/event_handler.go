package api

import (
	"net/http"
	"time"

	"rclone-sync-hub/internal/events"

	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	hub *events.Hub
}

func NewEventHandler(hub *events.Hub) *EventHandler {
	return &EventHandler{hub: hub}
}

// Stream emits task progress and status snapshots as Server-Sent Events.
// @Summary      订阅任务实时事件
// @Tags         events
// @Produce      text/event-stream
// @Success      200  {string}  string  "SSE stream"
// @Failure      503  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/events [get]
func (h *EventHandler) Stream(c *gin.Context) {
	channel, unsubscribe, err := h.hub.Subscribe()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many event subscribers"})
		return
	}
	defer unsubscribe()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	responseController := http.NewResponseController(c.Writer)
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-channel:
			if !ok {
				return
			}
			if err := responseController.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			c.SSEvent(event.Type, event)
			if err := responseController.Flush(); err != nil {
				return
			}
			// Clear both this per-write deadline and net/http's server-wide
			// WriteTimeout so an otherwise healthy SSE stream can live indefinitely.
			_ = responseController.SetWriteDeadline(time.Time{})
		case <-keepalive.C:
			if err := responseController.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
				return
			}
			if err := responseController.Flush(); err != nil {
				return
			}
			_ = responseController.SetWriteDeadline(time.Time{})
		}
	}
}
