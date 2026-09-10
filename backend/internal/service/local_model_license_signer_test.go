package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/uzapi/internal/config"
)

func licenseTestConfig(seed byte) *config.Config {
	return &config.Config{LocalModelAccess: config.LocalModelAccessConfig{SigningSeed: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32)), Issuer: "https://license.example.test", MaxDevices: 2}}
}
func TestLocalLicenseSignerValidation(t *testing.T) {
	svc, err := NewLocalModelLicenseService(nil, licenseTestConfig(1))
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	u := &User{ID: 7, Email: "license@example.test", PasswordHash: "hash", LocalModelAccessVersion: 3}
	d := &LocalModelDevice{ID: strings.Repeat("a", 32), KeyThumbprint: strings.Repeat("k", 43)}
	issued, err := svc.signer.sign(u, d, now)
	require.NoError(t, err)
	claims, err := svc.signer.validate(issued.License, u, d, now)
	require.NoError(t, err)
	require.Equal(t, LocalModelLicenseTTL, issued.ExpiresAt.Sub(now))
	require.Equal(t, LocalModelLicenseAudience, claims.Audience[0])
	require.Equal(t, issued.DeviceID, d.ID)
	for _, tc := range []struct {
		name   string
		mutate func(*localModelLicenseClaims, map[string]any)
	}{
		{"wrong audience", func(c *localModelLicenseClaims, _ map[string]any) { c.Audience = jwt.ClaimStrings{"other-app"} }},
		{"wrong issuer", func(c *localModelLicenseClaims, _ map[string]any) { c.Issuer = "https://other.test" }},
		{"wrong subject", func(c *localModelLicenseClaims, _ map[string]any) { c.Subject = "8" }},
		{"wrong device", func(c *localModelLicenseClaims, _ map[string]any) { c.DeviceID = strings.Repeat("b", 32) }},
		{"wrong device key", func(c *localModelLicenseClaims, _ map[string]any) { c.DeviceKeyThumbprint = "wrong" }},
		{"wrong feature", func(c *localModelLicenseClaims, _ map[string]any) { c.Feature = "balance" }},
		{"old grant", func(c *localModelLicenseClaims, _ map[string]any) { c.GrantVersion-- }},
		{"old login", func(c *localModelLicenseClaims, _ map[string]any) { c.TokenVersion = "old" }},
		{"expired", func(c *localModelLicenseClaims, _ map[string]any) { c.ExpiresAt = jwt.NewNumericDate(now) }},
		{"no expiry", func(c *localModelLicenseClaims, _ map[string]any) { c.ExpiresAt = nil }},
		{"future issued", func(c *localModelLicenseClaims, _ map[string]any) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(time.Minute))
		}},
		{"long lifetime", func(c *localModelLicenseClaims, _ map[string]any) {
			c.ExpiresAt = jwt.NewNumericDate(now.Add(24 * time.Hour))
		}},
		{"unknown kid", func(_ *localModelLicenseClaims, h map[string]any) { h["kid"] = "wrong" }},
		{"wrong type", func(_ *localModelLicenseClaims, h map[string]any) { h["typ"] = "JWT" }},
		{"critical header", func(_ *localModelLicenseClaims, h map[string]any) { h["crit"] = []string{"exp"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := *claims
			token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, &c)
			token.Header["kid"] = svc.signer.kid
			token.Header["typ"] = localLicenseType
			tc.mutate(&c, token.Header)
			raw, err := token.SignedString(svc.signer.private)
			require.NoError(t, err)
			_, err = svc.signer.validate(raw, u, d, now)
			require.ErrorIs(t, err, ErrLocalLicenseInvalid)
		})
	}
	hmac := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	hmac.Header["typ"] = localLicenseType
	hmac.Header["kid"] = svc.signer.kid
	raw, err := hmac.SignedString([]byte(svc.signer.verification[svc.signer.kid]))
	require.NoError(t, err)
	_, err = svc.signer.validate(raw, u, d, now)
	require.ErrorIs(t, err, ErrLocalLicenseInvalid)
	_, err = svc.signer.validate(issued.License+"x", u, d, now)
	require.ErrorIs(t, err, ErrLocalLicenseInvalid)
	_, err = svc.signer.validate(issued.License, u, d, issued.ExpiresAt)
	require.ErrorIs(t, err, ErrLocalLicenseInvalid)
}

func TestLocalLicenseKeyRotationAndFailClosedConfiguration(t *testing.T) {
	off, err := NewLocalModelLicenseService(nil, &config.Config{})
	require.NoError(t, err)
	_, err = off.PublicKeys()
	require.ErrorIs(t, err, ErrLocalLicenseDisabled)
	bad := licenseTestConfig(1)
	bad.LocalModelAccess.SigningSeed = "secret-that-must-not-be-echoed"
	_, err = NewLocalModelLicenseService(nil, bad)
	require.Error(t, err)
	require.NotContains(t, err.Error(), bad.LocalModelAccess.SigningSeed)
	bad = licenseTestConfig(1)
	bad.LocalModelAccess.Issuer = "http://untrusted.test"
	_, err = NewLocalModelLicenseService(nil, bad)
	require.Error(t, err)
	old, err := NewLocalModelLicenseService(nil, licenseTestConfig(1))
	require.NoError(t, err)
	nextCfg := licenseTestConfig(2)
	nextCfg.LocalModelAccess.PreviousPublicKeys = []string{base64.RawURLEncoding.EncodeToString(old.signer.verification[old.signer.kid])}
	next, err := NewLocalModelLicenseService(nil, nextCfg)
	require.NoError(t, err)
	u := &User{ID: 1, LocalModelAccessVersion: 1}
	d := &LocalModelDevice{ID: strings.Repeat("c", 32), KeyThumbprint: "thumb"}
	now := time.Now()
	token, err := old.signer.sign(u, d, now)
	require.NoError(t, err)
	_, err = next.signer.validate(token.License, u, d, now)
	require.NoError(t, err)
	keys, err := next.PublicKeys()
	require.NoError(t, err)
	serialized, err := json.Marshal(keys)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), nextCfg.LocalModelAccess.SigningSeed)
	require.NotContains(t, string(serialized), `"d":`)
	require.Len(t, keys["keys"], 2)
	nextCfg.LocalModelAccess.PreviousPublicKeys = nil
	next, err = NewLocalModelLicenseService(nil, nextCfg)
	require.NoError(t, err)
	_, err = next.signer.validate(token.License, u, d, now)
	require.ErrorIs(t, err, ErrLocalLicenseInvalid)
}

func TestDevicePublicKeyRejectsInvalidOrPrivateKeys(t *testing.T) {
	for _, key := range []DevicePublicKey{{}, {Kty: "RSA"}, {Kty: "EC", Crv: "P-256", X: strings.Repeat("A", 43), Y: strings.Repeat("A", 43)}, {Kty: "EC", Crv: "P-256", D: "private"}} {
		_, _, err := key.parse()
		require.ErrorIs(t, err, ErrLocalProofInvalid)
	}
}
