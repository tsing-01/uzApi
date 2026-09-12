//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/service"
)

// Real PostgreSQL transactions; only the clock/fault is replaceable in tests.
type quotaTestRepo struct {
	service.LocalModelQuotaRepository
	clock      func() time.Time
	failSave   bool
	loseCommit bool
}
type quotaTestStore struct {
	service.LocalModelQuotaStore
	repo *quotaTestRepo
}

func (r *quotaTestRepo) WithUser(ctx context.Context, id int64, f func(*service.User, service.LocalModelQuotaStore) error) error {
	err := r.LocalModelQuotaRepository.WithUser(ctx, id, func(u *service.User, s service.LocalModelQuotaStore) error { return f(u, quotaTestStore{s, r}) })
	if err == nil && r.loseCommit {
		return errors.New("simulated lost COMMIT acknowledgement")
	}
	return err
}
func (s quotaTestStore) Now(ctx context.Context) (time.Time, error) {
	if s.repo.clock != nil {
		return s.repo.clock(), nil
	}
	return s.LocalModelQuotaStore.Now(ctx)
}
func (s quotaTestStore) SaveDecision(ctx context.Context, date string, d *service.LocalModelRequestDecision) error {
	if err := s.LocalModelQuotaStore.SaveDecision(ctx, date, d); err != nil {
		return err
	}
	if s.repo.failSave {
		return errors.New("simulated failure after count and decision writes")
	}
	return nil
}
func newQuotaTest(t *testing.T) (*licenseTestEnv, *quotaTestRepo, *service.LocalModelQuotaService) {
	t.Helper()
	e := newLicenseTestEnv(t)
	_, err := integrationDB.ExecContext(e.ctx, `UPDATE users SET local_model_access_unlocked_at=NULL WHERE id=$1`, e.userID)
	require.NoError(t, err)
	r := &quotaTestRepo{LocalModelQuotaRepository: NewLocalModelQuotaRepository(integrationDB)}
	s, err := service.NewLocalModelQuotaService(r)
	require.NoError(t, err)
	return e, r, s
}

