package service

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	infraerrors "github.com/uzapi/internal/pkg/errors"
)

type RedeemCode struct {
	ID        int64
	Code      string
	Type      string
	Value     float64
	Status    string
	UsedBy    *int64
	UsedAt    *time.Time
	Notes     string
	CreatedAt time.Time
	ExpiresAt *time.Time

	GroupID      *int64
	ValidityDays int

	User  *User
	Group *Group
}

func (r *RedeemCode) IsUsed() bool {
	return r.Status == StatusUsed
}

func (r *RedeemCode) IsExpired() bool {
	return r.IsExpiredAt(time.Now())
}

func (r *RedeemCode) IsExpiredAt(now time.Time) bool {
	if r == nil {
		return false
	}
	if r.Status == StatusExpired {
		return true
	}
	return r.Status == StatusUnused && r.ExpiresAt != nil && !r.ExpiresAt.After(now)
}

func (r *RedeemCode) CanUse() bool {
	return r.Status == StatusUnused && !r.IsExpired()
}

func GenerateRedeemCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// A local access code is a permanent entitlement, without monetary or subscription value.
func validateLocalModelAccessCode(codeType string, value float64, groupID *int64, validityDays int) error {
	if codeType == RedeemTypeLocalModelAccess && (value != 0 || groupID != nil || validityDays != 0) {
		return infraerrors.BadRequest("LOCAL_MODEL_ACCESS_CODE_INVALID", "local model access codes require value=0, no group_id and validity_days=0")
	}
	return nil
}
