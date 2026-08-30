package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/uzapi/internal/domain"
	"github.com/uzapi/internal/handler/dto"
	"github.com/uzapi/internal/pkg/pagination"
	"github.com/uzapi/internal/pkg/response"
	"github.com/uzapi/internal/service"

	"github.com/gin-gonic/gin"
)

// ActivationCodeHandler 管理端激活码管理。
type ActivationCodeHandler struct {
	activationService *service.ActivationCodeService
}

// NewActivationCodeHandler 创建管理端激活码 handler。
func NewActivationCodeHandler(activationService *service.ActivationCodeService) *ActivationCodeHandler {
	return &ActivationCodeHandler{activationService: activationService}
}

// CreateActivationCodeRequest 创建激活码请求。
type CreateActivationCodeRequest struct {
	Code      string  `json:"code"`                            // 可选，为空或批量时自动生成
	Amount    float64 `json:"amount" binding:"required,min=0"` // 绑定时充入的金额
	StartsAt  *int64  `json:"starts_at"`                       // 生效日期（秒级时间戳），空=立即生效
	ExpiresAt *int64  `json:"expires_at"`                      // 失效日期（秒级时间戳），空=永不过期
	Notes     string  `json:"notes"`                           // 备注
	Count     int     `json:"count" binding:"omitempty,min=1"` // 批量生成数量，默认 1
}

// UpdateActivationCodeRequest 更新激活码请求。
// starts_at / expires_at 传 0 表示清空该时间，字段缺省表示保持不变（与优惠码一致）。
type UpdateActivationCodeRequest struct {
	Amount    *float64 `json:"amount" binding:"omitempty,min=0"`
	Status    *string  `json:"status" binding:"omitempty,oneof=unused disabled"`
	StartsAt  *int64   `json:"starts_at"`
	ExpiresAt *int64   `json:"expires_at"`
	Notes     *string  `json:"notes"`
}

// List 分页列出激活码。
// GET /api/v1/admin/activation-codes
func (h *ActivationCodeHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.Query("status")
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 100 {
		search = search[:100]
	}

	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	codes, paginationResult, err := h.activationService.List(c.Request.Context(), params, status, search)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminActivationCode, 0, len(codes))
	for i := range codes {
		out = append(out, *dto.ActivationCodeFromServiceAdmin(&codes[i]))
	}
	response.Paginated(c, out, paginationResult.Total, page, pageSize)
}

// GetByID 获取单个激活码。
// GET /api/v1/admin/activation-codes/:id
func (h *ActivationCodeHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid activation code ID")
		return
	}

	code, err := h.activationService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ActivationCodeFromServiceAdmin(code))
}

// Create 创建激活码（支持批量）。
// POST /api/v1/admin/activation-codes
func (h *ActivationCodeHandler) Create(c *gin.Context) {
	var req CreateActivationCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	input := &service.CreateActivationCodeInput{
		Code:      req.Code,
		Amount:    req.Amount,
		StartsAt:  timeFromUnixPtr(req.StartsAt),
		ExpiresAt: timeFromUnixPtr(req.ExpiresAt),
		Notes:     req.Notes,
		Count:     req.Count,
	}

	codes, err := h.activationService.Create(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminActivationCode, 0, len(codes))
	for i := range codes {
		out = append(out, *dto.ActivationCodeFromServiceAdmin(&codes[i]))
	}
	response.Success(c, out)
}

// Update 更新激活码。
// PUT /api/v1/admin/activation-codes/:id
func (h *ActivationCodeHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid activation code ID")
		return
	}

	var req UpdateActivationCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	input := &service.UpdateActivationCodeInput{
		Amount: req.Amount,
		Status: req.Status,
		Notes:  req.Notes,
	}
	if req.StartsAt != nil {
		input.StartsAt = service.NullableTimeUpdate{Set: true, Value: timeFromUnixPtr(req.StartsAt)}
	}
	if req.ExpiresAt != nil {
		input.ExpiresAt = service.NullableTimeUpdate{Set: true, Value: timeFromUnixPtr(req.ExpiresAt)}
	}

	code, err := h.activationService.Update(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ActivationCodeFromServiceAdmin(code))
}

// Delete 删除激活码。
// DELETE /api/v1/admin/activation-codes/:id
func (h *ActivationCodeHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid activation code ID")
		return
	}

	if err := h.activationService.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Activation code deleted"})
}

// ListUserLoginIPs 查看某个用户已占用的登录 IP。
// GET /api/v1/admin/users/:id/login-ips
func (h *ActivationCodeHandler) ListUserLoginIPs(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	ips, err := h.activationService.ListLoginIPs(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := dto.UserLoginIPsFromService(ips)
	response.Success(c, gin.H{
		"login_ips":      out,
		"login_ip_used":  len(out),
		"login_ip_limit": domain.UserLoginIPLimit,
	})
}

// ResetUserLoginIPs 清空某个用户的登录 IP 记录，用于用户换网络后解封。
// DELETE /api/v1/admin/users/:id/login-ips
func (h *ActivationCodeHandler) ResetUserLoginIPs(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	removed, err := h.activationService.ResetLoginIPs(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"removed": removed})
}

// timeFromUnixPtr 把秒级时间戳指针转成 *time.Time，nil 或 <=0 返回 nil。
func timeFromUnixPtr(ts *int64) *time.Time {
	if ts == nil || *ts <= 0 {
		return nil
	}
	t := time.Unix(*ts, 0)
	return &t
}
