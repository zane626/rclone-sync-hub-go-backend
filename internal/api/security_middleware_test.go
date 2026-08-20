package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsAuthenticateUsesDedicatedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := NewSecurityMiddleware(nil, nil, 1024, "01234567890123456789012345678901")
	router := gin.New()
	router.GET("/metrics", middleware.MetricsAuthenticate(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer 01234567890123456789012345678901")
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status=%d", authorized.Code)
	}
}

func TestBaseMiddlewareAddsSecurityHeadersAndLimitsBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := NewSecurityMiddleware(nil, nil, 4, "")
	router := gin.New()
	router.Use(middleware.Base())
	router.POST("/body", func(c *gin.Context) {
		buffer := make([]byte, 8)
		_, err := c.Request.Body.Read(buffer)
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("12345"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit status=%d", response.Code)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("missing security headers: %v", response.Header())
	}
}
