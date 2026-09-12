package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/uzapi/internal/pkg/errors"
	"github.com/uzapi/internal/pkg/response"
	"github.com/uzapi/internal/service"
)

type localModelQuotaService interface {
	Status(context.Context, int64) (*service.LocalModelAccessStatus, error)
	Authorize(context.Context, int64, string) (*service.LocalModelRequestAuthorization, error)
}

type LocalModelQuotaHandler struct{ quota localModelQuotaService }

func NewLocalModelQuotaHandler(quota *service.LocalModelQuotaService) *LocalModelQuotaHandler {
	return &LocalModelQuotaHandler{quota: quota}
}

func customAPIResponse(c *gin.Context, data any, err error) {
	if err == nil {
		response.Success(c, data)
		return
	}
	status, detail := infraerrors.ToHTTP(err)
	if retry := detail.Metadata["retry_after"]; retry != "" {
		c.Header("Retry-After", retry)
	}
	c.JSON(status, response.Response{Code: status, Message: detail.Message, Reason: detail.Reason, Data: data})
}

func (h *LocalModelQuotaHandler) Status(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		customAPIResponse(c, nil, service.ErrCustomAPIInvalidRequest)
		return
	}
	out, err := h.quota.Status(c.Request.Context(), id)
	if out != nil {
		c.Header("Date", out.ServerTime.Format(http.TimeFormat))
	}
	customAPIResponse(c, out, err)
}

// A single exact field keeps client-controlled identity, quota and upstream secrets
// out of the contract. Reject duplicate fields as well as unknown/trailing JSON.
func readCustomAPIRequestID(c *gin.Context) (string, bool) {
	if c.ContentType() != "application/json" || c.Request.URL.RawQuery != "" {
		return "", false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	d := json.NewDecoder(c.Request.Body)
	open, err := d.Token()
	if err != nil || open != json.Delim('{') || !d.More() {
		return "", false
	}
	key, err := d.Token()
	if err != nil || key != "request_id" {
		return "", false
	}
	var id string
	if err := d.Decode(&id); err != nil || d.More() {
		return "", false
	}
	closeToken, err := d.Token()
	if err != nil || closeToken != json.Delim('}') {
		return "", false
	}
	if _, err = d.Token(); err != io.EOF {
		return "", false
	}
	return id, true
}

func (h *LocalModelQuotaHandler) Authorize(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	requestID, ok := readCustomAPIRequestID(c)
	if !ok {
		customAPIResponse(c, nil, service.ErrCustomAPIInvalidRequest)
		return
	}
	start := time.Now()
	ctx := c.Request.Context()
	out, err := h.quota.Authorize(ctx, id, requestID)
	outcome, replayed := "allowed", false
	if err != nil {
		outcome = infraerrors.Reason(err)
	}
	if out != nil {
		replayed = out.Replayed
		c.Header("Date", out.ServerTime.Format(http.TimeFormat))
	}
	// No request body, access token, provider information or redemption code in logs.
	slog.InfoContext(ctx, "custom_api_authorization", "user_id", id, "outcome", outcome, "replayed", replayed, "duration_ms", time.Since(start).Milliseconds())
	customAPIResponse(c, out, err)
}
