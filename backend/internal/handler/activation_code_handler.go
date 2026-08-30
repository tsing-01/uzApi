package handler

import (
	"github.com/uzapi/internal/handler/dto"
	"github.com/uzapi/internal/pkg/response"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"

	"github.com/gin-gonic/gin"
)

// ActivationCodeHandler 处理用户侧激活码请求。
type ActivationCodeHandler struct {
	activationService *service.ActivationCodeService
}

// NewActivationCodeHandler 创建用户侧激活码 handler。
func NewActivationCodeHandler(activationService *service.ActivationCodeService) *ActivationCodeHandler {
	return &ActivationCodeHandler{activationService: activationService}
}

// AddActivationCodeRequest 添加激活码请求体。
type AddActivationCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// AddActivationCodeResponse 添加激活码响应体。
type AddActivationCodeResponse struct {
	Code       *dto.ActivationCode `json:"code"`
	Amount     float64             `json:"amount"`
	NewBalance float64             `json:"new_balance"`
	// Replaced 表示本次绑定替换掉的历史激活码数量。
	Replaced int `json:"replaced"`
	// LoginIPReset 恒为 true：绑定新激活码会清空已占用的登录 IP 名额。
	LoginIPReset bool `json:"login_ip_reset"`
}

// Verify 校验当前用户的激活码是否有效（无入参，仅凭 token）。
// GET /api/v1/activation-code/verify
func (h *ActivationCodeHandler) Verify(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	status, err := h.activationService.GetStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ActivationStatusFromService(status))
}

// Add 为当前用户添加（绑定）激活码：校验通过后把激活码配置的金额充入当前账户余额，
// 并把该用户之前绑定的激活码替换掉。
// POST /api/v1/activation-code
func (h *ActivationCodeHandler) Add(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req AddActivationCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.activationService.Bind(c.Request.Context(), subject.UserID, req.Code)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, AddActivationCodeResponse{
		Code:         dto.ActivationCodeFromService(result.Code),
		Amount:       result.Code.Amount,
		NewBalance:   result.NewBalance,
		Replaced:     result.ReplacedCount,
		LoginIPReset: true,
	})
}
