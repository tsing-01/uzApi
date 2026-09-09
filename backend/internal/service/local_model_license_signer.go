package service

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uzapi/internal/config"
)

const localLicenseType = "uzapi-local-model-license+jwt"

type localModelLicenseSigner struct {
	private      ed25519.PrivateKey
	verification map[string]ed25519.PublicKey
	kid          string
	issuer       string
}

type localModelLicenseClaims struct {
	jwt.RegisteredClaims
	DeviceID            string `json:"device_id"`
	DeviceKeyThumbprint string `json:"device_key_thumbprint"`
	Feature             string `json:"feature"`
	GrantVersion        int64  `json:"grant_version"`
	TokenVersion        string `json:"token_version"`
}

func licenseKeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func NewLocalModelLicenseService(repo LocalModelLicenseRepository, cfg *config.Config) (*LocalModelLicenseService, error) {
	svc := &LocalModelLicenseService{repo: repo, maxDevices: 2}
	if cfg == nil {
		return svc, nil
	}
	c := cfg.LocalModelAccess
	if c.MaxDevices != 0 {
		svc.maxDevices = c.MaxDevices
	}
	if svc.maxDevices < 1 || svc.maxDevices > 10 {
		return nil, fmt.Errorf("local_model_access.max_devices must be between 1 and 10")
	}
	seedText := strings.TrimSpace(c.SigningSeed)
	if seedText == "" {
		return svc, nil
	}
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("local_model_access.signing_seed must be a base64 encoded 32-byte Ed25519 seed")
	}
	issuer := strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("local_model_access.issuer must be an absolute HTTPS URL without credentials, query or fragment")
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := ed25519.PublicKey(private[ed25519.SeedSize:])
	signer := &localModelLicenseSigner{private: private, kid: licenseKeyID(public), issuer: issuer, verification: map[string]ed25519.PublicKey{}}
	signer.verification[signer.kid] = public
	if len(c.PreviousPublicKeys) > 8 {
		return nil, fmt.Errorf("local_model_access.previous_public_keys supports at most 8 keys")
	}
	for _, encoded := range c.PreviousPublicKeys {
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("local_model_access.previous_public_keys must contain base64url encoded Ed25519 public keys")
		}
		key := ed25519.PublicKey(raw)
		signer.verification[licenseKeyID(key)] = key
	}
	svc.signer = signer
	return svc, nil
}

func (s *localModelLicenseSigner) sign(u *User, d *LocalModelDevice, now time.Time) (*LocalModelLicenseResponse, error) {
	now = now.UTC().Truncate(time.Second)
	id, err := GenerateRedeemCode()
	if err != nil {
		return nil, err
	}
	expires := now.Add(LocalModelLicenseTTL)
	claims := localModelLicenseClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: s.issuer, Subject: strconv.FormatInt(u.ID, 10), Audience: jwt.ClaimStrings{LocalModelLicenseAudience}, ExpiresAt: jwt.NewNumericDate(expires), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: id},
		DeviceID: d.ID, DeviceKeyThumbprint: d.KeyThumbprint, Feature: RedeemTypeLocalModelAccess, GrantVersion: u.LocalModelAccessVersion, TokenVersion: strconv.FormatInt(resolvedTokenVersion(u), 10)}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = localLicenseType
	token.Header["kid"] = s.kid
	encoded, err := token.SignedString(s.private)
	if err != nil {
		return nil, err
	}
	return &LocalModelLicenseResponse{License: encoded, ExpiresAt: expires, RenewAfter: now.Add(2 * time.Minute), DeviceID: d.ID}, nil
}

func (s *localModelLicenseSigner) validate(raw string, u *User, d *LocalModelDevice, now time.Time) (*localModelLicenseClaims, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, ErrLocalLicenseInvalid
	}
	claims := &localModelLicenseClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Header["typ"] != localLicenseType || token.Header["crit"] != nil {
			return nil, ErrLocalLicenseInvalid
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, ErrLocalLicenseInvalid
		}
		key, ok := s.verification[kid]
		if !ok {
			return nil, ErrLocalLicenseInvalid
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithIssuer(s.issuer), jwt.WithAudience(LocalModelLicenseAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || !token.Valid || claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil {
		return nil, ErrLocalLicenseInvalid
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > LocalModelLicenseTTL || !claims.ExpiresAt.After(claims.IssuedAt.Time) || len(claims.ID) != 32 || claims.Subject != strconv.FormatInt(u.ID, 10) || claims.DeviceID != d.ID || claims.DeviceKeyThumbprint != d.KeyThumbprint || claims.Feature != RedeemTypeLocalModelAccess || claims.GrantVersion != u.LocalModelAccessVersion || claims.TokenVersion != strconv.FormatInt(resolvedTokenVersion(u), 10) {
		return nil, ErrLocalLicenseInvalid
	}
	return claims, nil
}

func (s *localModelLicenseSigner) publicKeys() []map[string]string {
	ids := make([]string, 0, len(s.verification))
	for id := range s.verification {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	keys := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, map[string]string{"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": id, "x": base64.RawURLEncoding.EncodeToString(s.verification[id])})
	}
	return keys
}
