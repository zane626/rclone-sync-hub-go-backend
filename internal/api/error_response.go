package api

import (
	"net/http"

	"rclone-sync-hub/internal/apperror"
	"rclone-sync-hub/internal/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func writeInternalError(c *gin.Context, err error) {
	requestID, _ := c.Get(requestIDContextKey)
	logger.L.Error("api request failed", zap.String("request_id", stringValue(requestID)), zap.String("method", c.Request.Method), zap.String("path", c.Request.URL.Path), zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{
		"error":      "internal server error",
		"code":       "internal_error",
		"request_id": stringValue(requestID),
	})
}

func writeAPIError(c *gin.Context, err error) {
	if publicError, ok := apperror.As(err); ok {
		requestID, _ := c.Get(requestIDContextKey)
		logger.L.Warn("api operation rejected", zap.String("request_id", stringValue(requestID)), zap.String("code", publicError.Code), zap.Error(err))
		c.JSON(publicError.Status, gin.H{"error": publicError.Message, "code": publicError.Code, "request_id": stringValue(requestID)})
		return
	}
	writeInternalError(c, err)
}
