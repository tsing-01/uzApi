package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/uzapi/internal/pkg/errors"
)

const (
	LocalModelLicenseAudience = "24hbutler.local-model-access"
	LocalModelLicenseTTL      = 5 * time.Minute
	localModelChallengeTTL    = time.Minute
)

var (
	ErrLocalLicenseDisabled  = infraerrors.ServiceUnavailable("LOCAL_LICENSE_DISABLED", "device licensing is not configured")
	ErrLocalAccessDenied     = infraerrors.Forbidden("LOCAL_MODEL_ACCESS_REQUIRED", "active paid local model access is required")
	ErrLocalDeviceNotFound   = infraerrors.NotFound("LOCAL_DEVICE_NOT_FOUND", "active device not found")
	ErrLocalDeviceRevoked    = infraerrors.Forbidden("LOCAL_DEVICE_REVOKED", "device key was revoked; register a new key")
	ErrLocalDeviceLimit      = infraerrors.Conflict("LOCAL_DEVICE_LIMIT", "active device limit reached")
	ErrLocalLicenseRateLimit = infraerrors.TooManyRequests("LOCAL_LICENSE_RATE_LIMIT", "too many device licensing requests")
	ErrLocalProofInvalid     = infraerrors.BadRequest("LOCAL_DEVICE_PROOF_INVALID", "invalid, expired or consumed device proof")
	ErrLocalLicenseInvalid   = infraerrors.BadRequest("LOCAL_LICENSE_INVALID", "invalid or expired local model license")
)

// DevicePublicKey is a public-only P-256 JWK. The private key never leaves the device.
type DevicePublicKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d,omitempty"` // Explicitly reject accidental private-key submission.
}

func (k DevicePublicKey) parse() (*ecdsa.PublicKey, string, error) {
	if k.Kty != "EC" || k.Crv != "P-256" || k.D != "" {
		return nil, "", ErrLocalProofInvalid
	}
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil || len(x) != 32 || base64.RawURLEncoding.EncodeToString(x) != k.X {
		return nil, "", ErrLocalProofInvalid
	}
	y, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil || len(y) != 32 || base64.RawURLEncoding.EncodeToString(y) != k.Y {
		return nil, "", ErrLocalProofInvalid
	}
	key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	if !key.IsOnCurve(key.X, key.Y) {
		return nil, "", ErrLocalProofInvalid
	}
	// RFC 7638 canonical JWK member ordering.
	canonical, err := json.Marshal(struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}{k.Crv, k.Kty, k.X, k.Y})
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return key, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type LocalModelDevice struct {
	ID            string          `json:"id"`
	UserID        int64           `json:"-"`
	Name          string          `json:"name"`
	PublicKey     DevicePublicKey `json:"public_key"`
	KeyThumbprint string          `json:"key_thumbprint"`
	CreatedAt     time.Time       `json:"created_at"`
	LastIssuedAt  *time.Time      `json:"last_issued_at"`
	RevokedAt     *time.Time      `json:"revoked_at"`
}

type LocalModelChallenge struct {
	TokenHash           string
	UserID              int64
	Action              string
	DeviceID            string
	PublicKey           DevicePublicKey
	KeyThumbprint       string
	GrantVersion        int64
	TokenVersion        int64
	PreviousLicenseHash string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	ConsumedAt          *time.Time
}

type LocalModelChallengeInput struct {
	Action    string          `json:"action" binding:"required,oneof=register issue renew"`
	DeviceID  string          `json:"device_id" binding:"omitempty,len=32"`
	PublicKey DevicePublicKey `json:"public_key"`
	License   string          `json:"license" binding:"max=4096"`
}

type LocalModelProof struct {
	Challenge string `json:"challenge" binding:"required,len=64"`
	Signature string `json:"signature" binding:"required,len=86"`
}

