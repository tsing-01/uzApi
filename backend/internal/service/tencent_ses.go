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
	tencentSESEndpoint = "https://ses.tencentcloudapi.com"
	tencentSESService  = "ses"
	tencentSESVersion  = "2020-10-02"
)

// tencentSESConfig is intentionally environment-only.  SES access keys must
// never be persisted in the settings table or checked into a configuration file.
type tencentSESConfig struct {
	SecretID         string
	SecretKey        string
	Region           string
	From             string
	TemplateID       int64
	TemplateVariable string
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
		variable = "code"
	}
	return &tencentSESConfig{
		SecretID: secretID, SecretKey: secretKey, Region: region, From: from,
		TemplateID: templateID, TemplateVariable: variable,
	}, nil
}

// sendTencentSESVerifyCode sends only verification messages through Tencent SES.
// Other mail (such as password reset) continues to use the configured SMTP path.
func (s *EmailService) sendTencentSESVerifyCode(ctx context.Context, config *tencentSESConfig, to, siteName, code string) error {
	templateData, err := json.Marshal(map[string]string{config.TemplateVariable: code})
	if err != nil {
		return fmt.Errorf("marshal Tencent SES template data: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"FromEmailAddress": config.From,
		"Destination":      []string{to},
		"Subject":          fmt.Sprintf("[%s] Email Verification Code", siteName),
		"Template": map[string]any{
			"TemplateID":   config.TemplateID,
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
