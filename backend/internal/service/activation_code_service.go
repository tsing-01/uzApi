package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/uzapi/ent"
	"github.com/uzapi/internal/domain"
	infraerrors "github.com/uzapi/internal/pkg/errors"
	"github.com/uzapi/internal/pkg/logger"
	"github.com/uzapi/internal/pkg/pagination"
)

var (
	ErrActivationCodeNotFound   = infraerrors.NotFound("ACTIVATION_CODE_NOT_FOUND", "activation code not found")
	ErrActivationCodeUsed       = infraerrors.Conflict("ACTIVATION_CODE_USED", "activation code already used")
	ErrActivationCodeDisabled   = infraerrors.BadRequest("ACTIVATION_CODE_DISABLED", "activation code is disabled")
	ErrActivationCodeNotStarted = infraerrors.BadRequest("ACTIVATION_CODE_NOT_STARTED", "activation code is not effective yet")
	ErrActivationCodeExpired    = infraerrors.BadRequest("ACTIVATION_CODE_EXPIRED", "activation code has expired")
	ErrActivationCodeInvalid    = infraerrors.BadRequest("ACTIVATION_CODE_INVALID", "invalid activation code")
	// ErrActivationCodeInactive 用于登录拦截：用户绑定过激活码，但当前这张已失效。
	ErrActivationCodeInactive = infraerrors.Forbidden("ACTIVATION_CODE_INACTIVE", "your activation code is no longer valid, please bind a new one")
	ErrLoginIPLimitExceeded   = infraerrors.Forbidden("LOGIN_IP_LIMIT_EXCEEDED", "too many login IP addresses for this account")
)

// maxActivationCodeBatch 单次批量创建激活码的上限。
const maxActivationCodeBatch = 200

