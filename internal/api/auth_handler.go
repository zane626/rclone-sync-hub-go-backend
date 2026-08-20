package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"rclone-sync-hub/internal/security"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	auth        *security.AuthService
	maxAttempts int
	mu          sync.Mutex
	attempts    map[string]loginAttemptWindow
}

type loginAttemptWindow struct {
	Count   int
	ResetAt time.Time
}

const maxTrackedLoginSources = 10000

func NewAuthHandler(auth *security.AuthService, maxAttempts int) *AuthHandler {
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	return &AuthHandler{auth: auth, maxAttempts: maxAttempts, attempts: make(map[string]loginAttemptWindow)}
}

type LoginRequest struct {
	Username string `json:"username" binding:"required,max=255"`
	Password string `json:"password" binding:"required,max=1024"`
}

// Login authenticates a configured local account and issues a signed token.
// @Summary      登录
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "用户名和密码"
// @Success      200   {object}  map[string]interface{}
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /api/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	if !h.auth.Enabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "authentication is disabled"})
		return
	}
	clientIP := c.ClientIP()
	if !h.allowAttempt(clientIP) {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}
	var request LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login request"})
		return
	}
	token, claims, err := h.auth.Login(strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	h.clearAttempts(clientIP)
	c.JSON(http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   claims.ExpiresAt,
		"user":         gin.H{"username": claims.Subject, "role": claims.Role},
	})
}

// Config reports whether authentication is enabled.
// @Summary      获取认证配置
// @Tags         auth
// @Produce      json
// @Success      200  {object}  map[string]bool
// @Router       /api/auth/config [get]
func (h *AuthHandler) Config(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.auth.Enabled()})
}

// Me returns the authenticated principal.
// @Summary      获取当前用户
// @Tags         auth
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]string
// @Security     BearerAuth
// @Router       /api/auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	claims := ClaimsFromContext(c)
	c.JSON(http.StatusOK, gin.H{"username": claims.Subject, "role": claims.Role, "expires_at": claims.ExpiresAt})
}

func (h *AuthHandler) allowAttempt(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	// Keep the in-memory limiter bounded if an attacker rotates source addresses.
	if len(h.attempts) >= maxTrackedLoginSources {
		for attemptKey, attempt := range h.attempts {
			if attempt.ResetAt.Before(now) {
				delete(h.attempts, attemptKey)
			}
		}
		if _, tracked := h.attempts[key]; !tracked && len(h.attempts) >= maxTrackedLoginSources {
			return false
		}
	}
	window := h.attempts[key]
	if window.ResetAt.Before(now) {
		window = loginAttemptWindow{ResetAt: now.Add(time.Minute)}
	}
	window.Count++
	h.attempts[key] = window
	return window.Count <= h.maxAttempts
}

func (h *AuthHandler) clearAttempts(key string) {
	h.mu.Lock()
	delete(h.attempts, key)
	h.mu.Unlock()
}
