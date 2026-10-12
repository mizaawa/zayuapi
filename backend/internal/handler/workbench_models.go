package handler

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) workbenchModels(c *gin.Context, group *service.Group, platform, kind string) {
	models := h.gatewayService.GetAvailableModels(c.Request.Context(), &group.ID, platform)
	if platform == service.PlatformComposite {
		models = h.compositeAvailableModels(c.Request.Context(), &group.ID)
	}
	if len(models) == 0 {
		models = defaultModelIDsForPlatform(platform)
	}
	priced := h.gatewayService.GetWorkbenchPricedModels(c.Request.Context(), group, platform, kind)
	models = mergeModelIDs(models, priced)
	if group.CustomModelsListEnabled() {
		models = filterModelsByCustomList(models, defaultModelIDsForPlatform(platform), group.ModelsListConfig.Models)
	}
	declarations := make(map[string]struct{}, len(priced))
	for _, model := range priced {
		declarations[model] = struct{}{}
	}
	items := make([]gin.H, 0, len(models))
	created := time.Now().Unix()
	for _, model := range models {
		item := gin.H{"id": model, "object": "model", "owned_by": platform, "created": created}
		if _, declared := declarations[model]; declared {
			item["media_type"] = kind
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": items})
}