// ActivationCodeService 激活码服务：管理员创建/维护激活码，用户绑定激活码充值，
// 并在登录时校验激活码有效性与登录 IP 数量。
type ActivationCodeService struct {
	activationRepo       ActivationCodeRepository
	loginIPRepo          UserLoginIPRepository
	userRepo             UserRepository
	billingCacheService  *BillingCacheService
	entClient            *dbent.Client
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewActivationCodeService 创建激活码服务实例。
func NewActivationCodeService(
	activationRepo ActivationCodeRepository,
	loginIPRepo UserLoginIPRepository,
	userRepo UserRepository,
	billingCacheService *BillingCacheService,
	entClient *dbent.Client,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *ActivationCodeService {
	return &ActivationCodeService{
		activationRepo:       activationRepo,
		loginIPRepo:          loginIPRepo,
		userRepo:             userRepo,
		billingCacheService:  billingCacheService,
		entClient:            entClient,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// GenerateRandomCode 生成随机激活码。
func (s *ActivationCodeService) GenerateRandomCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}

// Create 创建激活码。Count > 1 时批量生成随机码（此时忽略 input.Code）。
func (s *ActivationCodeService) Create(ctx context.Context, input *CreateActivationCodeInput) ([]ActivationCode, error) {
	if input == nil {
		return nil, ErrActivationCodeInvalid
	}
	if input.Amount < 0 {
		return nil, infraerrors.BadRequest("ACTIVATION_CODE_INVALID_AMOUNT", "amount must not be negative")
	}
	if input.StartsAt != nil && input.ExpiresAt != nil && !input.ExpiresAt.After(*input.StartsAt) {
		return nil, infraerrors.BadRequest("ACTIVATION_CODE_INVALID_PERIOD", "expires_at must be after starts_at")
	}

	count := input.Count
	if count <= 0 {
		count = 1
	}
	if count > maxActivationCodeBatch {
		return nil, infraerrors.BadRequest("ACTIVATION_CODE_BATCH_TOO_LARGE", fmt.Sprintf("count must not exceed %d", maxActivationCodeBatch))
	}

	codes := make([]ActivationCode, 0, count)
	for i := 0; i < count; i++ {
		code := strings.ToUpper(strings.TrimSpace(input.Code))
		if count > 1 || code == "" {
			generated, err := s.GenerateRandomCode()
			if err != nil {
				return nil, err
			}
			code = generated
		}
		codes = append(codes, ActivationCode{
			Code:      code,
			Amount:    input.Amount,
			Status:    domain.ActivationCodeStatusUnused,
			StartsAt:  input.StartsAt,
			ExpiresAt: input.ExpiresAt,
			Notes:     input.Notes,
		})
	}

	if len(codes) == 1 {
		if err := s.activationRepo.Create(ctx, &codes[0]); err != nil {
			return nil, err
		}
		return codes, nil
	}
	if err := s.activationRepo.CreateBatch(ctx, codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// GetByID 按 ID 获取激活码。
func (s *ActivationCodeService) GetByID(ctx context.Context, id int64) (*ActivationCode, error) {
	return s.activationRepo.GetByID(ctx, id)
}

// List 分页查询激活码，支持按状态过滤与按码/备注搜索。
func (s *ActivationCodeService) List(
	ctx context.Context,
	params pagination.PaginationParams,
	status, search string,
) ([]ActivationCode, *pagination.PaginationResult, error) {
	return s.activationRepo.List(ctx, params, status, search)
}

// Update 更新激活码。已绑定的激活码只允许改状态、有效期与备注，金额不再可改
// （金额已在绑定时充入用户余额，改动不会回溯）。
func (s *ActivationCodeService) Update(ctx context.Context, id int64, input *UpdateActivationCodeInput) (*ActivationCode, error) {
	if input == nil {
		return nil, ErrActivationCodeInvalid
	}
	code, err := s.activationRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Amount != nil {
		if code.Status != domain.ActivationCodeStatusUnused {
			return nil, infraerrors.BadRequest("ACTIVATION_CODE_AMOUNT_LOCKED", "amount can only be changed while the code is unused")
		}
		if *input.Amount < 0 {
			return nil, infraerrors.BadRequest("ACTIVATION_CODE_INVALID_AMOUNT", "amount must not be negative")
		}
		code.Amount = *input.Amount
	}
	if input.Status != nil {
		switch *input.Status {
		case domain.ActivationCodeStatusUnused, domain.ActivationCodeStatusDisabled:
			code.Status = *input.Status
		default:
			return nil, infraerrors.BadRequest("ACTIVATION_CODE_INVALID_STATUS", "status must be unused or disabled")
		}
	}
	if input.StartsAt.Set {
		code.StartsAt = input.StartsAt.Value
	}
	if input.ExpiresAt.Set {
		code.ExpiresAt = input.ExpiresAt.Value
	}
	if input.Notes != nil {
		code.Notes = *input.Notes
	}
	if code.StartsAt != nil && code.ExpiresAt != nil && !code.ExpiresAt.After(*code.StartsAt) {
		return nil, infraerrors.BadRequest("ACTIVATION_CODE_INVALID_PERIOD", "expires_at must be after starts_at")
	}

	if err := s.activationRepo.Update(ctx, code); err != nil {
		return nil, err
	}
	return code, nil
}

// Delete 删除激活码。
func (s *ActivationCodeService) Delete(ctx context.Context, id int64) error {
	return s.activationRepo.Delete(ctx, id)
}

// GetStatus 返回用户当前绑定的激活码及其有效性，供「校验激活码有效性」接口使用。
func (s *ActivationCodeService) GetStatus(ctx context.Context, userID int64) (*ActivationStatus, error) {
	out := &ActivationStatus{LoginIPLimit: domain.UserLoginIPLimit}

	ips, err := s.loginIPRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out.LoginIPs = ips

	code, err := s.activationRepo.GetCurrentByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrActivationCodeNotFound) {
			out.Reason = ActivationReasonNotBound
			return out, nil
		}
		return nil, err
	}

	out.Bound = true
	out.Code = code
	out.Reason = activationInvalidReason(code, time.Now())
	out.Valid = out.Reason == ""
	return out, nil
}

// activationInvalidReason 返回激活码失效原因，空字符串表示有效。
func activationInvalidReason(code *ActivationCode, now time.Time) string {
	switch {
	case code == nil:
		return ActivationReasonNotBound
	case code.Status == domain.ActivationCodeStatusDisabled:
		return ActivationReasonDisabled
	case code.Status == domain.ActivationCodeStatusReplaced:
		return ActivationReasonReplaced
	case code.NotStarted(now):
		return ActivationReasonNotStarted
	case code.IsExpired(now):
		return ActivationReasonExpired
	default:
		return ""
	}
}

// ActivationBindResult 绑定激活码的结果。
type ActivationBindResult struct {
	Code       *ActivationCode
	NewBalance float64
	// ReplacedCount 本次被替换掉的历史激活码数量。
	ReplacedCount int
}

// Bind 为用户绑定激活码：校验有效性后把金额充入余额，把该用户之前的激活码标记为
// replaced，并清空其登录 IP 记录（换码等于重新分配可用 IP 名额）。
func (s *ActivationCodeService) Bind(ctx context.Context, userID int64, rawCode string) (*ActivationBindResult, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return nil, ErrActivationCodeInvalid
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	activation, err := s.activationRepo.GetByCodeForUpdate(txCtx, code)
	if err != nil {
		return nil, err
	}
	if err := validateActivationForBind(activation, time.Now()); err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.activationRepo.Bind(txCtx, activation.ID, userID, now); err != nil {
		return nil, err
	}

	replaced, err := s.activationRepo.ReplacePrevious(txCtx, userID, activation.ID)
	if err != nil {
		return nil, fmt.Errorf("replace previous activation codes: %w", err)
	}

	if activation.Amount != 0 {
		if err := s.userRepo.UpdateBalance(txCtx, userID, activation.Amount); err != nil {
			return nil, fmt.Errorf("update user balance: %w", err)
		}
	}

	if _, err := s.loginIPRepo.DeleteByUser(txCtx, userID); err != nil {
		return nil, fmt.Errorf("reset login ips: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	activation.Status = domain.ActivationCodeStatusUsed
	activation.UsedBy = &userID
	activation.UsedAt = &now

	s.invalidateBalanceCaches(ctx, userID, activation.Amount)

	result := &ActivationBindResult{Code: activation, ReplacedCount: replaced}
	if user, err := s.userRepo.GetByID(ctx, userID); err == nil && user != nil {
		result.NewBalance = user.Balance
	}
	return result, nil
}

// validateActivationForBind 校验激活码当前是否可被绑定。
func validateActivationForBind(code *ActivationCode, now time.Time) error {
	if code == nil {
		return ErrActivationCodeNotFound
	}
	switch code.Status {
	case domain.ActivationCodeStatusUnused:
	case domain.ActivationCodeStatusDisabled:
		return ErrActivationCodeDisabled
	default:
		return ErrActivationCodeUsed
	}
	if code.NotStarted(now) {
		return ErrActivationCodeNotStarted
	}
	if code.IsExpired(now) {
		return ErrActivationCodeExpired
	}
	return nil
}

// EnsureLoginAllowed 登录时的准入校验，按顺序检查两件事：
//  1. 用户当前绑定的激活码是否仍在有效期内（从未绑定过激活码的用户不受影响，
//     管理员始终豁免）；
//  2. 该用户登录过的不同 IP 是否超过 domain.UserLoginIPLimit 个。
//
// 校验通过时会记录/刷新本次登录 IP。
func (s *ActivationCodeService) EnsureLoginAllowed(ctx context.Context, user *User, clientIP string) error {
	if s == nil || user == nil || user.IsAdmin() {
		return nil
	}

	activation, err := s.activationRepo.GetCurrentByUser(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrActivationCodeNotFound) {
			// 未参与激活码体系的用户不受激活码与 IP 限制影响。
			return nil
		}
		return err
	}
	if reason := activationInvalidReason(activation, time.Now()); reason != "" {
		return infraerrors.Forbidden("ACTIVATION_CODE_INACTIVE", "activation code is not valid: "+reason)
	}

	return s.recordLoginIP(ctx, user.ID, clientIP)
}

// recordLoginIP 记录登录 IP，超过上限的新 IP 返回 ErrLoginIPLimitExceeded。
func (s *ActivationCodeService) recordLoginIP(ctx context.Context, userID int64, clientIP string) error {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		return nil
	}

	touched, err := s.loginIPRepo.Touch(ctx, userID, clientIP)
	if err != nil {
		return err
	}
	if touched {
		return nil
	}

	existing, err := s.loginIPRepo.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	if len(existing) >= domain.UserLoginIPLimit {
		return ErrLoginIPLimitExceeded
	}

	if err := s.loginIPRepo.Create(ctx, userID, clientIP); err != nil {
		// 并发登录可能同时插入同一 IP，唯一索引冲突时视为已记录。
		if touched, touchErr := s.loginIPRepo.Touch(ctx, userID, clientIP); touchErr == nil && touched {
			return nil
		}
		return err
	}
	return nil
}

// ResetLoginIPs 清空用户的登录 IP 记录（管理员操作，用于用户换网络后解封）。
func (s *ActivationCodeService) ResetLoginIPs(ctx context.Context, userID int64) (int, error) {
	return s.loginIPRepo.DeleteByUser(ctx, userID)
}

// ListLoginIPs 返回用户登录过的 IP 记录。
func (s *ActivationCodeService) ListLoginIPs(ctx context.Context, userID int64) ([]UserLoginIP, error) {
	return s.loginIPRepo.ListByUser(ctx, userID)
}

func (s *ActivationCodeService) invalidateBalanceCaches(ctx context.Context, userID int64, amount float64) {
	if amount == 0 {
		return
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingCacheService.InvalidateUserBalance(cacheCtx, userID); err != nil {
				logger.LegacyPrintf("service.activation", "[Activation] invalidate balance cache failed: %v", err)
			}
		}()
	}
}
