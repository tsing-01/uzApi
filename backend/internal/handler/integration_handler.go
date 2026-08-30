package handler

import (
	"sort"

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
	Balance           integrationBalance     `json:"balance"`
	APIKeys           []dto.APIKey           `json:"api_keys"`
	APIKeysPagination integrationPagination  `json:"api_keys_pagination"`
	Groups            []dto.Group            `json:"groups"`
	GroupRates        map[int64]float64      `json:"group_rates"`
	Channels          []userAvailableChannel `json:"channels"`
	Models            []integrationModel     `json:"models"`
}

// integrationBalance 是余额/并发的扁平快照，方便外部客户端直接渲染
// 「当前余额 + 并发数」而不必从 user 字段里挑。
type integrationBalance struct {
	Balance        float64 `json:"balance"`
	Concurrency    int     `json:"concurrency"`
	TotalRecharged float64 `json:"total_recharged"`
}

// integrationModel 是按 platform+name 去重后的扁平模型条目，
// 由 channels 聚合而来，供外部客户端做模型选择器使用。
type integrationModel struct {
	Name     string                     `json:"name"`
	Platform string                     `json:"platform"`
	Pricing  *userSupportedModelPricing `json:"pricing"`
	GroupIDs []int64                    `json:"group_ids"`
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
		User: userProfileResponseFromService(user, identities),
		Balance: integrationBalance{
			Balance:        user.Balance,
			Concurrency:    user.Concurrency,
			TotalRecharged: user.TotalRecharged,
		},
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
		Models:     flattenIntegrationModels(channels),
	})
}

// flattenIntegrationModels 把 channels 里嵌套的 supported_models 拍平成一份
// 按 platform+name 去重的模型列表，并合并每个模型可用的分组 ID。
// 定价取第一个非空的条目（同平台同模型在不同渠道下的用户可见定价一致）。
func flattenIntegrationModels(channels []userAvailableChannel) []integrationModel {
	models := make([]integrationModel, 0)
	indexByKey := make(map[string]int)
	groupSeen := make(map[string]map[int64]struct{})

	for _, ch := range channels {
		for _, section := range ch.Platforms {
			groupIDs := make([]int64, 0, len(section.Groups))
			for _, g := range section.Groups {
				groupIDs = append(groupIDs, g.ID)
			}
			for _, m := range section.SupportedModels {
				key := m.Platform + "\x00" + m.Name
				idx, ok := indexByKey[key]
				if !ok {
					models = append(models, integrationModel{
						Name:     m.Name,
						Platform: m.Platform,
						Pricing:  m.Pricing,
						GroupIDs: make([]int64, 0, len(groupIDs)),
					})
					idx = len(models) - 1
					indexByKey[key] = idx
					groupSeen[key] = make(map[int64]struct{}, len(groupIDs))
				}
				if models[idx].Pricing == nil {
					models[idx].Pricing = m.Pricing
				}
				for _, id := range groupIDs {
					if _, dup := groupSeen[key][id]; dup {
						continue
					}
					groupSeen[key][id] = struct{}{}
					models[idx].GroupIDs = append(models[idx].GroupIDs, id)
				}
			}
		}
	}

	for i := range models {
		sort.Slice(models[i].GroupIDs, func(a, b int) bool { return models[i].GroupIDs[a] < models[i].GroupIDs[b] })
	}
	sort.Slice(models, func(a, b int) bool {
		if models[a].Platform != models[b].Platform {
			return models[a].Platform < models[b].Platform
		}
		return models[a].Name < models[b].Name
	})
	return models
}
