//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbent "github.com/uzapi/ent"
	"github.com/uzapi/ent/redeemcode"
	"github.com/uzapi/ent/schema/mixins"
	"github.com/uzapi/ent/user"
	"github.com/uzapi/internal/service"
)

// Use real PostgreSQL transactions and no Redis so correctness cannot depend on a cache lock.
func TestLocalModelAccessRedemption(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewRedeemCodeRepository(client)
	users := NewUserRepository(client, integrationDB)
	svc := service.NewRedeemService(repo, users, nil, nil, nil, client, nil, nil)
	var createdUsers, createdCodes []int64
	t.Cleanup(func() {
		_, err := client.RedeemCode.Delete().Where(redeemcode.IDIn(createdCodes...)).Exec(ctx)
		require.NoError(t, err)
		_, err = client.User.Delete().Where(user.IDIn(createdUsers...)).Exec(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err)
	})
	newUser := func() *dbent.User {
		suffix, err := service.GenerateRedeemCode()
		require.NoError(t, err)
		u, err := client.User.Create().SetEmail(suffix + "@local-access.test").SetPasswordHash("test-hash").SetBalance(12).SetConcurrency(3).Save(ctx)
		require.NoError(t, err)
		createdUsers = append(createdUsers, u.ID)
		return u
	}
	newCode := func() *service.RedeemCode {
		code, err := service.GenerateRedeemCode()
		require.NoError(t, err)
		c := &service.RedeemCode{Code: code, Type: service.RedeemTypeLocalModelAccess, Status: service.StatusUnused}
		require.NoError(t, svc.CreateCode(ctx, c))
		createdCodes = append(createdCodes, c.ID)
		return c
	}
	assertUnused := func(c *service.RedeemCode) {
		stored, err := repo.GetByID(ctx, c.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusUnused, stored.Status)
		require.Nil(t, stored.UsedBy)
		require.Nil(t, stored.UsedAt)
	}
	t.Run("grant persists and profile updates cannot grant or erase it", func(t *testing.T) {
		u, other, c := newUser(), newUser(), newCode()
		stale, err := users.GetByID(ctx, u.ID)
		require.NoError(t, err)
		result, err := svc.Redeem(ctx, u.ID, c.Code)
		require.NoError(t, err)
		require.Equal(t, u.ID, *result.UsedBy)
		require.Equal(t, service.StatusUsed, result.Status)
		granted, err := users.GetByID(ctx, u.ID)
		require.NoError(t, err)
		require.NotNil(t, granted.LocalModelAccessUnlockedAt)
		require.Equal(t, float64(12), granted.Balance)
		require.Equal(t, 3, granted.Concurrency)
		require.Zero(t, granted.TotalRecharged)
		require.NoError(t, users.Update(ctx, stale))
		stored, err := users.GetByID(ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, granted.LocalModelAccessUnlockedAt, stored.LocalModelAccessUnlockedAt)
		forged, err := users.GetByID(ctx, other.ID)
		require.NoError(t, err)
		forged.LocalModelAccessUnlockedAt = granted.LocalModelAccessUnlockedAt
		require.NoError(t, users.Update(ctx, forged))
		unchanged, err := users.GetByID(ctx, other.ID)
		require.NoError(t, err)
		require.Nil(t, unchanged.LocalModelAccessUnlockedAt)
		_, err = svc.Redeem(ctx, other.ID, c.Code)
		require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
		spare := newCode()
		_, err = svc.Redeem(ctx, u.ID, spare.Code)
		require.ErrorIs(t, err, service.ErrLocalModelAccessAlreadyUnlocked)
		assertUnused(spare)
	})
	t.Run("same code concurrent users has exactly one winner", func(t *testing.T) {
		c := newCode()
		participants := []*dbent.User{newUser(), newUser(), newUser(), newUser()}
		start := make(chan struct{})
		results := make(chan error, len(participants))
		for _, u := range participants {
			go func(id int64) { <-start; _, err := svc.Redeem(ctx, id, c.Code); results <- err }(u.ID)
		}
		close(start)
		successes := 0
		for range participants {
			err := <-results
			if err == nil {
				successes++
			} else {
				require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
			}
		}
		require.Equal(t, 1, successes)
		unlocked := 0
		for _, u := range participants {
			stored, err := users.GetByID(ctx, u.ID)
			require.NoError(t, err)
			if stored.LocalModelAccessUnlockedAt != nil {
				unlocked++
			}
		}
		require.Equal(t, 1, unlocked)
	})
	t.Run("different codes concurrent same user only consume one", func(t *testing.T) {
		u := newUser()
		codes := []*service.RedeemCode{newCode(), newCode(), newCode(), newCode()}
		// Barrier after reads forces each request to enter with an initially locked account.
		var readBarrier sync.WaitGroup
		readBarrier.Add(len(codes))
		barrierUsers := &localAccessBarrierUserRepo{UserRepository: users, barrier: &readBarrier}
		concurrentSvc := service.NewRedeemService(repo, barrierUsers, nil, nil, nil, client, nil, nil)
		results := make(chan error, len(codes))
		for _, c := range codes {
			go func(code string) { _, err := concurrentSvc.Redeem(ctx, u.ID, code); results <- err }(c.Code)
		}
		successes := 0
		for range codes {
			err := <-results
			if err == nil {
				successes++
			} else {
				require.ErrorIs(t, err, service.ErrLocalModelAccessAlreadyUnlocked)
			}
		}
		require.Equal(t, 1, successes)
		used := 0
		for _, c := range codes {
			stored, err := repo.GetByID(ctx, c.ID)
			require.NoError(t, err)
			if stored.IsUsed() {
				used++
			} else {
				assertUnused(c)
			}
		}
		require.Equal(t, 1, used)
	})
	t.Run("expired disabled malformed and inactive cannot grant", func(t *testing.T) {
		for _, scenario := range []string{"expired", "disabled", "malformed", "inactive"} {
			t.Run(scenario, func(t *testing.T) {
				u, c := newUser(), newCode()
				switch scenario {
				case "expired":
					require.NoError(t, client.RedeemCode.UpdateOneID(c.ID).SetExpiresAt(time.Now().Add(-time.Second)).Exec(ctx))
				case "disabled":
					require.NoError(t, client.RedeemCode.UpdateOneID(c.ID).SetStatus(service.StatusDisabled).Exec(ctx))
				case "malformed":
					require.NoError(t, client.RedeemCode.UpdateOneID(c.ID).SetValue(100).Exec(ctx))
				case "inactive":
					require.NoError(t, client.User.UpdateOneID(u.ID).SetStatus(service.StatusDisabled).Exec(ctx))
				}
				_, err := svc.Redeem(ctx, u.ID, c.Code)
				require.Error(t, err)
				stored, err := users.GetByID(ctx, u.ID)
				require.NoError(t, err)
				require.Nil(t, stored.LocalModelAccessUnlockedAt)
				consumed, err := client.RedeemCode.Query().Where(redeemcode.IDEQ(c.ID), redeemcode.UsedByNotNil()).Exist(ctx)
				require.NoError(t, err)
				require.False(t, consumed)
			})
		}
	})
	t.Run("database failure rolls back code use", func(t *testing.T) {
		u, c := newUser(), newCode()
		// A PostgreSQL trigger rejects this one test user's entitlement grant.
		_, err := integrationDB.ExecContext(ctx, `CREATE FUNCTION test_reject_local_access() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.email LIKE '%@local-access-failure.test' AND NEW.local_model_access_unlocked_at IS NOT NULL THEN RAISE EXCEPTION 'test grant failure'; END IF; RETURN NEW; END; $$`)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER test_reject_local_access BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION test_reject_local_access()`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = integrationDB.ExecContext(ctx, `DROP TRIGGER IF EXISTS test_reject_local_access ON users; DROP FUNCTION IF EXISTS test_reject_local_access()`)
		})
		require.NoError(t, client.User.Update().Where(user.IDEQ(u.ID)).SetEmail(c.Code+"@local-access-failure.test").Exec(ctx))
		_, err = svc.Redeem(ctx, u.ID, c.Code)
		require.ErrorContains(t, err, "test grant failure")
		assertUnused(c)
		stored, err := users.GetByID(ctx, u.ID)
		require.NoError(t, err)
		require.Nil(t, stored.LocalModelAccessUnlockedAt)
	})
	t.Run("database consumption rechecks expiry", func(t *testing.T) {
		u, c := newUser(), newCode()
		require.NoError(t, client.RedeemCode.UpdateOneID(c.ID).SetExpiresAt(time.Now().Add(-time.Second)).Exec(ctx))
		require.True(t, errors.Is(repo.Use(ctx, c.ID, u.ID), service.ErrRedeemCodeUsed))
		assertUnused(c)
	})
}

type localAccessBarrierUserRepo struct {
	service.UserRepository
	barrier *sync.WaitGroup
}

func (r *localAccessBarrierUserRepo) GetByID(ctx context.Context, id int64) (*service.User, error) {
	u, err := r.UserRepository.GetByID(ctx, id)
	r.barrier.Done()
	r.barrier.Wait()
	return u, err
}
