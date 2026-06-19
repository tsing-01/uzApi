package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/config"
)

func newCSRFRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CSRFProtection(config.CSRFConfig{Enabled: true}))
	r.GET("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/change", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestCSRFProtectionIssuesCookieOnSafeRequest(t *testing.T) {
	r := newCSRFRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/ping", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	require.Equal(t, CSRFTokenCookieName, cookies[0].Name)
	require.False(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
}

func TestCSRFProtectionBlocksCrossSiteOrigin(t *testing.T) {
	r := newCSRFRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/change", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://evil.example")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestCSRFProtectionRequiresMatchingHeaderWhenCookiePresent(t *testing.T) {
	r := newCSRFRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/change", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(&http.Cookie{Name: CSRFTokenCookieName, Value: "token-1"})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/change", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(&http.Cookie{Name: CSRFTokenCookieName, Value: "token-1"})
	req.Header.Set(CSRFTokenHeaderName, "token-1")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestCSRFProtectionAllowsServiceClientBearerWithoutBrowserSignals(t *testing.T) {
	r := newCSRFRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/change", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer sk-test")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
