//go:build integration

package repository

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uzapi/ent/schema/mixins"
	"github.com/uzapi/ent/user"
	"github.com/uzapi/internal/config"
	"github.com/uzapi/internal/service"
)

type licenseTestEnv struct {
	ctx    context.Context
	svc    *service.LocalModelLicenseService
	repo   service.LocalModelLicenseRepository
	userID int64
}

func newLicenseTestEnv(t *testing.T) *licenseTestEnv {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	suffix, err := service.GenerateRedeemCode()
	require.NoError(t, err)
	u, err := client.User.Create().SetEmail(suffix + "@device-license.test").SetPasswordHash("license-test-hash").SetLocalModelAccessUnlockedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := client.User.Delete().Where(user.IDEQ(u.ID)).Exec(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err)
	})
	repo := NewLocalModelLicenseRepository(integrationDB)
	svc, err := service.NewLocalModelLicenseService(repo, licenseIntegrationConfig())
	require.NoError(t, err)
	return &licenseTestEnv{ctx: ctx, svc: svc, repo: repo, userID: u.ID}
}
func licenseIntegrationConfig() *config.Config {
	return &config.Config{LocalModelAccess: config.LocalModelAccessConfig{SigningSeed: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), Issuer: "https://license.example.test", MaxDevices: 2}}
}
func newLicenseDeviceKey(t *testing.T) (*ecdsa.PrivateKey, service.DevicePublicKey) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return k, service.DevicePublicKey{Kty: "EC", Crv: "P-256", X: base64.RawURLEncoding.EncodeToString(k.X.FillBytes(make([]byte, 32))), Y: base64.RawURLEncoding.EncodeToString(k.Y.FillBytes(make([]byte, 32)))}
}
func signLicenseChallenge(t *testing.T, c *service.LocalModelChallengeResponse, k *ecdsa.PrivateKey) service.LocalModelProof {
	t.Helper()
	hash := sha256.Sum256([]byte(c.Message))
	r, s, err := ecdsa.Sign(rand.Reader, k, hash[:])
	require.NoError(t, err)
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return service.LocalModelProof{Challenge: c.Challenge, Signature: base64.RawURLEncoding.EncodeToString(sig)}
}
func (e *licenseTestEnv) register(t *testing.T) (*service.LocalModelDevice, *ecdsa.PrivateKey) {
	t.Helper()
	key, jwk := newLicenseDeviceKey(t)
	c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
	require.NoError(t, err)
	d, err := e.svc.Register(e.ctx, e.userID, "Test computer", signLicenseChallenge(t, c, key))
	require.NoError(t, err)
	return d, key
}
func (e *licenseTestEnv) issueProof(t *testing.T, d *service.LocalModelDevice, key *ecdsa.PrivateKey, previous string) service.LocalModelProof {
	t.Helper()
	action := "issue"
	if previous != "" {
		action = "renew"
	}
	c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: action, DeviceID: d.ID, License: previous})
	require.NoError(t, err)
	return signLicenseChallenge(t, c, key)
}

