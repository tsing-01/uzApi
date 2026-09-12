package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const localModelQuotaContract = "uzapi.local_model_quota_contract"

// Runs before CORS, CSRF and JWT, including failures before a handler is reached.
func LocalModelQuotaHeaders(c *gin.Context) {
	path := c.Request.URL.Path
	quota := path == "/api/v1/local-model-access" || path == "/api/v1/local-model-access/requests"
	entitlement := quota || path == "/api/v1/auth/me" || path == "/api/v1/redeem" || strings.HasPrefix(path, "/api/v1/local-model-access/") || (strings.HasPrefix(path, "/api/v1/admin/users/") && strings.Contains(path, "/local-model-access/"))
	if !entitlement {
		c.Next()
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Date", time.Now().UTC().Format(http.TimeFormat))
	if quota {
		c.Set(localModelQuotaContract, true)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
	}
	c.Next()
}
