package dto

import (
	"time"

	"github.com/uzapi/internal/service"
)

// ActivationCode 是用户可见的激活码字段（不含管理员备注）。
type ActivationCode struct {
	ID        int64      `json:"id"`
	Code      string     `json:"code"`
	Amount    float64    `json:"amount"`
	Status    string     `json:"status"`
	StartsAt  *time.Time `json:"starts_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	UsedBy    *int64     `json:"used_by"`
	UsedAt    *time.Time `json:"used_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// AdminActivationCode 是管理员接口使用的激活码 DTO（含备注）。
type AdminActivationCode struct {
	ActivationCode

	Notes string `json:"notes"`
}

// UserLoginIP 用户登录 IP 记录。
type UserLoginIP struct {
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// ActivationStatus 是「校验激活码有效性」接口的响应体。
type ActivationStatus struct {
	Bound        bool            `json:"bound"`
	Valid        bool            `json:"valid"`
	Reason       string          `json:"reason,omitempty"`
	Code         *ActivationCode `json:"code"`
	LoginIPs     []UserLoginIP   `json:"login_ips"`
	LoginIPUsed  int             `json:"login_ip_used"`
	LoginIPLimit int             `json:"login_ip_limit"`
}

func ActivationCodeFromService(ac *service.ActivationCode) *ActivationCode {
	if ac == nil {
		return nil
	}
	out := activationCodeFromServiceBase(ac)
	return &out
}

func ActivationCodeFromServiceAdmin(ac *service.ActivationCode) *AdminActivationCode {
	if ac == nil {
		return nil
	}
	return &AdminActivationCode{
		ActivationCode: activationCodeFromServiceBase(ac),
		Notes:          ac.Notes,
	}
}

func activationCodeFromServiceBase(ac *service.ActivationCode) ActivationCode {
	return ActivationCode{
		ID:        ac.ID,
		Code:      ac.Code,
		Amount:    ac.Amount,
		Status:    ac.Status,
		StartsAt:  ac.StartsAt,
		ExpiresAt: ac.ExpiresAt,
		UsedBy:    ac.UsedBy,
		UsedAt:    ac.UsedAt,
		CreatedAt: ac.CreatedAt,
		UpdatedAt: ac.UpdatedAt,
	}
}

func UserLoginIPsFromService(ips []service.UserLoginIP) []UserLoginIP {
	out := make([]UserLoginIP, 0, len(ips))
	for i := range ips {
		out = append(out, UserLoginIP{
			IP:         ips[i].IP,
			CreatedAt:  ips[i].CreatedAt,
			LastSeenAt: ips[i].LastSeenAt,
		})
	}
	return out
}

func ActivationStatusFromService(status *service.ActivationStatus) *ActivationStatus {
	if status == nil {
		return nil
	}
	ips := UserLoginIPsFromService(status.LoginIPs)
	return &ActivationStatus{
		Bound:        status.Bound,
		Valid:        status.Valid,
		Reason:       status.Reason,
		Code:         ActivationCodeFromService(status.Code),
		LoginIPs:     ips,
		LoginIPUsed:  len(ips),
		LoginIPLimit: status.LoginIPLimit,
	}
}
