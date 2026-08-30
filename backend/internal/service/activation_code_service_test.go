//go:build unit

package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	dbent "github.com/uzapi/ent"
	"github.com/uzapi/ent/enttest"
	"github.com/uzapi/internal/domain"
	"github.com/uzapi/internal/repository"
	"github.com/uzapi/internal/service"

	_ "modernc.org/sqlite"
)

func newActivationTestEnv(t *testing.T) (*service.ActivationCodeService, *dbent.Client, service.ActivationCodeRepository) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:activation_"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	activationRepo := repository.NewActivationCodeRepository(client)
	loginIPRepo := repository.NewUserLoginIPRepository(client)
	userRepo := repository.NewUserRepository(client, db)

	svc := service.NewActivationCodeService(activationRepo, loginIPRepo, userRepo, nil, client, nil)
	return svc, client, activationRepo
}

func createActivationTestUser(t *testing.T, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	u, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		SetBalance(0).
		Save(context.Background())
	require.NoError(t, err)
	return u
}

func createActivationCode(t *testing.T, client *dbent.Client, code string, amount float64, startsAt, expiresAt *time.Time) *dbent.ActivationCode {
	t.Helper()
	builder := client.ActivationCode.Create().
		SetCode(code).
		SetAmount(amount).
		SetStatus(domain.ActivationCodeStatusUnused)
	if startsAt != nil {
		builder.SetStartsAt(*startsAt)
	}
	if expiresAt != nil {
		builder.SetExpiresAt(*expiresAt)
	}
	created, err := builder.Save(context.Background())
	require.NoError(t, err)
	return created
}

func TestActivationBindCreditsBalanceAndReplacesPrevious(t *testing.T) {
	svc, client, repo := newActivationTestEnv(t)
	ctx := context.Background()
	user := createActivationTestUser(t, client, "bind@example.com")

	createActivationCode(t, client, "FIRST", 10, nil, nil)
	createActivationCode(t, client, "SECOND", 25, nil, nil)

	first, err := svc.Bind(ctx, user.ID, "first")
	require.NoError(t, err)
	require.Equal(t, 10.0, first.NewBalance)
	require.Equal(t, 0, first.ReplacedCount)

	// 第一张码绑定后占用一个登录 IP 名额
	require.NoError(t, svc.EnsureLoginAllowed(ctx, &service.User{ID: user.ID, Role: domain.RoleUser}, "1.1.1.1"))

	second, err := svc.Bind(ctx, user.ID, "SECOND")
	require.NoError(t, err)
	require.Equal(t, 35.0, second.NewBalance)
	require.Equal(t, 1, second.ReplacedCount, "旧激活码应被替换")

	current, err := repo.GetCurrentByUser(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "SECOND", current.Code)

	old, err := repo.GetByCode(ctx, "FIRST")
	require.NoError(t, err)
	require.Equal(t, domain.ActivationCodeStatusReplaced, old.Status)

	// 换码会清空登录 IP 名额
	ips, err := svc.ListLoginIPs(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, ips)
}

func TestActivationBindRejectsUnusableCodes(t *testing.T) {
	svc, client, _ := newActivationTestEnv(t)
	ctx := context.Background()
	user := createActivationTestUser(t, client, "reject@example.com")
	other := createActivationTestUser(t, client, "other@example.com")

	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)
	createActivationCode(t, client, "NOTSTARTED", 5, &future, nil)
	createActivationCode(t, client, "EXPIRED", 5, nil, &past)
	createActivationCode(t, client, "TAKEN", 5, nil, nil)

	_, err := svc.Bind(ctx, user.ID, "NOTSTARTED")
	require.ErrorIs(t, err, service.ErrActivationCodeNotStarted)

	_, err = svc.Bind(ctx, user.ID, "EXPIRED")
	require.ErrorIs(t, err, service.ErrActivationCodeExpired)

	_, err = svc.Bind(ctx, other.ID, "TAKEN")
	require.NoError(t, err)
	_, err = svc.Bind(ctx, user.ID, "TAKEN")
	require.ErrorIs(t, err, service.ErrActivationCodeUsed)

	_, err = svc.Bind(ctx, user.ID, "NOPE")
	require.ErrorIs(t, err, service.ErrActivationCodeNotFound)
}

