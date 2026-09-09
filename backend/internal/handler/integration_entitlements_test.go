package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/config"
	"github.com/uzapi/internal/handler/dto"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"
)

type entitlementUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *entitlementUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	u := r.users[id]
	if u == nil {
		return nil, service.ErrUserNotFound
	}
	copy := *u
	return &copy, nil
}
func (r *entitlementUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func (r *entitlementUserRepo) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

func TestIntegrationEntitlementsAuthenticationAndIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	locked := &service.User{ID: 1, Status: service.StatusActive, PasswordHash: "password-hash"}
	paid := &service.User{ID: 2, Status: service.StatusActive, LocalModelAccessUnlockedAt: &now}
	repo := &entitlementUserRepo{users: map[int64]*service.User{1: locked, 2: paid}}
	cfg := &config.Config{}
	cfg.JWT.Secret = "entitlement-test-signing-key-32-bytes"
	cfg.JWT.AccessTokenExpireMinutes = 60
	auth := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	users := service.NewUserService(repo, nil, nil, nil)
	h := NewIntegrationHandler(users, nil, nil)
	router := gin.New()
	router.GET("/api/v1/integration/entitlements", gin.HandlerFunc(middleware2.NewJWTAuthMiddleware(auth, users)), h.Entitlements)
	token, err := auth.GenerateToken(locked)
	require.NoError(t, err)
	paidToken, err := auth.GenerateToken(paid)
	require.NoError(t, err)
	request := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/integration/entitlements?user_id=2&local_model_access_enabled=true", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, bearer := range []string{"", "sk-inference-api-key", token + "tampered"} {
		require.Equal(t, http.StatusUnauthorized, request(bearer).Code)
	}
	w := request(token)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var response struct {
		Data struct {
			UserID     int64      `json:"user_id"`
			Enabled    bool       `json:"local_model_access_enabled"`
			UnlockedAt *time.Time `json:"local_model_access_unlocked_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, int64(1), response.Data.UserID)
	require.False(t, response.Data.Enabled)
	require.Nil(t, response.Data.UnlockedAt)
	require.NotContains(t, w.Body.String(), "password")
	require.NoError(t, json.Unmarshal(request(paidToken).Body.Bytes(), &response))
	require.Equal(t, int64(2), response.Data.UserID)
	require.True(t, response.Data.Enabled)
	require.NotNil(t, response.Data.UnlockedAt)
	// A preexisting login token sees a newly granted entitlement immediately.
	locked.LocalModelAccessUnlockedAt = &now
	require.NoError(t, json.Unmarshal(request(token).Body.Bytes(), &response))
	require.True(t, response.Data.Enabled)
	require.True(t, dto.UserFromService(locked).LocalModelAccessEnabled)
	// Disabling, soft deleting, password rotation and deletion revoke access.
	locked.Status = service.StatusDisabled
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
	locked.Status = service.StatusActive
	locked.DeletedAt = &now
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
	locked.DeletedAt = nil
	locked.PasswordHash = "changed-password-hash"
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
	delete(repo.users, 1)
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
}