func TestCustomAPIQuotaConcurrent(t *testing.T) {
	previousMax := integrationDB.Stats().MaxOpenConnections
	integrationDB.SetMaxOpenConns(16)
	t.Cleanup(func() { integrationDB.SetMaxOpenConns(previousMax) })
	for _, sameID := range []bool{false, true} {
		t.Run(fmt.Sprintf("same_id=%t", sameID), func(t *testing.T) {
			e, _, first := newQuotaTest(t)
			second, err := service.NewLocalModelQuotaService(NewLocalModelQuotaRepository(integrationDB))
			require.NoError(t, err)
			instances := []*service.LocalModelQuotaService{first, second}
			id := uuid.NewString()
			type result struct {
				out *service.LocalModelRequestAuthorization
				err error
			}
			results := make(chan result, 100)
			start := make(chan struct{})
			for i := range 100 {
				go func(i int) {
					requestID := id
					if !sameID {
						requestID = uuid.NewString()
					}
					<-start
					o, err := instances[i%2].Authorize(e.ctx, e.userID, requestID)
					results <- result{o, err}
				}(i)
			}
			close(start)
			approved, originals := 0, 0
			var original *service.LocalModelRequestAuthorization
			for range 100 {
				r := <-results
				if r.err != nil {
					require.ErrorIs(t, r.err, service.ErrCustomAPIDailyLimit)
					require.False(t, r.out.Allowed)
					continue
				}
				approved++
				require.True(t, r.out.Allowed)
				if !r.out.Replayed {
					originals++
				}
				if sameID {
					if original == nil {
						original = r.out
					}
					require.Equal(t, original.AuthorizedAt, r.out.AuthorizedAt)
					require.Equal(t, original.ExpiresAt, r.out.ExpiresAt)
					require.Equal(t, original.EntitlementVersion, r.out.EntitlementVersion)
				}
			}
			wantUsed := int64(5)
			if sameID {
				require.Equal(t, 100, approved)
				require.Equal(t, 1, originals)
				wantUsed = 1
			} else {
				require.Equal(t, 5, approved)
			}
			status, err := second.Status(e.ctx, e.userID)
			require.NoError(t, err)
			require.Equal(t, wantUsed, status.Quota.Used)
			var decisions int
			require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT count(*) FROM local_model_request_decisions WHERE user_id=$1`, e.userID).Scan(&decisions))
			if sameID {
				require.Equal(t, 1, decisions)
			} else {
				require.Equal(t, 100, decisions)
			}
		})
	}
}

func TestCustomAPIQuotaLifecycle(t *testing.T) {
	e, r, s := newQuotaTest(t)
	now := time.Date(2026, 9, 12, 15, 59, 50, 0, time.UTC)
	r.clock = func() time.Time { return now }
	status, err := s.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.Equal(t, int64(5), *status.Quota.Remaining)
	require.Equal(t, "Asia/Shanghai", status.Quota.Timezone)
	require.False(t, status.Enabled)
	ids := make([]string, 6)
	var first *service.LocalModelRequestAuthorization
	for i := range ids {
		ids[i] = uuid.NewString()
		out, err := s.Authorize(e.ctx, e.userID, ids[i])
		if i == 5 {
			require.ErrorIs(t, err, service.ErrCustomAPIDailyLimit)
			require.False(t, out.Allowed)
		} else {
			require.NoError(t, err)
			require.Equal(t, int64(i+1), out.Quota.Used)
		}
		if i == 0 {
			first = out
		}
	}
	require.Equal(t, time.Date(2026, 9, 12, 16, 0, 0, 0, time.UTC), *first.ExpiresAt)
	// Grant changes the version but never erases the trial ledger.
	require.NoError(t, e.svc.GrantEntitlement(e.ctx, e.userID, e.userID, "support purchase"))
	status, err = s.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.Nil(t, status.Quota.Remaining)
	require.Equal(t, int64(5), status.Quota.Used)
	_, err = s.Authorize(e.ctx, e.userID, ids[5])
	require.ErrorIs(t, err, service.ErrCustomAPIDailyLimit)
	_, err = s.Authorize(e.ctx, e.userID, ids[0])
	require.ErrorIs(t, err, service.ErrCustomAPIAuthorizationRevoked)
	paidID := uuid.NewString()
	paid, err := s.Authorize(e.ctx, e.userID, paidID)
	require.NoError(t, err)
	require.Equal(t, int64(6), paid.Quota.Used)
	require.Nil(t, paid.Quota.Remaining)
	require.NoError(t, e.svc.RevokeEntitlement(e.ctx, e.userID, e.userID, "refund"))
	_, err = s.Authorize(e.ctx, e.userID, paidID)
	require.ErrorIs(t, err, service.ErrCustomAPIAuthorizationRevoked)
	status, err = s.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.Equal(t, int64(6), status.Quota.Used)
	require.Zero(t, *status.Quota.Remaining)
	require.Equal(t, paid.EntitlementVersion+1, status.EntitlementVersion)
	_, err = s.Authorize(e.ctx, e.userID, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCustomAPIDailyLimit)
	// Midnight naturally chooses a new row. Old denied IDs cannot become approvals.
	now = now.Add(10 * time.Second)
	_, err = s.Authorize(e.ctx, e.userID, ids[5])
	require.ErrorIs(t, err, service.ErrCustomAPIDailyLimit)
	newDay, err := s.Authorize(e.ctx, e.userID, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, "2026-09-13", newDay.Quota.Date)
	require.Equal(t, int64(1), newDay.Quota.Used)
	now = now.Add(30 * time.Second)
	expired, err := s.Authorize(e.ctx, e.userID, newDay.RequestID)
	require.ErrorIs(t, err, service.ErrCustomAPIAuthorizationExpired)
	require.False(t, expired.Allowed)
	require.Equal(t, newDay.ExpiresAt, expired.ExpiresAt)
	// Fresh service instance and an empty Redis still see durable usage.
	require.NoError(t, integrationRedis.FlushDB(e.ctx).Err()) // Only the disposable test Redis.
	newService, err := service.NewLocalModelQuotaService(r)
	require.NoError(t, err)
	status, err = newService.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Quota.Used)
	other, otherRepo, otherService := newQuotaTest(t)
	otherRepo.clock = r.clock
	independent, err := otherService.Authorize(e.ctx, other.userID, newDay.RequestID)
	require.NoError(t, err)
	require.False(t, independent.Replayed)
	require.Equal(t, int64(1), independent.Quota.Used)
	// Audits contain previous/new state, version, actor and reason.
	var audits int
	require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT count(*) FROM local_model_license_events WHERE user_id=$1 AND actor_id=$1 AND previous_enabled<>new_enabled AND new_version=previous_version+1 AND reason<>''`, e.userID).Scan(&audits))
	require.Equal(t, 2, audits)
}

func TestCustomAPIQuotaFaultsAndRetention(t *testing.T) {
	e, r, s := newQuotaTest(t)
	now := time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC)
	r.clock = func() time.Time { return now }
	id := uuid.NewString()
	r.failSave = true
	out, err := s.Authorize(e.ctx, e.userID, id)
	require.ErrorIs(t, err, service.ErrCustomAPIQuotaUnavailable)
	require.Nil(t, out)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT count(*) FROM local_model_request_decisions WHERE user_id=$1`, e.userID).Scan(&count))
	require.Zero(t, count)
	r.failSave = false
	r.loseCommit = true
	out, err = s.Authorize(e.ctx, e.userID, id)
	require.ErrorIs(t, err, service.ErrCustomAPIQuotaUnavailable)
	require.Nil(t, out)
	r.loseCommit = false
	out, err = s.Authorize(e.ctx, e.userID, id)
	require.NoError(t, err)
	require.True(t, out.Replayed)
	require.Equal(t, int64(1), out.Quota.Used)
	// An account disabled after the original approval cannot replay it.
	_, err = integrationDB.ExecContext(e.ctx, `UPDATE users SET status='disabled' WHERE id=$1`, e.userID)
	require.NoError(t, err)
	out, err = s.Authorize(e.ctx, e.userID, id)
	require.ErrorIs(t, err, service.ErrCustomAPIAccountDisabled)
	require.Nil(t, out)
	_, err = integrationDB.ExecContext(e.ctx, `UPDATE users SET status='active' WHERE id=$1`, e.userID)
	require.NoError(t, err)
	now = now.Add(7 * 24 * time.Hour)
	_, err = s.Authorize(e.ctx, e.userID, uuid.NewString())
	require.NoError(t, err)
	_, err = s.Authorize(e.ctx, e.userID, id)
	require.ErrorIs(t, err, service.ErrCustomAPIAuthorizationExpired)
	now = now.Add(time.Second)
	_, err = s.Authorize(e.ctx, e.userID, uuid.NewString()) // Bounded cleanup beyond 7 days.
	require.NoError(t, err)
	out, err = s.Authorize(e.ctx, e.userID, id)
	require.NoError(t, err)
	require.False(t, out.Replayed)
	require.Equal(t, int64(3), out.Quota.Used) // Missing records never mean free authorization.
	closed, err := sql.Open("postgres", "postgres://invalid:invalid@127.0.0.1:1/unused?sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	unavailable, err := service.NewLocalModelQuotaService(NewLocalModelQuotaRepository(closed))
	require.NoError(t, err)
	out, err = unavailable.Authorize(e.ctx, e.userID, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCustomAPIQuotaUnavailable)
	require.Nil(t, out)
	status, err := unavailable.Status(e.ctx, e.userID)
	require.ErrorIs(t, err, service.ErrCustomAPIQuotaUnavailable)
	require.Nil(t, status)
}

func TestCustomAPIQuotaLockWaitCrossesMidnightAndRevocation(t *testing.T) {
	e, r, s := newQuotaTest(t)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 9, 12, 15, 59, 59, 0, time.UTC).Unix())
	r.clock = func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	tx, err := integrationDB.BeginTx(e.ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(e.ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, e.userID)
	require.NoError(t, err)
	type result struct {
		out *service.LocalModelRequestAuthorization
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := s.Authorize(e.ctx, e.userID, uuid.NewString()); done <- result{out, err} }()
	// Observe PostgreSQL's actual lock wait, rather than depending on a sleep.
	require.Eventually(t, func() bool {
		var n int
		err := integrationDB.QueryRowContext(e.ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%local_model_access_version FROM users%'`).Scan(&n)
		return err == nil && n > 0
	}, 5*time.Second, 10*time.Millisecond)
	clock.Add(2)
	_, err = tx.ExecContext(e.ctx, `UPDATE users SET local_model_access_revoked_at=clock_timestamp(),local_model_access_version=local_model_access_version+1 WHERE id=$1`, e.userID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	resultAfter := <-done
	require.NoError(t, resultAfter.err)
	require.Equal(t, "2026-09-13", resultAfter.out.Quota.Date)
	require.False(t, resultAfter.out.Enabled)
	require.Equal(t, int64(2), resultAfter.out.EntitlementVersion)
	var yesterday int
	require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT count(*) FROM local_model_daily_usage WHERE user_id=$1 AND quota_date='2026-09-12'`, e.userID).Scan(&yesterday))
	require.Zero(t, yesterday)
}

func TestCustomAPIQuotaRateLimit(t *testing.T) {
	e, r, s := newQuotaTest(t)
	now := time.Date(2026, 9, 12, 4, 0, 10, 0, time.UTC)
	r.clock = func() time.Time { return now }
	require.NoError(t, e.svc.GrantEntitlement(e.ctx, e.userID, e.userID, "paid"))
	for range service.CustomAPIRateLimit {
		_, err := s.Authorize(e.ctx, e.userID, uuid.NewString())
		require.NoError(t, err)
	}
	id := uuid.NewString()
	out, err := s.Authorize(e.ctx, e.userID, id)
	require.ErrorIs(t, err, service.ErrCustomAPIRateLimited)
	require.Nil(t, out)
	now = now.Add(time.Minute)
	out, err = s.Authorize(e.ctx, e.userID, id)
	require.NoError(t, err)
	require.False(t, out.Replayed)
	require.Equal(t, int64(service.CustomAPIRateLimit+1), out.Quota.Used)
}

func TestCustomAPIEntitlementAuditFailureRollsBack(t *testing.T) {
	e, _, quota := newQuotaTest(t)
	failing, err := service.NewLocalModelLicenseService(licenseFailAuditRepo{e.repo}, licenseIntegrationConfig())
	require.NoError(t, err)
	before, err := quota.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.Error(t, failing.GrantEntitlement(e.ctx, e.userID, e.userID, "purchase"))
	after, err := quota.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.False(t, after.Enabled)
	require.Equal(t, before.EntitlementVersion, after.EntitlementVersion)
	require.NoError(t, e.svc.GrantEntitlement(e.ctx, e.userID, e.userID, "purchase"))
	require.Error(t, failing.RevokeEntitlement(e.ctx, e.userID, e.userID, "refund"))
	after, err = quota.Status(e.ctx, e.userID)
	require.NoError(t, err)
	require.True(t, after.Enabled)
	require.Equal(t, before.EntitlementVersion+1, after.EntitlementVersion)
}
