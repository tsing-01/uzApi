package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setTencentSESEnv(t *testing.T, id, key, from, templateID string) {
	t.Helper()
	t.Setenv("TENCENT_SES_SECRET_ID", id)
	t.Setenv("TENCENT_SES_SECRET_KEY", key)
	t.Setenv("TENCENT_SES_FROM", from)
	t.Setenv("TENCENT_SES_TEMPLATE_ID", templateID)
	t.Setenv("TENCENT_SES_REGION", "")
	t.Setenv("TENCENT_SES_TEMPLATE_VARIABLE", "")
}

func TestLoadTencentSESConfig_NotConfigured(t *testing.T) {
	setTencentSESEnv(t, "", "", "", "")

	cfg, err := loadTencentSESConfig()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when nothing configured, got %+v", cfg)
	}
}

func TestLoadTencentSESConfig_Incomplete(t *testing.T) {
	setTencentSESEnv(t, "AKIDexample", "", "noreply@example.com", "12345")

	if _, err := loadTencentSESConfig(); err == nil {
		t.Fatal("expected error for incomplete configuration")
	}
}

func TestLoadTencentSESConfig_InvalidTemplateID(t *testing.T) {
	setTencentSESEnv(t, "AKIDexample", "secret", "noreply@example.com", "not-a-number")

	if _, err := loadTencentSESConfig(); err == nil {
		t.Fatal("expected error for invalid template id")
	}
}

func TestLoadTencentSESConfig_Defaults(t *testing.T) {
	setTencentSESEnv(t, "AKIDexample", "secret", "noreply@example.com", "12345")

	cfg, err := loadTencentSESConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Region != "ap-guangzhou" {
		t.Errorf("expected default region ap-guangzhou, got %q", cfg.Region)
	}
	if cfg.TemplateVariable != "otp_code" {
		t.Errorf("expected default template variable otp_code, got %q", cfg.TemplateVariable)
	}
	if cfg.UsernameVariable != "username" {
		t.Errorf("expected default username variable username, got %q", cfg.UsernameVariable)
	}
	if cfg.TemplateID != 12345 {
		t.Errorf("expected template id 12345, got %d", cfg.TemplateID)
	}
}

func TestTencentSESAuthorization_Format(t *testing.T) {
	cfg := &tencentSESConfig{SecretID: "AKIDexample", SecretKey: "secret"}
	now := time.Unix(1700000000, 0).UTC()

	auth := tencentSESAuthorization(cfg, now, []byte(`{"a":1}`))

	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDexample/2023-11-14/ses/tc3_request, SignedHeaders=content-type;host, Signature=") {
		t.Fatalf("unexpected authorization header: %s", auth)
	}
	sig := auth[strings.LastIndex(auth, "=")+1:]
	if len(sig) != 64 {
		t.Fatalf("expected 64-char hex signature, got %d chars: %s", len(sig), sig)
	}
	// Same inputs must always produce the same signature.
	if again := tencentSESAuthorization(cfg, now, []byte(`{"a":1}`)); again != auth {
		t.Fatal("signature is not deterministic")
	}
}

func newTencentSESTestServer(t *testing.T, status int, responseBody string, gotBody *map[string]any, gotHeaders *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if gotBody != nil {
			_ = json.Unmarshal(body, gotBody)
		}
		if gotHeaders != nil {
			*gotHeaders = r.Header.Clone()
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
}

func withTencentSESEndpoint(t *testing.T, url string) {
	t.Helper()
	old := tencentSESEndpoint
	tencentSESEndpoint = url
	t.Cleanup(func() { tencentSESEndpoint = old })
}

func testTencentSESConfig() *tencentSESConfig {
	return &tencentSESConfig{
		SecretID: "AKIDexample", SecretKey: "secret", Region: "ap-guangzhou",
		From: "noreply@example.com", TemplateID: 12345,
		TemplateVariable: "otp_code", UsernameVariable: "username",
	}
}

func TestSendTencentSESVerifyCode_Success(t *testing.T) {
	var gotBody map[string]any
	var gotHeaders http.Header
	srv := newTencentSESTestServer(t, http.StatusOK, `{"Response":{"RequestId":"req-1","MessageId":"msg-1"}}`, &gotBody, &gotHeaders)
	defer srv.Close()
	withTencentSESEndpoint(t, srv.URL)

	svc := &EmailService{}
	if err := svc.sendTencentSESVerifyCode(context.Background(), testTencentSESConfig(), "user@example.com", "uzApi", "654321"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotHeaders.Get("X-TC-Action") != "SendEmail" {
		t.Errorf("expected X-TC-Action SendEmail, got %q", gotHeaders.Get("X-TC-Action"))
	}
	if gotHeaders.Get("X-TC-Region") != "ap-guangzhou" {
		t.Errorf("expected X-TC-Region ap-guangzhou, got %q", gotHeaders.Get("X-TC-Region"))
	}
	if !strings.HasPrefix(gotHeaders.Get("Authorization"), "TC3-HMAC-SHA256 ") {
		t.Errorf("unexpected Authorization header: %q", gotHeaders.Get("Authorization"))
	}

	if gotBody["FromEmailAddress"] != "noreply@example.com" {
		t.Errorf("unexpected FromEmailAddress: %v", gotBody["FromEmailAddress"])
	}
	dest, _ := gotBody["Destination"].([]any)
	if len(dest) != 1 || dest[0] != "user@example.com" {
		t.Errorf("unexpected Destination: %v", gotBody["Destination"])
	}
	tmpl, _ := gotBody["Template"].(map[string]any)
	if tmpl == nil || tmpl["TemplateID"] != float64(12345) {
		t.Errorf("unexpected Template: %v", gotBody["Template"])
	}
	templateDataStr, ok := tmpl["TemplateData"].(string)
	if !ok {
		t.Fatalf("TemplateData is not a string: %v", tmpl["TemplateData"])
	}
	var templateData map[string]string
	if err := json.Unmarshal([]byte(templateDataStr), &templateData); err != nil {
		t.Fatalf("failed to decode TemplateData: %v", err)
	}
	if templateData["otp_code"] != "654321" {
		t.Errorf("expected otp_code 654321, got %q", templateData["otp_code"])
	}
	if templateData["username"] != "user@example.com" {
		t.Errorf("expected username to be recipient email, got %q", templateData["username"])
	}
}

func TestSendTencentSESVerifyCode_APIError(t *testing.T) {
	srv := newTencentSESTestServer(t, http.StatusOK, `{"Response":{"Error":{"Code":"FailedOperation.SendEmailFail","Message":"bad"}}}`, nil, nil)
	defer srv.Close()
	withTencentSESEndpoint(t, srv.URL)

	svc := &EmailService{}
	err := svc.sendTencentSESVerifyCode(context.Background(), testTencentSESConfig(), "user@example.com", "uzApi", "654321")
	if err == nil || !strings.Contains(err.Error(), "FailedOperation.SendEmailFail") {
		t.Fatalf("expected API error with code, got %v", err)
	}
}

func TestSendTencentSESVerifyCode_HTTPError(t *testing.T) {
	srv := newTencentSESTestServer(t, http.StatusInternalServerError, `{}`, nil, nil)
	defer srv.Close()
	withTencentSESEndpoint(t, srv.URL)

	svc := &EmailService{}
	err := svc.sendTencentSESVerifyCode(context.Background(), testTencentSESConfig(), "user@example.com", "uzApi", "654321")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected HTTP status error, got %v", err)
	}
}
