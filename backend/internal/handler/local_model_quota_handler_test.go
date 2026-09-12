package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/config"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"
)

type quotaHTTPFake struct {
	calls  int
	userID int64
	err    error
}

func (s *quotaHTTPFake) Status(_ context.Context, id int64) (*service.LocalModelAccessStatus, error) {
	s.calls++
	s.userID = id
	if s.err != nil {
		return nil, s.err
	}
	n := int64(5)
	return &service.LocalModelAccessStatus{ContractVersion: 1, ServerTime: time.Now().UTC(), Quota: service.LocalModelQuota{Used: 0, Remaining: &n}}, nil
}
func (s *quotaHTTPFake) Authorize(_ context.Context, id int64, requestID string) (*service.LocalModelRequestAuthorization, error) {
	s.calls++
	s.userID = id
	if s.err != nil {
		if errors.Is(s.err, service.ErrCustomAPIDailyLimit) {
			return &service.LocalModelRequestAuthorization{RequestID: requestID, LocalModelAccessStatus: service.LocalModelAccessStatus{ServerTime: time.Now().UTC(), Quota: service.LocalModelQuota{Used: 5}}}, s.err
		}
		return nil, s.err
	}
	return &service.LocalModelRequestAuthorization{RequestID: requestID, Allowed: true, LocalModelAccessStatus: service.LocalModelAccessStatus{ServerTime: time.Now().UTC()}}, nil
}

type quotaHTTPUsers struct {
	*entitlementUserRepo
	failure error
}

func (r *quotaHTTPUsers) GetByID(ctx context.Context, id int64) (*service.User, error) {
	if r.failure != nil {
		return nil, r.failure
	}
	return r.entitlementUserRepo.GetByID(ctx, id)
}

func TestCustomAPIQuotaHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	u := &service.User{ID: 42, Status: service.StatusActive, Role: service.RoleUser, PasswordHash: "hash"}
	users := &quotaHTTPUsers{entitlementUserRepo: &entitlementUserRepo{users: map[int64]*service.User{42: u}}}
	cfg := &config.Config{}
	cfg.JWT.Secret = strings.Repeat("q", 32)
	cfg.JWT.AccessTokenExpireMinutes = 60
	auth := service.NewAuthService(nil, users, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(users, nil, nil, nil)
	quota := &quotaHTTPFake{}
	h := &LocalModelQuotaHandler{quota: quota}
	router := gin.New()
	router.Use(middleware2.LocalModelQuotaHeaders, middleware2.CSRFProtection(config.CSRFConfig{Enabled: true}))
	group := router.Group("/api/v1/local-model-access", gin.HandlerFunc(middleware2.NewJWTAuthMiddleware(auth, userService)))
	group.GET("", h.Status)
	group.POST("/requests", h.Authorize)
	token, err := auth.GenerateToken(u)
	require.NoError(t, err)
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/local-model-access"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("x-api-key", "not-a-login-token")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		_, err := http.ParseTime(w.Header().Get("Date"))
		require.NoError(t, err)
		return w
	}
	for _, bearer := range []string{"", "sk-model-key", token + "tampered"} {
		require.Equal(t, 401, request("GET", "", "", bearer).Code)
		require.Equal(t, 401, request("POST", "/requests", `{}`, bearer).Code)
	}
	require.Zero(t, quota.calls)
	csrfRequest := httptest.NewRequest("POST", "/api/v1/local-model-access/requests", strings.NewReader(`{}`))
	csrfRequest.Header.Set("Authorization", "Bearer "+token)
	csrfRequest.Header.Set("Origin", "https://unrelated.example")
	csrfResponse := httptest.NewRecorder()
	router.ServeHTTP(csrfResponse, csrfRequest)
	require.Equal(t, http.StatusForbidden, csrfResponse.Code)
	require.Equal(t, "no-store", csrfResponse.Header().Get("Cache-Control"))
	require.Zero(t, quota.calls)
	requestID := uuid.NewString()
	body := `{"request_id":"` + requestID + `"}`
	for _, invalid := range []string{`{}`, `null`, `[]`, `{`, `{"request_id":1}`, body + `{}`, `{"request_id":"x","request_id":"y"}`, `{"user_id":1,"request_id":"x"}`, `{"request_id":"x","unlocked":true}`, `{"request_id":"x","used":0}`, `{"request_id":"x","date":"2000-01-01"}`, `{"request_id":"x","free":true}`, `{"request_id":"x","amount":0}`, `{"request_id":"` + strings.Repeat("x", 1024) + `"}`} {
		w := request("POST", "/requests", invalid, token)
		require.Equal(t, 400, w.Code, invalid)
		require.Contains(t, w.Body.String(), "CUSTOM_API_INVALID_REQUEST")
	}
	require.Equal(t, 400, request("GET", "?user_id=1", "", token).Code)
	require.Equal(t, 400, request("POST", "/requests?free=true", body, token).Code)
	require.Zero(t, quota.calls)
	w := request("POST", "/requests", body, token)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"allowed":true`)
	require.Equal(t, int64(42), quota.userID)
	w = request("GET", "", "", token)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"remaining":5`)
	for _, tc := range []struct {
		err    error
		code   int
		reason string
	}{
		{service.ErrCustomAPIDailyLimit, 429, "CUSTOM_API_DAILY_LIMIT_EXCEEDED"},
		{service.ErrCustomAPIRateLimited.WithMetadata(map[string]string{"retry_after": "12"}), 429, "RATE_LIMITED"},
		{service.ErrCustomAPIAuthorizationRevoked, 403, "CUSTOM_API_AUTHORIZATION_REVOKED"},
		{service.ErrCustomAPIAuthorizationExpired, 410, "CUSTOM_API_AUTHORIZATION_EXPIRED"},
		{service.ErrCustomAPIQuotaUnavailable, 503, "CUSTOM_API_QUOTA_UNAVAILABLE"},
	} {
		quota.err = tc.err
		w = request("POST", "/requests", body, token)
		require.Equal(t, tc.code, w.Code)
		var response map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, tc.reason, response["reason"])
		require.NotContains(t, w.Body.String(), `"allowed":true`)
		if tc.reason == "RATE_LIMITED" {
			require.Equal(t, "12", w.Header().Get("Retry-After"))
		}
		if tc.reason == "CUSTOM_API_DAILY_LIMIT_EXCEEDED" {
			require.Contains(t, w.Body.String(), `"used":5`)
			require.Empty(t, w.Header().Get("Retry-After"))
		}
	}
	quota.err = nil
	u.Status = service.StatusDisabled
	w = request("POST", "/requests", body, token)
	require.Equal(t, 403, w.Code)
	var authFailure struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &authFailure))
	require.Equal(t, 403, authFailure.Code)
	require.Equal(t, "ACCOUNT_DISABLED", authFailure.Reason)
	u.Status = service.StatusActive
	users.failure = errors.New("SQL secret database credentials")
	w = request("POST", "/requests", body, token)
	require.Equal(t, 503, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &authFailure))
	require.Equal(t, 503, authFailure.Code)
	require.Equal(t, "CUSTOM_API_QUOTA_UNAVAILABLE", authFailure.Reason)
	require.NotContains(t, w.Body.String(), "SQL")
	require.NotContains(t, w.Body.String(), "credentials")
}
