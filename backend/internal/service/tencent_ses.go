package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	tencentSESService = "ses"
	tencentSESVersion = "2020-10-02"
)

// tencentSESEndpoint is a var so tests can point requests at a local server.
var tencentSESEndpoint = "https://ses.tencentcloudapi.com"

// tencentSESConfig is intentionally environment-only.  SES access keys must
// never be persisted in the settings table or checked into a configuration file.
type tencentSESConfig struct {
	SecretID   string
	SecretKey  string
	Region     string
	From       string
	TemplateID int64
	// TemplateVariable is the template placeholder that receives the OTP code.
	// UsernameVariable receives a display name (we use the recipient's email).
	// Both must match the variable names configured in the Tencent SES template.
	TemplateVariable string
	UsernameVariable string
	// ResetTemplateID / ResetTemplateVariable configure the password-reset email.
	// When ResetTemplateID is 0 the reset flow keeps using the SMTP path.
	// ResetTemplateVariable is the placeholder that receives the reset link.
	ResetTemplateID       int64
	ResetTemplateVariable string
}

// ResetEnabled reports whether password-reset emails should be sent via Tencent SES.
func (c *tencentSESConfig) ResetEnabled() bool {
	return c != nil && c.ResetTemplateID > 0
}

func loadTencentSESConfig() (*tencentSESConfig, error) {
	secretID := strings.TrimSpace(os.Getenv("TENCENT_SES_SECRET_ID"))
	secretKey := strings.TrimSpace(os.Getenv("TENCENT_SES_SECRET_KEY"))
	from := strings.TrimSpace(os.Getenv("TENCENT_SES_FROM"))
	templateIDText := strings.TrimSpace(os.Getenv("TENCENT_SES_TEMPLATE_ID"))

	if secretID == "" && secretKey == "" && from == "" && templateIDText == "" {
		return nil, nil
	}
	if secretID == "" || secretKey == "" || from == "" || templateIDText == "" {
		return nil, fmt.Errorf("tencent SES configuration is incomplete")
	}
	templateID, err := strconv.ParseInt(templateIDText, 10, 64)
	if err != nil || templateID <= 0 {
		return nil, fmt.Errorf("invalid TENCENT_SES_TEMPLATE_ID")
	}

	region := strings.TrimSpace(os.Getenv("TENCENT_SES_REGION"))
	if region == "" {
		region = "ap-guangzhou"
	}
	variable := strings.TrimSpace(os.Getenv("TENCENT_SES_TEMPLATE_VARIABLE"))
	if variable == "" {
		variable = "otp_code"
	}
	usernameVariable := strings.TrimSpace(os.Getenv("TENCENT_SES_TEMPLATE_USERNAME_VARIABLE"))
	if usernameVariable == "" {
		usernameVariable = "username"
	}

	// Password-reset template is optional. When unset/invalid, reset emails keep
	// using the SMTP path (ResetTemplateID stays 0 → ResetEnabled() is false).
	var resetTemplateID int64
	if resetText := strings.TrimSpace(os.Getenv("TENCENT_SES_RESET_TEMPLATE_ID")); resetText != "" {
		if id, convErr := strconv.ParseInt(resetText, 10, 64); convErr == nil && id > 0 {
			resetTemplateID = id
		} else {
			return nil, fmt.Errorf("invalid TENCENT_SES_RESET_TEMPLATE_ID")
		}
	}
	resetVariable := strings.TrimSpace(os.Getenv("TENCENT_SES_RESET_TEMPLATE_VARIABLE"))
	if resetVariable == "" {
		resetVariable = "reset_url"
	}

	return &tencentSESConfig{
		SecretID: secretID, SecretKey: secretKey, Region: region, From: from,
		TemplateID: templateID, TemplateVariable: variable, UsernameVariable: usernameVariable,
		ResetTemplateID: resetTemplateID, ResetTemplateVariable: resetVariable,
	}, nil
}

