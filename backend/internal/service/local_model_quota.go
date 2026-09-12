package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	infraerrors "github.com/uzapi/internal/pkg/errors"
)

const (
	CustomAPIContractVersion  = 1
	CustomAPIDailyLimit       = 5
	CustomAPIRateLimit        = 120 // Per account/minute, shared by status, new IDs and retries.
	CustomAPIAuthorizationTTL = 30 * time.Second
)

var (
	ErrCustomAPIInvalidRequest       = infraerrors.BadRequest("CUSTOM_API_INVALID_REQUEST", "request_id must be a UUID v4; no other fields are accepted")
	ErrCustomAPIAccountDisabled      = infraerrors.Forbidden("ACCOUNT_DISABLED", "账户不可用。")
	ErrCustomAPIAuthorizationRevoked = infraerrors.Forbidden("CUSTOM_API_AUTHORIZATION_REVOKED", "本次批准的权限版本已失效，请重新开始操作。")
	ErrCustomAPIAuthorizationExpired = infraerrors.New(http.StatusGone, "CUSTOM_API_AUTHORIZATION_EXPIRED", "本次批准已到期，请重新开始操作。")
	ErrCustomAPIDailyLimit           = infraerrors.TooManyRequests("CUSTOM_API_DAILY_LIMIT_EXCEEDED", "今天的 5 次自定义 API 试用已用完，请兑换解锁码或切换到账户模型。")
	ErrCustomAPIRateLimited          = infraerrors.TooManyRequests("RATE_LIMITED", "授权接口请求过于频繁，请稍后重试。")
	ErrCustomAPIQuotaUnavailable     = infraerrors.New(http.StatusServiceUnavailable, "CUSTOM_API_QUOTA_UNAVAILABLE", "自定义 API 授权暂不可用，请保留原请求 ID 后重试。")
)

type LocalModelQuota struct {
	Date       string    `json:"date"`
	Timezone   string    `json:"timezone"`
	DailyLimit int       `json:"daily_limit"`
	Used       int64     `json:"used"`
	Remaining  *int64    `json:"remaining"`
	ResetsAt   time.Time `json:"resets_at"`
}

type LocalModelAccessStatus struct {
	ContractVersion    int             `json:"contract_version"`
	Enabled            bool            `json:"local_model_access_enabled"`
	EntitlementVersion int64           `json:"entitlement_version"`
	ServerTime         time.Time       `json:"server_time"`
	Quota              LocalModelQuota `json:"quota"`
}

