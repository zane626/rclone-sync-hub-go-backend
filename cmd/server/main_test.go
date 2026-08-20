package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestServeFrontendDistinguishesRoutesAndStaticAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	staticFS := fs.FS(fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	})
	router := gin.New()
	router.NoRoute(serveFrontend(staticFS))

	tests := []struct {
		path        string
		status      int
		body        string
		cacheHeader string
	}{
		{path: "/dashboard", status: http.StatusOK, body: "<html>app</html>", cacheHeader: "no-cache"},
		{path: "/assets/app.js", status: http.StatusOK, body: "console.log('app')", cacheHeader: "immutable"},
		{path: "/assets/missing.js", status: http.StatusNotFound},
		{path: "/api/missing", status: http.StatusNotFound, body: "api endpoint not found"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || test.body != "" && !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if test.cacheHeader != "" && !strings.Contains(response.Header().Get("Cache-Control"), test.cacheHeader) {
				t.Fatalf("Cache-Control=%q", response.Header().Get("Cache-Control"))
			}
		})
	}
}
