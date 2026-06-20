package handler

import (
	"github.com/uzapi/internal/handler/dto"
	"github.com/uzapi/internal/pkg/pagination"
	"github.com/uzapi/internal/pkg/response"
	middleware2 "github.com/uzapi/internal/server/middleware"
	"github.com/uzapi/internal/service"

	"github.com/gin-gonic/gin"
)

// IntegrationHandler serves compact user-facing snapshots for external apps.
type IntegrationHandler struct {
	userService             *service.UserService
	apiKeyService           *service.APIKeyService
	availableChannelHandler *AvailableChannelHandler
}

func NewIntegrationHandler(
	userService *service.UserService,
	apiKeyService *service.APIKeyService,
	availableChannelHandler *AvailableChannelHandler,
) *IntegrationHandler {
	return &IntegrationHandler{
		userService:             userService,
		apiKeyService:           apiKeyService,
		availableChannelHandler: availableChannelHandler,
	}
}

type integrationMeResponse struct {
	User              userProfileResponse    `json:"user"`
	APIKeys           []dto.APIKey           `json:"api_keys"`
	APIKeysPagination integrationPagination  `json:"api_keys_pagination"`
	Groups            []dto.Group            `json:"groups"`
	GroupRates        map[int64]float64      `json:"group_rates"`
	Channels          []userAvailableChannel `json:"channels"`
}

type integrationPagination struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

// Me returns the current user's profile, keys, available groups, rates, and channel/model snapshot.
// GET /api/v1/integration/me
func (h *IntegrationHandler) Me(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	user, err := h.userService.GetProfile(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	identities, err := h.userService.GetProfileIdentitySummaries(c.Request.Context(), subject.UserID, user)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	page, pageSize := response.ParsePagination(c)
	keys, keyPagination, err := h.apiKeyService.List(c.Request.Context(), subject.UserID, pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("api_keys_sort_by", "created_at"),
		SortOrder: c.DefaultQuery("api_keys_sort_order", "desc"),
	}, service.APIKeyListFilters{})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	apiKeys := make([]dto.APIKey, 0, len(keys))
	for i := range keys {
		apiKeys = append(apiKeys, *dto.APIKeyFromService(&keys[i]))
	}

	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	groupDTOs := make([]dto.Group, 0, len(groups))
	for i := range groups {
		groupDTOs = append(groupDTOs, *dto.GroupFromService(&groups[i]))
	}

	groupRates, err := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if groupRates == nil {
		groupRates = map[int64]float64{}
	}

	channels, err := h.availableChannelHandler.listForUser(c, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	keyPages := 1
	keyTotal := int64(0)
	if keyPagination != nil {
		keyPages = keyPagination.Pages
		keyTotal = keyPagination.Total
	}
	response.Success(c, integrationMeResponse{
		User:    userProfileResponseFromService(user, identities),
		APIKeys: apiKeys,
		APIKeysPagination: integrationPagination{
			Total:    keyTotal,
			Page:     page,
			PageSize: pageSize,
			Pages:    keyPages,
		},
		Groups:     groupDTOs,
		GroupRates: groupRates,
		Channels:   channels,
	})
}