func TestLocalDeviceLicenseLifecycle(t *testing.T) {
	e := newLicenseTestEnv(t)
	d, key := e.register(t)
	proof := e.issueProof(t, d, key, "")
	lease, err := e.svc.Issue(e.ctx, e.userID, proof, "", false)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), lease.ExpiresAt, 2*time.Second)
	require.Equal(t, d.ID, lease.DeviceID)
	_, err = e.svc.Issue(e.ctx, e.userID, proof, "", false)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	renewed, err := e.svc.Issue(e.ctx, e.userID, e.issueProof(t, d, key, lease.License), lease.License, true)
	require.NoError(t, err)
	require.NotEqual(t, lease.License, renewed.License)
	devices, err := e.svc.Devices(e.ctx, e.userID)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.NotNil(t, devices[0].LastIssuedAt)
	pending := e.issueProof(t, d, key, "")
	require.NoError(t, e.svc.RevokeDevice(e.ctx, e.userID, d.ID))
	require.NoError(t, e.svc.RevokeDevice(e.ctx, e.userID, d.ID))
	_, err = e.svc.Issue(e.ctx, e.userID, pending, "", false)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "renew", DeviceID: d.ID, License: renewed.License})
	require.ErrorIs(t, err, service.ErrLocalDeviceRevoked)
	require.NoError(t, e.svc.RevokeEntitlement(e.ctx, e.userID, e.userID, "refund"))
	profile, err := NewUserRepository(testEntClient(t), integrationDB).GetByID(e.ctx, e.userID)
	require.NoError(t, err)
	require.False(t, profile.HasLocalModelAccess())
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register"})
	require.ErrorIs(t, err, service.ErrLocalAccessDenied)
	// A new paid redemption grants a new version, never resurrecting old device keys.
	codes := NewRedeemCodeRepository(testEntClient(t))
	code, err := service.GenerateRedeemCode()
	require.NoError(t, err)
	rc := &service.RedeemCode{Code: code, Type: service.RedeemTypeLocalModelAccess, Status: service.StatusUnused}
	require.NoError(t, codes.Create(e.ctx, rc))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(e.ctx, `DELETE FROM redeem_codes WHERE id=$1`, rc.ID) })
	redeem := service.NewRedeemService(codes, NewUserRepository(testEntClient(t), integrationDB), nil, nil, nil, testEntClient(t), nil, nil)
	_, err = redeem.Redeem(e.ctx, e.userID, code)
	require.NoError(t, err)
	granted, err := NewUserRepository(testEntClient(t), integrationDB).GetByID(e.ctx, e.userID)
	require.NoError(t, err)
	require.True(t, granted.HasLocalModelAccess())
	require.Greater(t, granted.LocalModelAccessVersion, profile.LocalModelAccessVersion)
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "issue", DeviceID: d.ID})
	require.ErrorIs(t, err, service.ErrLocalDeviceRevoked)
	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT COUNT(*) FROM local_model_license_events WHERE user_id=$1`, e.userID).Scan(&auditCount))
	require.GreaterOrEqual(t, auditCount, 5)
}

func TestLocalDeviceProofIsolationAndExpiry(t *testing.T) {
	e := newLicenseTestEnv(t)
	other := newLicenseTestEnv(t)
	key, jwk := newLicenseDeviceKey(t)
	c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
	require.NoError(t, err)
	proof := signLicenseChallenge(t, c, key)
	_, err = e.svc.Register(e.ctx, other.userID, "Other", proof)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	_, err = e.svc.Issue(e.ctx, e.userID, proof, "", false)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	wrong, _ := newLicenseDeviceKey(t)
	_, err = e.svc.Register(e.ctx, e.userID, "Forged", signLicenseChallenge(t, c, wrong))
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	_, err = e.svc.Register(e.ctx, e.userID, "Valid", proof)
	require.NoError(t, err)
	_, err = e.svc.Register(e.ctx, e.userID, "Replay", proof)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	_, jwk = newLicenseDeviceKey(t)
	expired, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
	require.NoError(t, err)
	hash := sha256.Sum256([]byte(expired.Challenge))
	_, err = integrationDB.ExecContext(e.ctx, `UPDATE local_model_challenges SET expires_at=$2 WHERE token_hash=$1`, hex.EncodeToString(hash[:]), time.Now().Add(-time.Second))
	require.NoError(t, err)
	_, err = e.svc.Register(e.ctx, e.userID, "Expired", signLicenseChallenge(t, expired, key))
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	err = e.svc.RevokeDevice(e.ctx, other.userID, strings.Repeat("a", 32))
	require.ErrorIs(t, err, service.ErrLocalDeviceNotFound)
}

func TestLocalDeviceRegistrationConcurrentLimit(t *testing.T) {
	e := newLicenseTestEnv(t)
	proofs := make([]service.LocalModelProof, 3)
	for i := range proofs {
		key, jwk := newLicenseDeviceKey(t)
		c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
		require.NoError(t, err)
		proofs[i] = signLicenseChallenge(t, c, key)
	}
	start := make(chan struct{})
	results := make(chan error, 3)
	for _, proof := range proofs {
		go func(p service.LocalModelProof) {
			<-start
			_, err := e.svc.Register(e.ctx, e.userID, "Concurrent", p)
			results <- err
		}(proof)
	}
	close(start)
	successes := 0
	for range proofs {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrLocalDeviceLimit)
		}
	}
	require.Equal(t, 2, successes)
	devices, err := e.svc.Devices(e.ctx, e.userID)
	require.NoError(t, err)
	require.Len(t, devices, 2)
}

func TestLocalDeviceConcurrentProofAndRevocation(t *testing.T) {
	e := newLicenseTestEnv(t)
	d, key := e.register(t)
	proof := e.issueProof(t, d, key, "")
	start := make(chan struct{})
	results := make(chan error, 4)
	for range 4 {
		go func() { <-start; _, err := e.svc.Issue(e.ctx, e.userID, proof, "", false); results <- err }()
	}
	close(start)
	successes := 0
	for range 4 {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrLocalProofInvalid)
		}
	}
	require.Equal(t, 1, successes)
	proof = e.issueProof(t, d, key, "")
	var wg sync.WaitGroup
	wg.Add(2)
	var issueErr, revokeErr error
	go func() { defer wg.Done(); _, issueErr = e.svc.Issue(e.ctx, e.userID, proof, "", false) }()
	go func() {
		defer wg.Done()
		revokeErr = e.svc.RevokeEntitlement(e.ctx, e.userID, e.userID, "security revoke")
	}()
	wg.Wait()
	require.NoError(t, revokeErr)
	if issueErr != nil {
		require.True(t, errors.Is(issueErr, service.ErrLocalAccessDenied) || errors.Is(issueErr, service.ErrLocalProofInvalid))
	}
	_, err := e.svc.Issue(e.ctx, e.userID, proof, "", false)
	require.ErrorIs(t, err, service.ErrLocalAccessDenied)
	devices, err := e.svc.Devices(e.ctx, e.userID)
	require.NoError(t, err)
	require.NotNil(t, devices[0].RevokedAt)
	var lateIssues int
	require.NoError(t, integrationDB.QueryRowContext(e.ctx, `SELECT COUNT(*) FROM local_model_license_events e JOIN users u ON e.user_id=u.id WHERE e.user_id=$1 AND event='license_issue' AND e.created_at>u.local_model_access_revoked_at`, e.userID).Scan(&lateIssues))
	require.Zero(t, lateIssues)
}

type licenseFailAuditRepo struct {
	service.LocalModelLicenseRepository
}
type licenseFailAuditStore struct{ service.LocalModelLicenseStore }

func (r licenseFailAuditRepo) WithUser(ctx context.Context, id int64, f func(*service.User, service.LocalModelLicenseStore) error) error {
	return r.LocalModelLicenseRepository.WithUser(ctx, id, func(u *service.User, s service.LocalModelLicenseStore) error { return f(u, licenseFailAuditStore{s}) })
}
func (licenseFailAuditStore) Audit(context.Context, int64, string, string, string, time.Time) error {
	return errors.New("injected audit failure")
}
func TestLocalDeviceRegistrationRollbackAndRateLimit(t *testing.T) {
	e := newLicenseTestEnv(t)
	key, jwk := newLicenseDeviceKey(t)
	c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
	require.NoError(t, err)
	failing, err := service.NewLocalModelLicenseService(licenseFailAuditRepo{e.repo}, licenseIntegrationConfig())
	require.NoError(t, err)
	proof := signLicenseChallenge(t, c, key)
	_, err = failing.Register(e.ctx, e.userID, "Rollback", proof)
	require.ErrorContains(t, err, "injected audit failure")
	devices, err := e.svc.Devices(e.ctx, e.userID)
	require.NoError(t, err)
	require.Empty(t, devices)
	_, err = e.svc.Register(e.ctx, e.userID, "Retry", proof)
	require.NoError(t, err)
	for range 5 {
		_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
		require.NoError(t, err)
	}
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
	require.ErrorIs(t, err, service.ErrLocalLicenseRateLimit)
}

func TestLocalDevicePasswordAndEntitlementChangesInvalidateChallenges(t *testing.T) {
	for _, change := range []string{"password", "disabled", "unpaid", "email"} {
		t.Run(change, func(t *testing.T) {
			e := newLicenseTestEnv(t)
			key, jwk := newLicenseDeviceKey(t)
			c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
			require.NoError(t, err)
			update := testEntClient(t).User.UpdateOneID(e.userID)
			switch change {
			case "password":
				update.SetPasswordHash("rotated")
			case "disabled":
				update.SetStatus(service.StatusDisabled)
			case "unpaid":
				update.ClearLocalModelAccessUnlockedAt()
			case "email":
				update.SetEmail("changed-" + c.Challenge + "@test.example")
			}
			require.NoError(t, update.Exec(e.ctx))
			_, err = e.svc.Register(e.ctx, e.userID, "Blocked", signLicenseChallenge(t, c, key))
			require.Error(t, err)
		})
	}
}

func TestLocalDeviceRenewalBindsExactLicenseAndDevice(t *testing.T) {
	e := newLicenseTestEnv(t)
	d, key := e.register(t)
	other, _ := e.register(t)
	lease, err := e.svc.Issue(e.ctx, e.userID, e.issueProof(t, d, key, ""), "", false)
	require.NoError(t, err)
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "renew", DeviceID: other.ID, License: lease.License})
	require.ErrorIs(t, err, service.ErrLocalLicenseInvalid)
	second, err := e.svc.Issue(e.ctx, e.userID, e.issueProof(t, d, key, ""), "", false)
	require.NoError(t, err)
	proof := e.issueProof(t, d, key, lease.License)
	_, err = e.svc.Issue(e.ctx, e.userID, proof, second.License, true)
	require.ErrorIs(t, err, service.ErrLocalLicenseInvalid)
	_, err = e.svc.Issue(e.ctx, e.userID, proof, "", false)
	require.ErrorIs(t, err, service.ErrLocalProofInvalid)
	_, err = e.svc.Issue(e.ctx, e.userID, proof, lease.License, true)
	require.NoError(t, err)
	// A registered key cannot be renewed after account credential rotation.
	require.NoError(t, testEntClient(t).User.UpdateOneID(e.userID).SetPasswordHash("new-hash").Exec(e.ctx))
	_, err = e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "renew", DeviceID: d.ID, License: lease.License})
	require.ErrorIs(t, err, service.ErrLocalLicenseInvalid)
}

func TestLocalDeviceRateLimitsCannotBeBypassedByConsumptionOrRevocation(t *testing.T) {
	t.Run("device churn", func(t *testing.T) {
		e := newLicenseTestEnv(t)
		for range 5 {
			d, _ := e.register(t)
			require.NoError(t, e.svc.RevokeDevice(e.ctx, e.userID, d.ID))
		}
		key, jwk := newLicenseDeviceKey(t)
		c, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "register", PublicKey: jwk})
		require.NoError(t, err)
		_, err = e.svc.Register(e.ctx, e.userID, "too many replacements", signLicenseChallenge(t, c, key))
		require.ErrorIs(t, err, service.ErrLocalLicenseRateLimit)
	})
	t.Run("consumed challenges", func(t *testing.T) {
		e := newLicenseTestEnv(t)
		d, key := e.register(t) // first of the 30 allowed challenges
		for range 29 {
			_, err := e.svc.Issue(e.ctx, e.userID, e.issueProof(t, d, key, ""), "", false)
			require.NoError(t, err)
		}
		_, err := e.svc.Challenge(e.ctx, e.userID, service.LocalModelChallengeInput{Action: "issue", DeviceID: d.ID})
		require.ErrorIs(t, err, service.ErrLocalLicenseRateLimit)
	})
}