type LocalModelRequestAuthorization struct {
	LocalModelAccessStatus
	RequestID    string     `json:"request_id"`
	Allowed      bool       `json:"allowed"`
	Replayed     bool       `json:"replayed"`
	AuthorizedAt *time.Time `json:"authorized_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type LocalModelRequestDecision struct {
	RequestID          string
	Allowed            bool
	Enabled            bool
	EntitlementVersion int64
	Used               int64
	DecidedAt          time.Time
	ExpiresAt          time.Time
}

type LocalModelQuotaRepository interface {
	WithUser(context.Context, int64, func(*User, LocalModelQuotaStore) error) error
}

type LocalModelQuotaStore interface {
	Now(context.Context) (time.Time, error)
	TakeRateLimit(context.Context, string, time.Time) (bool, error)
	Used(context.Context, string) (int64, error)
	Decision(context.Context, string) (*LocalModelRequestDecision, error)
	SaveDecision(context.Context, string, *LocalModelRequestDecision) error
}

type LocalModelQuotaService struct {
	repo     LocalModelQuotaRepository
	location *time.Location
}

func NewLocalModelQuotaService(repo LocalModelQuotaRepository) (*LocalModelQuotaService, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	return &LocalModelQuotaService{repo: repo, location: location}, nil
}

func (s *LocalModelQuotaService) status(now time.Time, enabled bool, version, used int64) LocalModelAccessStatus {
	local := now.In(s.location)
	reset := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.location).UTC()
	var remaining *int64
	if !enabled {
		n := int64(CustomAPIDailyLimit) - used
		if n < 0 {
			n = 0
		}
		remaining = &n
	}
	return LocalModelAccessStatus{ContractVersion: CustomAPIContractVersion, Enabled: enabled, EntitlementVersion: version, ServerTime: now.UTC(),
		Quota: LocalModelQuota{Date: local.Format(time.DateOnly), Timezone: "Asia/Shanghai", DailyLimit: CustomAPIDailyLimit, Used: used, Remaining: remaining, ResetsAt: reset}}
}

// The DB clock is read AFTER acquiring the users row lock, including across midnight.
func (s *LocalModelQuotaService) prepare(ctx context.Context, u *User, store LocalModelQuotaStore) (time.Time, error) {
	if u == nil || !u.IsActive() || u.DeletedAt != nil {
		return time.Time{}, ErrCustomAPIAccountDisabled
	}
	now, err := store.Now(ctx)
	if err != nil {
		return time.Time{}, err
	}
	date := now.In(s.location).Format(time.DateOnly)
	allowed, err := store.TakeRateLimit(ctx, date, now.Truncate(time.Minute))
	if err != nil {
		return time.Time{}, err
	}
	if !allowed {
		seconds := max(1, int(math.Ceil(now.Truncate(time.Minute).Add(time.Minute).Sub(now).Seconds())))
		return now, ErrCustomAPIRateLimited.WithMetadata(map[string]string{"retry_after": strconv.Itoa(seconds)})
	}
	return now, nil
}

func customAPIStorageError(err error) error {
	if errors.Is(err, ErrUserNotFound) {
		return ErrCustomAPIAccountDisabled
	}
	// Do not expose driver errors, SQL or a speculative quota after uncertain commits.
	return ErrCustomAPIQuotaUnavailable
}

func (s *LocalModelQuotaService) Status(ctx context.Context, userID int64) (*LocalModelAccessStatus, error) {
	var out *LocalModelAccessStatus
	var decisionErr error
	err := s.repo.WithUser(ctx, userID, func(u *User, store LocalModelQuotaStore) error {
		now, err := s.prepare(ctx, u, store)
		if err != nil {
			if errors.Is(err, ErrCustomAPIRateLimited) || errors.Is(err, ErrCustomAPIAccountDisabled) {
				decisionErr = err
				return nil
			}
			return err
		}
		status := s.status(now, u.HasLocalModelAccess(), u.LocalModelAccessVersion, 0)
		used, err := store.Used(ctx, status.Quota.Date)
		if err != nil {
			return err
		}
		status = s.status(now, u.HasLocalModelAccess(), u.LocalModelAccessVersion, used)
		out = &status
		return nil
	})
	if err != nil {
		return nil, customAPIStorageError(err)
	}
	return out, decisionErr
}

func (s *LocalModelQuotaService) Authorize(ctx context.Context, userID int64, requestID string) (*LocalModelRequestAuthorization, error) {
	id, err := uuid.Parse(requestID)
	if err != nil || len(requestID) != 36 || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
		return nil, ErrCustomAPIInvalidRequest
	}
	requestID = id.String()
	var out *LocalModelRequestAuthorization
	var decisionErr error
	err = s.repo.WithUser(ctx, userID, func(u *User, store LocalModelQuotaStore) error {
		now, err := s.prepare(ctx, u, store)
		if err != nil {
			if errors.Is(err, ErrCustomAPIRateLimited) || errors.Is(err, ErrCustomAPIAccountDisabled) {
				decisionErr = err
				return nil
			}
			return err
		}
		record, err := store.Decision(ctx, requestID)
		if err != nil {
			return err
		}
		replayed := record != nil
		if record == nil {
			status := s.status(now, u.HasLocalModelAccess(), u.LocalModelAccessVersion, 0)
			used, err := store.Used(ctx, status.Quota.Date)
			if err != nil {
				return err
			}
			allowed := u.HasLocalModelAccess() || used < CustomAPIDailyLimit
			if allowed {
				if used == math.MaxInt64 {
					return ErrCustomAPIQuotaUnavailable
				}
				used++
			}
			expires := now.Add(CustomAPIAuthorizationTTL)
			if expires.After(status.Quota.ResetsAt) {
				expires = status.Quota.ResetsAt
			}
			record = &LocalModelRequestDecision{RequestID: requestID, Allowed: allowed, Enabled: u.HasLocalModelAccess(), EntitlementVersion: u.LocalModelAccessVersion,
				Used: used, DecidedAt: now, ExpiresAt: expires}
			if err = store.SaveDecision(ctx, status.Quota.Date, record); err != nil {
				return err
			}
		}
		// A denied ID stays denied across grants and midnight. Only successful approvals
		// can become revoked/expired; neither path increments usage or extends expiry.
		if !record.Allowed {
			decisionErr = ErrCustomAPIDailyLimit
		} else if record.EntitlementVersion != u.LocalModelAccessVersion {
			decisionErr = ErrCustomAPIAuthorizationRevoked
		} else if !record.ExpiresAt.After(now) {
			decisionErr = ErrCustomAPIAuthorizationExpired
		}
		out = &LocalModelRequestAuthorization{LocalModelAccessStatus: s.status(record.DecidedAt, record.Enabled, record.EntitlementVersion, record.Used),
			RequestID: requestID, Allowed: decisionErr == nil, Replayed: replayed}
		out.ServerTime = now.UTC()
		if record.Allowed {
			out.AuthorizedAt = &record.DecidedAt
			out.ExpiresAt = &record.ExpiresAt
		}
		return nil
	})
	if err != nil {
		return nil, customAPIStorageError(err)
	}
	return out, decisionErr
}
