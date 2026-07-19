// Command sestest sends a real verification-code email through Tencent SES to
// verify the deployment configuration. It reads the same TENCENT_SES_* env
// variables as the server:
//
//	TENCENT_SES_SECRET_ID / TENCENT_SES_SECRET_KEY / TENCENT_SES_FROM /
//	TENCENT_SES_TEMPLATE_ID (required)
//	TENCENT_SES_REGION / TENCENT_SES_TEMPLATE_VARIABLE (optional)
//
// Usage:
//
//	go run ./cmd/sestest -to you@example.com
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/uzapi/internal/service"
)

func main() {
	to := flag.String("to", "", "recipient email address (required)")
	site := flag.String("site", "uzApi", "site name used in the email subject")
	code := flag.String("code", "888888", "verification code to embed in the template")
	flag.Parse()

	if *to == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := service.SendTencentSESTestEmail(ctx, *to, *site, *code); err != nil {
		fmt.Fprintf(os.Stderr, "send failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Tencent SES test email sent to %s (code %s)\n", *to, *code)
}