// sendTencentSESVerifyCode sends registration/login verification codes via Tencent SES.
func (s *EmailService) sendTencentSESVerifyCode(ctx context.Context, config *tencentSESConfig, to, siteName, code string) error {
	// The username placeholder has no real name to show at signup, so we use the
	// recipient's email address.
	templateVars := map[string]string{config.TemplateVariable: code}
	if config.UsernameVariable != "" {
		templateVars[config.UsernameVariable] = to
	}
	subject := fmt.Sprintf("[%s] Email Verification Code", siteName)
	return s.sendTencentSESTemplate(ctx, config, to, subject, config.TemplateID, templateVars)
}

// sendTencentSESPasswordReset sends the password-reset email via Tencent SES using
// the dedicated reset template. resetURL is the full link (with email+token).
func (s *EmailService) sendTencentSESPasswordReset(ctx context.Context, config *tencentSESConfig, to, siteName, resetURL string) error {
	templateVars := map[string]string{config.ResetTemplateVariable: resetURL}
	if config.UsernameVariable != "" {
		templateVars[config.UsernameVariable] = to
	}
	subject := fmt.Sprintf("[%s] Password Reset", siteName)
	return s.sendTencentSESTemplate(ctx, config, to, subject, config.ResetTemplateID, templateVars)
}

// sendTencentSESTemplate posts a templated email to the Tencent SES SendEmail API.
func (s *EmailService) sendTencentSESTemplate(ctx context.Context, config *tencentSESConfig, to, subject string, templateID int64, templateVars map[string]string) error {
	templateData, err := json.Marshal(templateVars)
	if err != nil {
		return fmt.Errorf("marshal Tencent SES template data: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"FromEmailAddress": config.From,
		"Destination":      []string{to},
		"Subject":          subject,
		"Template": map[string]any{
			"TemplateID":   templateID,
			"TemplateData": string(templateData),
		},
	})
	if err != nil {
		return fmt.Errorf("marshal Tencent SES request: %w", err)
	}

	now := time.Now().UTC()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tencentSESEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Tencent SES request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "ses.tencentcloudapi.com")
	req.Header.Set("X-TC-Action", "SendEmail")
	req.Header.Set("X-TC-Version", tencentSESVersion)
	req.Header.Set("X-TC-Region", config.Region)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("Authorization", tencentSESAuthorization(config, now, payload))

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send Tencent SES request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Tencent SES response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("tencent SES returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Response struct {
			Error *struct {
				Code string `json:"Code"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode Tencent SES response: %w", err)
	}
	if result.Response.Error != nil {
		return fmt.Errorf("tencent SES rejected email: %s", result.Response.Error.Code)
	}
	return nil
}

// SendTencentSESTestEmail sends a one-off verification-code email through
// Tencent SES using the environment configuration. It exists for deployment
// verification (cmd/sestest) and bypasses the Redis-backed code store.
func SendTencentSESTestEmail(ctx context.Context, to, siteName, code string) error {
	config, err := loadTencentSESConfig()
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("tencent SES is not configured: set TENCENT_SES_SECRET_ID/SECRET_KEY/FROM/TEMPLATE_ID")
	}
	var s EmailService
	return s.sendTencentSESVerifyCode(ctx, config, to, siteName, code)
}

func tencentSESAuthorization(config *tencentSESConfig, now time.Time, payload []byte) string {
	payloadHash := sha256Hex(payload)
	canonicalHeaders := "content-type:application/json\nhost:ses.tencentcloudapi.com\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		http.MethodPost, "/", "", canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")
	date := now.UTC().Format("2006-01-02")
	credentialScope := date + "/" + tencentSESService + "/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))
	secretDate := hmacSHA256([]byte("TC3"+config.SecretKey), date)
	secretService := hmacSHA256(secretDate, tencentSESService)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	return "TC3-HMAC-SHA256 Credential=" + config.SecretID + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
