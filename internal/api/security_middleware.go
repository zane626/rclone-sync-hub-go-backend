package api

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"rclone-sync-hub/internal/logger"
	"rclone-sync-hub/internal/model"
	"rclone-sync-hub/internal/repository"
	"rclone-sync-hub/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	claimsContextKey    = "auth_claims"
	requestIDContextKey = "request_id"
)

type SecurityMiddleware struct {
	auth         *security.AuthService
	auditRepo    repository.AuditLogRepository
	maxBodyBytes int64
	metricsToken string
}

func NewSecurityMiddleware(auth *security.AuthService, auditRepo repository.AuditLogRepository, maxBodyBytes int64, metricsToken string) *SecurityMiddleware {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 1024 * 1024
	}
	return &SecurityMiddleware{auth: auth, auditRepo: auditRepo, maxBodyBytes: maxBodyBytes, metricsToken: metricsToken}
}

func (m *SecurityMiddleware) MetricsAuthenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		provided := ""
		if len(header) >= 8 && strings.EqualFold(header[:7], "Bearer ") {
			provided = strings.TrimSpace(header[7:])
		}
		if m.metricsToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(m.metricsToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "metrics authentication required"})
			return
		}
		c.Next()
	}
}

func (m *SecurityMiddleware) Base() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 64 {
			requestID = randomRequestID()
		}
		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(c.Request.URL.Path, "/swagger/") {
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'")
		}
		if c.Request.Body != nil && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, m.maxBodyBytes)
		}
		c.Next()
	}
}

func (m *SecurityMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.auth.Enabled() {
			c.Set(claimsContextKey, security.Claims{Subject: "anonymous", Role: security.RoleAdmin})
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		claims, err := m.auth.Verify(strings.TrimSpace(header[7:]))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired access token"})
			return
		}
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func (m *SecurityMiddleware) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ClaimsFromContext(c).Role != security.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator role required"})
			return
		}
		c.Next()
	}
}

func (m *SecurityMiddleware) AuditMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			return
		}
		claims := ClaimsFromContext(c)
		requestID, _ := c.Get(requestIDContextKey)
		resource := c.FullPath()
		if resource == "" {
			resource = c.Request.URL.Path
		}
		entry := &model.AuditLog{
			RequestID:  stringValue(requestID),
			Actor:      claims.Subject,
			Role:       claims.Role,
			Action:     strings.ToLower(c.Request.Method),
			Resource:   resource,
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			StatusCode: c.Writer.Status(),
			ClientIP:   c.ClientIP(),
		}
		auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.auditRepo.Create(auditCtx, entry); err != nil {
			logger.L.Warn("audit log persistence failed", zap.String("request_id", entry.RequestID), zap.Error(err))
		}
	}
}

func ClaimsFromContext(c *gin.Context) security.Claims {
	if value, ok := c.Get(claimsContextKey); ok {
		if claims, valid := value.(security.Claims); valid {
			return claims
		}
	}
	return security.Claims{Subject: "anonymous", Role: security.RoleViewer}
}

func randomRequestID() string {
	buffer := make([]byte, 16)
	if _, err := cryptorand.Read(buffer); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buffer)
}

func stringValue(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