type LocalModelChallengeResponse struct {
	Challenge string    `json:"challenge"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}

type LocalModelLicenseResponse struct {
	License    string    `json:"license"`
	ExpiresAt  time.Time `json:"expires_at"`
	RenewAfter time.Time `json:"renew_after"`
	DeviceID   string    `json:"device_id"`
}

// All entitlement/device mutations serialize on the same users row as redemption.
// Implementations must commit the callback atomically, or roll it back on any error.
type LocalModelLicenseRepository interface {
	WithUser(context.Context, int64, func(*User, LocalModelLicenseStore) error) error
}

type LocalModelLicenseStore interface {
	ChallengeCounts(context.Context, time.Time) (recent, pending int, err error)
	CreateChallenge(context.Context, *LocalModelChallenge) error
	GetChallenge(context.Context, string) (*LocalModelChallenge, error)
	ConsumeChallenge(context.Context, string, time.Time) error
	Devices(context.Context) ([]LocalModelDevice, error)
	Device(context.Context, string) (*LocalModelDevice, error)
	DeviceByKey(context.Context, string) (*LocalModelDevice, error)
	RegistrationsSince(context.Context, time.Time) (int, error)
	CreateDevice(context.Context, *LocalModelDevice) error
	TouchDevice(context.Context, string, time.Time) error
	RevokeDevice(context.Context, string, time.Time) (bool, error)
	RevokeEntitlement(context.Context, time.Time) error
	Audit(context.Context, int64, string, string, string, time.Time) error
}

type LocalModelLicenseService struct {
	repo       LocalModelLicenseRepository
	signer     *localModelLicenseSigner
	maxDevices int
}

func requireLocalAccess(u *User) error {
	if u == nil || !u.IsActive() || u.DeletedAt != nil || !u.HasLocalModelAccess() {
		return ErrLocalAccessDenied
	}
	return nil
}

func licenseHash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

func (s *LocalModelLicenseService) proofMessage(c *LocalModelChallenge, nonce string) string {
	path := map[string]string{"register": "devices", "issue": "licenses", "renew": "licenses/renew"}[c.Action]
	return strings.Join([]string{"UZAPI-DEVICE-PROOF-V1", s.signer.issuer, "POST", "/api/v1/local-model-access/" + path,
		strconv.FormatInt(c.UserID, 10), c.DeviceID, c.KeyThumbprint, strconv.FormatInt(c.GrantVersion, 10), strconv.FormatInt(c.TokenVersion, 10), nonce, c.PreviousLicenseHash}, "\n")
}

func (s *LocalModelLicenseService) Challenge(ctx context.Context, userID int64, input LocalModelChallengeInput) (*LocalModelChallengeResponse, error) {
	if s.signer == nil {
		return nil, ErrLocalLicenseDisabled
	}
	if input.Action != "register" && input.Action != "issue" && input.Action != "renew" {
		return nil, ErrLocalProofInvalid
	}
	// The nonce is 256 random bits. Only its hash is persisted.
	a, err := GenerateRedeemCode()
	if err != nil {
		return nil, err
	}
	b, err := GenerateRedeemCode()
	if err != nil {
		return nil, err
	}
	nonce := a + b
	var out *LocalModelChallengeResponse
	err = s.repo.WithUser(ctx, userID, func(u *User, store LocalModelLicenseStore) error {
		if err := requireLocalAccess(u); err != nil {
			return err
		}
		now := time.Now().UTC()
		recent, pending, err := store.ChallengeCounts(ctx, now)
		if err != nil {
			return err
		}
		if recent >= 30 || pending >= 5 {
			return ErrLocalLicenseRateLimit
		}
		c := &LocalModelChallenge{TokenHash: licenseHash(nonce), UserID: userID, Action: input.Action, GrantVersion: u.LocalModelAccessVersion,
			TokenVersion: resolvedTokenVersion(u), CreatedAt: now, ExpiresAt: now.Add(localModelChallengeTTL)}
		if input.Action == "register" {
			if input.DeviceID != "" || input.License != "" {
				return ErrLocalProofInvalid
			}
			_, thumb, err := input.PublicKey.parse()
			if err != nil {
				return err
			}
			c.PublicKey, c.KeyThumbprint = input.PublicKey, thumb
		} else {
			if input.PublicKey != (DevicePublicKey{}) {
				return ErrLocalProofInvalid
			}
			d, err := store.Device(ctx, input.DeviceID)
			if err != nil {
				return err
			}
			if d.RevokedAt != nil {
				return ErrLocalDeviceRevoked
			}
			c.DeviceID, c.PublicKey, c.KeyThumbprint = d.ID, d.PublicKey, d.KeyThumbprint
			if input.Action == "renew" {
				if _, err := s.signer.validate(input.License, u, d, now); err != nil {
					return err
				}
				c.PreviousLicenseHash = licenseHash(input.License)
			} else if input.License != "" {
				return ErrLocalProofInvalid
			}
		}
		if err := store.CreateChallenge(ctx, c); err != nil {
			return err
		}
		out = &LocalModelChallengeResponse{Challenge: nonce, Message: s.proofMessage(c, nonce), ExpiresAt: c.ExpiresAt}
		return nil
	})
	return out, err
}

func (s *LocalModelLicenseService) useProof(ctx context.Context, u *User, store LocalModelLicenseStore, action string, proof LocalModelProof) (*LocalModelChallenge, error) {
	if err := requireLocalAccess(u); err != nil {
		return nil, err
	}
	if len(proof.Challenge) != 64 || len(proof.Signature) != 86 {
		return nil, ErrLocalProofInvalid
	}
	c, err := store.GetChallenge(ctx, licenseHash(proof.Challenge))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if c.UserID != u.ID || c.Action != action || c.ConsumedAt != nil || !c.ExpiresAt.After(now) || c.GrantVersion != u.LocalModelAccessVersion || c.TokenVersion != resolvedTokenVersion(u) {
		return nil, ErrLocalProofInvalid
	}
	key, thumb, err := c.PublicKey.parse()
	if err != nil || thumb != c.KeyThumbprint {
		return nil, ErrLocalProofInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(proof.Signature)
	if err != nil || len(sig) != 64 || base64.RawURLEncoding.EncodeToString(sig) != proof.Signature {
		return nil, ErrLocalProofInvalid
	}
	digest := sha256.Sum256([]byte(s.proofMessage(c, proof.Challenge)))
	if !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return nil, ErrLocalProofInvalid
	}
	if err := store.ConsumeChallenge(ctx, c.TokenHash, now); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *LocalModelLicenseService) Register(ctx context.Context, userID int64, name string, proof LocalModelProof) (*LocalModelDevice, error) {
	if s.signer == nil {
		return nil, ErrLocalLicenseDisabled
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n\x00") {
		return nil, infraerrors.BadRequest("LOCAL_DEVICE_NAME_INVALID", "device name must contain 1-100 bytes without control line breaks")
	}
	var out *LocalModelDevice
	err := s.repo.WithUser(ctx, userID, func(u *User, store LocalModelLicenseStore) error {
		c, err := s.useProof(ctx, u, store, "register", proof)
		if err != nil {
			return err
		}
		existing, err := store.DeviceByKey(ctx, c.KeyThumbprint)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.RevokedAt != nil {
				return ErrLocalDeviceRevoked
			}
			out = existing
			return nil
		}
		devices, err := store.Devices(ctx)
		if err != nil {
			return err
		}
		active := 0
		for _, d := range devices {
			if d.RevokedAt == nil {
				active++
			}
		}
		if active >= s.maxDevices {
			return ErrLocalDeviceLimit
		}
		now := time.Now().UTC()
		recent, err := store.RegistrationsSince(ctx, now.Add(-24*time.Hour))
		if err != nil {
			return err
		}
		if recent >= 5 {
			return ErrLocalLicenseRateLimit
		}
		id, err := GenerateRedeemCode()
		if err != nil {
			return err
		}
		out = &LocalModelDevice{ID: id, UserID: userID, Name: name, PublicKey: c.PublicKey, KeyThumbprint: c.KeyThumbprint, CreatedAt: now}
		if err := store.CreateDevice(ctx, out); err != nil {
			return err
		}
		return store.Audit(ctx, userID, "device_registered", id, "", now)
	})
	return out, err
}

func (s *LocalModelLicenseService) Issue(ctx context.Context, userID int64, proof LocalModelProof, previous string, renew bool) (*LocalModelLicenseResponse, error) {
	if s.signer == nil {
		return nil, ErrLocalLicenseDisabled
	}
	action := "issue"
	if renew {
		action = "renew"
	} else if previous != "" {
		return nil, ErrLocalLicenseInvalid
	}
	var out *LocalModelLicenseResponse
	err := s.repo.WithUser(ctx, userID, func(u *User, store LocalModelLicenseStore) error {
		c, err := s.useProof(ctx, u, store, action, proof)
		if err != nil {
			return err
		}
		d, err := store.Device(ctx, c.DeviceID)
		if err != nil {
			return err
		}
		if d.RevokedAt != nil || d.KeyThumbprint != c.KeyThumbprint {
			return ErrLocalDeviceRevoked
		}
		now := time.Now().UTC()
		if renew {
			if c.PreviousLicenseHash != licenseHash(previous) {
				return ErrLocalLicenseInvalid
			}
			if _, err := s.signer.validate(previous, u, d, now); err != nil {
				return err
			}
		}
		out, err = s.signer.sign(u, d, now)
		if err != nil {
			return err
		}
		if err := store.TouchDevice(ctx, d.ID, now); err != nil {
			return err
		}
		return store.Audit(ctx, userID, "license_"+action, d.ID, "", now)
	})
	return out, err
}

func (s *LocalModelLicenseService) Devices(ctx context.Context, userID int64) ([]LocalModelDevice, error) {
	out := []LocalModelDevice{}
	err := s.repo.WithUser(ctx, userID, func(_ *User, store LocalModelLicenseStore) error {
		var err error
		out, err = store.Devices(ctx)
		return err
	})
	return out, err
}

func (s *LocalModelLicenseService) RevokeDevice(ctx context.Context, userID int64, deviceID string) error {
	return s.repo.WithUser(ctx, userID, func(_ *User, store LocalModelLicenseStore) error {
		now := time.Now().UTC()
		changed, err := store.RevokeDevice(ctx, deviceID, now)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return store.Audit(ctx, userID, "device_revoked", deviceID, "", now)
	})
}

func (s *LocalModelLicenseService) RevokeEntitlement(ctx context.Context, userID, actorID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return infraerrors.BadRequest("LOCAL_ACCESS_REVOCATION_REASON_REQUIRED", "reason must contain 1-500 bytes")
	}
	return s.repo.WithUser(ctx, userID, func(u *User, store LocalModelLicenseStore) error {
		if u.LocalModelAccessRevokedAt != nil {
			return nil
		}
		now := time.Now().UTC()
		if err := store.RevokeEntitlement(ctx, now); err != nil {
			return err
		}
		return store.Audit(ctx, actorID, "entitlement_revoked", "", reason, now)
	})
}

func (s *LocalModelLicenseService) PublicKeys() (map[string]any, error) {
	if s.signer == nil {
		return nil, ErrLocalLicenseDisabled
	}
	keys := s.signer.publicKeys()
	return map[string]any{"keys": keys, "issuer": s.signer.issuer, "audience": LocalModelLicenseAudience}, nil
}

// Do not log proof bodies, challenge values, license JWTs or signing material.
