package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/config"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"
)

type httpLicenseRepository struct {
	service.LocalModelLicenseRepository
	users map[int64]*service.User
	calls []int64
}
type httpLicenseStore struct {
	service.LocalModelLicenseStore
	userID int64
}

func (r *httpLicenseRepository) WithUser(_ context.Context, id int64, f func(*service.User, service.LocalModelLicenseStore) error) error {
	r.calls = append(r.calls, id)
	return f(r.users[id], httpLicenseStore{userID: id})
}
func (s httpLicenseStore) Devices(context.Context) ([]service.LocalModelDevice, error) {
	return []service.LocalModelDevice{{ID: strings.Repeat("a", 32), Name: "Owned device", UserID: s.userID}}, nil
}

func TestLocalLicenseHTTPAuthenticationIsolationAndBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	u := &service.User{ID: 1, Status: service.StatusActive, Role: service.RoleUser, PasswordHash: "hash"}
	admin := &service.User{ID: 2, Status: service.StatusActive, Role: service.RoleAdmin, LocalModelAccessRevokedAt: &now}
	users := map[int64]*service.User{1: u, 2: admin}
	repo := &entitlementUserRepo{users: users}
	cfg := &config.Config{LocalModelAccess: config.LocalModelAccessConfig{SigningSeed: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))), Issuer: "https://license.example.test"}}
	cfg.JWT.Secret = strings.Repeat("h", 32)
	cfg.JWT.AccessTokenExpireMinutes = 60
	auth := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(repo, nil, nil, nil)
	licensesRepo := &httpLicenseRepository{users: users}
	licenses, err := service.NewLocalModelLicenseService(licensesRepo, cfg)
	require.NoError(t, err)
	h := NewLocalModelLicenseHandler(licenses)
	router := gin.New()
	router.GET("/api/v1/local-model-access/keys", h.Keys)
	group := router.Group("/api/v1", gin.HandlerFunc(middleware2.NewJWTAuthMiddleware(auth, userService)))
	group.GET("/local-model-access/devices", h.Devices)
	group.POST("/local-model-access/challenges", h.Challenge)
	group.POST("/local-model-access/devices", h.Register)
	group.POST("/local-model-access/licenses", h.Issue)
	group.POST("/local-model-access/licenses/renew", h.Renew)
	group.DELETE("/local-model-access/devices/:device_id", h.RevokeDevice)
	group.POST("/admin/users/:id/local-model-access/revoke", h.RevokeEntitlement)
	token, err := auth.GenerateToken(u)
	require.NoError(t, err)
	adminToken, err := auth.GenerateToken(admin)
	require.NoError(t, err)
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		// Neither an administrator API key nor caller-supplied user fields replace login authentication.
		r.Header.Set("x-api-key", "fake-admin-api-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	endpoints := []struct{ method, path string }{{"GET", "/local-model-access/devices"}, {"POST", "/local-model-access/challenges"}, {"POST", "/local-model-access/devices"}, {"POST", "/local-model-access/licenses"}, {"POST", "/local-model-access/licenses/renew"}, {"DELETE", "/local-model-access/devices/abc"}, {"POST", "/admin/users/1/local-model-access/revoke"}}
	for _, ep := range endpoints {
		for _, bearer := range []string{"", "sk-inference-key", token + "tampered"} {
			require.Equal(t, 401, request(ep.method, ep.path, "{}", bearer).Code, ep.path)
		}
	}
	require.Empty(t, licensesRepo.calls)
	w := request("GET", "/local-model-access/devices?user_id=2", "", token)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, []int64{1}, licensesRepo.calls)
	require.NotContains(t, w.Body.String(), "user_id")
	require.NotContains(t, w.Body.String(), "PasswordHash")
	// Unpaid users cannot bypass the grant with request properties.
	w = request("POST", "/local-model-access/challenges", `{"action":"register","user_id":2,"local_model_access_enabled":true}`, token)
	require.Equal(t, http.StatusForbidden, w.Code)
	for _, body := range []string{`{`, `{"action":"other"}`, `{"action":"register","license":"` + strings.Repeat("x", 17000) + `"}`} {
		require.Equal(t, 400, request("POST", "/local-model-access/challenges", body, token).Code)
	}
	for _, path := range []string{"devices", "licenses", "licenses/renew"} {
		require.Equal(t, 400, request("POST", "/local-model-access/"+path, `{"name":"device","challenge":"bad","signature":"bad"}`, token).Code)
	}
	before := len(licensesRepo.calls)
	require.Equal(t, 403, request("POST", "/admin/users/1/local-model-access/revoke", `{"reason":"forged admin"}`, token).Code)
	require.Len(t, licensesRepo.calls, before)
	require.Equal(t, 400, request("POST", "/admin/users/1/local-model-access/revoke", `{}`, adminToken).Code)
	require.Equal(t, 400, request("POST", "/admin/users/invalid/local-model-access/revoke", `{"reason":"refund"}`, adminToken).Code)
	// Authorized idempotent admin revocation targets the path user, not the caller.
	require.Equal(t, 200, request("POST", "/admin/users/2/local-model-access/revoke", `{"reason":"refund"}`, adminToken).Code)
	require.Equal(t, int64(2), licensesRepo.calls[len(licensesRepo.calls)-1])
	w = request("GET", "/local-model-access/keys", "", "")
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), cfg.LocalModelAccess.SigningSeed)
	require.NotContains(t, w.Body.String(), `"d":`)
	u.Status = service.StatusDisabled
	require.Equal(t, 401, request("GET", "/local-model-access/devices", "", token).Code)
	u.Status = service.StatusActive
	u.PasswordHash = "changed"
	require.Equal(t, 401, request("GET", "/local-model-access/devices", "", token).Code)
}
