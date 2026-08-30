package service

import (
	"context"
	"time"

	"github.com/uzapi/internal/pkg/pagination"
)

// ActivationCode 激活码。
//
// 一码一人：管理员创建时设置生效日期与充值金额，用户登录后自行绑定；绑定时把 Amount
// 充入用户余额，并把该用户之前绑定的激活码置为 replaced。
type ActivationCode struct {
	ID        int64
	Code      string
	Amount    float64
	Status    string
	StartsAt  *time.Time
	ExpiresAt *time.Time
	UsedBy    *int64
	UsedAt    *time.Time
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NotStarted 报告激活码是否尚未到生效日期。
func (a *ActivationCode) NotStarted(now time.Time) bool {
	return a.StartsAt != nil && now.Before(*a.StartsAt)
}

// IsExpired 报告激活码是否已过失效日期。
func (a *ActivationCode) IsExpired(now time.Time) bool {
	return a.ExpiresAt != nil && !now.Before(*a.ExpiresAt)
}

// InEffectivePeriod 报告当前时间是否落在激活码的生效区间内。
func (a *ActivationCode) InEffectivePeriod(now time.Time) bool {
	return !a.NotStarted(now) && !a.IsExpired(now)
}

// UserLoginIP 用户登录过的客户端 IP 记录。
type UserLoginIP struct {
	ID         int64
	UserID     int64
	IP         string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// ActivationStatus 是「校验激活码有效性」接口的返回值：当前用户绑定了哪个激活码、
// 是否仍然有效、以及已占用的登录 IP。
type ActivationStatus struct {
	Bound        bool
	Valid        bool
	Reason       string
	Code         *ActivationCode
	LoginIPs     []UserLoginIP
	LoginIPLimit int
}

// 激活码校验失败原因（Reason 字段取值）。
const (
	ActivationReasonNotBound   = "NOT_BOUND"
	ActivationReasonNotStarted = "NOT_STARTED"
	ActivationReasonExpired    = "EXPIRED"
	ActivationReasonDisabled   = "DISABLED"
	ActivationReasonReplaced   = "REPLACED"
)

// CreateActivationCodeInput 创建激活码输入。
type CreateActivationCodeInput struct {
	Code      string
	Amount    float64
	StartsAt  *time.Time
	ExpiresAt *time.Time
	Notes     string
	Count     int
}

// UpdateActivationCodeInput 更新激活码输入。
type UpdateActivationCodeInput struct {
	Amount    *float64
	Status    *string
	StartsAt  NullableTimeUpdate
	ExpiresAt NullableTimeUpdate
	Notes     *string
}

// ActivationCodeRepository 激活码仓储接口。
type ActivationCodeRepository interface {
	Create(ctx context.Context, code *ActivationCode) error
	CreateBatch(ctx context.Context, codes []ActivationCode) error
	GetByID(ctx context.Context, id int64) (*ActivationCode, error)
	GetByCode(ctx context.Context, code string) (*ActivationCode, error)
	// GetByCodeForUpdate 带行锁查询，用于并发绑定时的互斥。
	GetByCodeForUpdate(ctx context.Context, code string) (*ActivationCode, error)
	Update(ctx context.Context, code *ActivationCode) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]ActivationCode, *pagination.PaginationResult, error)

	// GetCurrentByUser 返回用户当前绑定（status=used）的激活码，没有则返回 ErrActivationCodeNotFound。
	GetCurrentByUser(ctx context.Context, userID int64) (*ActivationCode, error)
	// Bind 以乐观锁方式把 unused 的激活码绑定到用户，已被抢占时返回 ErrActivationCodeUsed。
	Bind(ctx context.Context, id, userID int64, usedAt time.Time) error
	// ReplacePrevious 把用户此前绑定的激活码（排除 exceptID）标记为 replaced，返回受影响行数。
	ReplacePrevious(ctx context.Context, userID, exceptID int64) (int, error)
}

// UserLoginIPRepository 用户登录 IP 仓储接口。
type UserLoginIPRepository interface {
	ListByUser(ctx context.Context, userID int64) ([]UserLoginIP, error)
	// Touch 更新已存在记录的 last_seen_at，返回是否命中已有记录。
	Touch(ctx context.Context, userID int64, ip string) (bool, error)
	Create(ctx context.Context, userID int64, ip string) error
	DeleteByUser(ctx context.Context, userID int64) (int, error)
}
