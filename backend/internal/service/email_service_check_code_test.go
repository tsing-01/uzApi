//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// checkCodeCacheFake is a stateful verification-code store so tests can assert
// whether a check consumed the code or advanced the attempt counter.
type checkCodeCacheFake struct {
	emailCacheStub
	stored  *VerificationCodeData
	deleted bool
}

func (f *checkCodeCacheFake) GetVerificationCode(ctx context.Context, email string) (*VerificationCodeData, error) {
	if f.deleted || f.stored == nil {
		return nil, nil
	}
	data := *f.stored
	return &data, nil
}

func (f *checkCodeCacheFake) SetVerificationCode(ctx context.Context, email string, data *VerificationCodeData, ttl time.Duration) error {
	f.stored = data
	return nil
}

func (f *checkCodeCacheFake) DeleteVerificationCode(ctx context.Context, email string) error {
	f.deleted = true
	return nil
}

func newCheckCodeFixture(code string, attempts int) (*EmailService, *checkCodeCacheFake) {
	cache := &checkCodeCacheFake{
		stored: &VerificationCodeData{
			Code:      code,
			Attempts:  attempts,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
	}
	return &EmailService{cache: cache}, cache
}

func TestEmailService_CheckVerifyCode_DoesNotConsume(t *testing.T) {
	svc, cache := newCheckCodeFixture("123456", 0)
	ctx := context.Background()

	require.NoError(t, svc.CheckVerifyCode(ctx, "user@example.com", "123456"))
	require.False(t, cache.deleted, "pre-check must not consume the code")

	// The register flow can still consume the same code afterwards.
	require.NoError(t, svc.VerifyCode(ctx, "user@example.com", "123456"))
	require.True(t, cache.deleted, "register-time verification must consume the code")

	// And once consumed it is gone.
	require.ErrorIs(t, svc.VerifyCode(ctx, "user@example.com", "123456"), ErrInvalidVerifyCode)
}

func TestEmailService_CheckVerifyCode_WrongCodeCountsAttempt(t *testing.T) {
	svc, cache := newCheckCodeFixture("123456", 0)

	err := svc.CheckVerifyCode(context.Background(), "user@example.com", "000000")
	require.ErrorIs(t, err, ErrInvalidVerifyCode)
	require.Equal(t, 1, cache.stored.Attempts, "failed pre-check must count toward brute-force limit")
	require.False(t, cache.deleted)
}

func TestEmailService_CheckVerifyCode_MaxAttempts(t *testing.T) {
	svc, _ := newCheckCodeFixture("123456", maxVerifyCodeAttempts)

	err := svc.CheckVerifyCode(context.Background(), "user@example.com", "123456")
	require.ErrorIs(t, err, ErrVerifyCodeMaxAttempts)
}
