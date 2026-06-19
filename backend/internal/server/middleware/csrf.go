package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/uzapi/internal/config"
)

const (
	CSRFTokenCookieName = "csrf_token"
	CSRFTokenHeaderName = "X-CSRF-Token"
)

// CSRFProtection provides browser-side CSRF protection without breaking API-key clients.
// The app primarily authenticates with Authorization: Bearer tokens, which are not sent
// automatically by browsers. This middleware still protects cookie-assisted browser flows by:
//   - rejecting unsafe browser requests with a cross-site Origin/Referer
//   - issuing a SameSite=Lax csrf_token cookie
//   - requiring X-CSRF-Token to match the cookie when the cookie is present
func CSRFProtection(cfg config.CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Enabled || c.Request == nil {
			c.Next()
			return
		}

		ensureCSRFCookie(c)

		if isSafeMethod(c.Request.Method) || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// Non-browser service clients authenticate with explicit headers and do not send
		// browser cookies automatically, so CSRF does not apply to those calls.
		if c.GetHeader("Cookie") == "" && hasExplicitAPIAuth(c) && !hasBrowserOriginSignal(c) {
			c.Next()
			return
		}

		if !isSameOriginRequest(c) {
			AbortWithError(c, http.StatusForbidden, "CSRF_FORBIDDEN", "Cross-site request blocked")
			return
		}

		cookieToken, err := c.Cookie(CSRFTokenCookieName)
		if err != nil {
			if strings.TrimSpace(c.GetHeader("Cookie")) != "" {
				AbortWithError(c, http.StatusForbidden, "CSRF_TOKEN_MISSING", "Missing CSRF token")
				return
			}
		} else if strings.TrimSpace(cookieToken) != "" {
			headerToken := strings.TrimSpace(c.GetHeader(CSRFTokenHeaderName))
			if !csrfTokensEqual(cookieToken, headerToken) {
				AbortWithError(c, http.StatusForbidden, "CSRF_TOKEN_INVALID", "Invalid CSRF token")
				return
			}
		}

		c.Next()
	}
}

func ensureCSRFCookie(c *gin.Context) {
	if _, err := c.Cookie(CSRFTokenCookieName); err == nil {
		return
	}
	token, err := generateCSRFToken()
	if err != nil {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CSRFTokenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   12 * 60 * 60,
		Secure:   requestIsHTTPS(c),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func hasExplicitAPIAuth(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("Authorization")) != "" ||
		strings.TrimSpace(c.GetHeader("X-API-Key")) != "" ||
		strings.TrimSpace(c.GetHeader("X-Goog-API-Key")) != ""
}

func hasBrowserOriginSignal(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("Origin")) != "" || strings.TrimSpace(c.GetHeader("Referer")) != ""
}

func isSameOriginRequest(c *gin.Context) bool {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin != "" {
		return originMatchesRequest(c, origin)
	}

	referer := strings.TrimSpace(c.GetHeader("Referer"))
	if referer != "" {
		return originMatchesRequest(c, referer)
	}

	// Non-browser requests typically have neither Origin nor Referer.
	return true
}

func originMatchesRequest(c *gin.Context, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	requestHost := strings.ToLower(strings.TrimSpace(c.Request.Host))
	if requestHost == "" {
		return false
	}
	if strings.ToLower(parsed.Host) != requestHost {
		return false
	}

	requestScheme := requestScheme(c)
	return strings.EqualFold(parsed.Scheme, requestScheme)
}

func requestScheme(c *gin.Context) string {
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		if idx := strings.Index(proto, ","); idx >= 0 {
			proto = proto[:idx]
		}
		proto = strings.ToLower(strings.TrimSpace(proto))
		if proto == "http" || proto == "https" {
			return proto
		}
	}
	if c.Request != nil && c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func requestIsHTTPS(c *gin.Context) bool {
	return requestScheme(c) == "https"
}

func csrfTokensEqual(a, b string) bool {
	if a == "" || b == "" || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
