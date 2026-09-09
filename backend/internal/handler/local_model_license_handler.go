package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/uzapi/internal/pkg/response"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"
)

type LocalModelLicenseHandler struct {
	licenses *service.LocalModelLicenseService
}

func NewLocalModelLicenseHandler(licenses *service.LocalModelLicenseService) *LocalModelLicenseHandler {
	return &LocalModelLicenseHandler{licenses: licenses}
}

func localLicenseSubject(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}
func bindLocalLicense(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(input); err != nil {
		response.BadRequest(c, "Invalid device licensing request")
		return false
	}
	return true
}
func (h *LocalModelLicenseHandler) Keys(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	keys, err := h.licenses.PublicKeys()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, keys)
}
func (h *LocalModelLicenseHandler) Challenge(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	var input service.LocalModelChallengeInput
	if !bindLocalLicense(c, &input) {
		return
	}
	out, err := h.licenses.Challenge(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *LocalModelLicenseHandler) Register(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	var input struct {
		service.LocalModelProof
		Name string `json:"name" binding:"required,max=100"`
	}
	if !bindLocalLicense(c, &input) {
		return
	}
	out, err := h.licenses.Register(c.Request.Context(), id, input.Name, input.LocalModelProof)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *LocalModelLicenseHandler) Issue(c *gin.Context) { h.issue(c, false) }
func (h *LocalModelLicenseHandler) Renew(c *gin.Context) { h.issue(c, true) }
func (h *LocalModelLicenseHandler) issue(c *gin.Context, renew bool) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	var input struct {
		service.LocalModelProof
		License string `json:"license" binding:"max=4096"`
	}
	if !bindLocalLicense(c, &input) {
		return
	}
	out, err := h.licenses.Issue(c.Request.Context(), id, input.LocalModelProof, input.License, renew)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *LocalModelLicenseHandler) Devices(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	devices, err := h.licenses.Devices(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, devices)
}
func (h *LocalModelLicenseHandler) RevokeDevice(c *gin.Context) {
	id, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	if err := h.licenses.RevokeDevice(c.Request.Context(), id, c.Param("device_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}
func (h *LocalModelLicenseHandler) RevokeEntitlement(c *gin.Context) {
	actorID, ok := localLicenseSubject(c)
	if !ok {
		return
	}
	role, ok := middleware2.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	var input struct {
		Reason string `json:"reason" binding:"required,max=500"`
	}
	if !bindLocalLicense(c, &input) {
		return
	}
	if err := h.licenses.RevokeEntitlement(c.Request.Context(), id, actorID, input.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}