func TestActivationEnsureLoginAllowedIPLimit(t *testing.T) {
	svc, client, _ := newActivationTestEnv(t)
	ctx := context.Background()
	user := createActivationTestUser(t, client, "iplimit@example.com")
	createActivationCode(t, client, "IPLIMIT", 1, nil, nil)
	_, err := svc.Bind(ctx, user.ID, "IPLIMIT")
	require.NoError(t, err)

	subject := &service.User{ID: user.ID, Role: domain.RoleUser}
	require.NoError(t, svc.EnsureLoginAllowed(ctx, subject, "1.1.1.1"))
	require.NoError(t, svc.EnsureLoginAllowed(ctx, subject, "2.2.2.2"))
	// 已记录的 IP 可以反复登录
	require.NoError(t, svc.EnsureLoginAllowed(ctx, subject, "1.1.1.1"))
	// 第三个不同 IP 被拦截
	require.ErrorIs(t, svc.EnsureLoginAllowed(ctx, subject, "3.3.3.3"), service.ErrLoginIPLimitExceeded)

	ips, err := svc.ListLoginIPs(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, ips, domain.UserLoginIPLimit)

	// 管理员清空后可以换新 IP 登录
	removed, err := svc.ResetLoginIPs(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.UserLoginIPLimit, removed)
	require.NoError(t, svc.EnsureLoginAllowed(ctx, subject, "3.3.3.3"))
}

func TestActivationEnsureLoginAllowedCodeValidity(t *testing.T) {
	svc, client, _ := newActivationTestEnv(t)
	ctx := context.Background()

	// 从未绑定激活码的用户不受影响
	plain := createActivationTestUser(t, client, "plain@example.com")
	require.NoError(t, svc.EnsureLoginAllowed(ctx, &service.User{ID: plain.ID, Role: domain.RoleUser}, "9.9.9.9"))

	// 绑定后激活码过期 → 登录被拦截
	user := createActivationTestUser(t, client, "expiring@example.com")
	soon := time.Now().Add(time.Hour)
	code := createActivationCode(t, client, "EXPIRING", 3, nil, &soon)
	_, err := svc.Bind(ctx, user.ID, "EXPIRING")
	require.NoError(t, err)

	subject := &service.User{ID: user.ID, Role: domain.RoleUser}
	require.NoError(t, svc.EnsureLoginAllowed(ctx, subject, "1.1.1.1"))

	past := time.Now().Add(-time.Minute)
	_, err = client.ActivationCode.UpdateOneID(code.ID).SetExpiresAt(past).Save(ctx)
	require.NoError(t, err)

	err = svc.EnsureLoginAllowed(ctx, subject, "1.1.1.1")
	require.Error(t, err)
	require.Contains(t, err.Error(), service.ActivationReasonExpired)

	// 管理员豁免
	require.NoError(t, svc.EnsureLoginAllowed(ctx, &service.User{ID: user.ID, Role: domain.RoleAdmin}, "8.8.8.8"))
}

func TestActivationGetStatus(t *testing.T) {
	svc, client, _ := newActivationTestEnv(t)
	ctx := context.Background()

	user := createActivationTestUser(t, client, "status@example.com")
	status, err := svc.GetStatus(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, status.Bound)
	require.False(t, status.Valid)
	require.Equal(t, service.ActivationReasonNotBound, status.Reason)
	require.Equal(t, domain.UserLoginIPLimit, status.LoginIPLimit)

	createActivationCode(t, client, "STATUS", 7, nil, nil)
	_, err = svc.Bind(ctx, user.ID, "STATUS")
	require.NoError(t, err)
	require.NoError(t, svc.EnsureLoginAllowed(ctx, &service.User{ID: user.ID, Role: domain.RoleUser}, "1.1.1.1"))

	status, err = svc.GetStatus(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, status.Bound)
	require.True(t, status.Valid)
	require.Empty(t, status.Reason)
	require.Equal(t, "STATUS", status.Code.Code)
	require.Len(t, status.LoginIPs, 1)
}
