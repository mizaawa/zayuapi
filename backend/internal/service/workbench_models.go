package service

import (
	"context"
	"sort"
	"strings"
)

// Pricing can declare media models on Custom accounts that accept arbitrary names.
func (s *GatewayService) GetWorkbenchPricedModels(ctx context.Context, group *Group, platform, kind string) []string {
	if s == nil || s.accountRepo == nil || group == nil || (kind != "image" && kind != "video") {
		return nil
	}
	if platform == PlatformComposite {
		platform = PlatformCustom
	}
	pricing := append([]ChannelModelPricing(nil), group.ModelPricing...)
	if s.channelService != nil {
		if channel, err := s.channelService.GetChannelForGroup(ctx, group.ID); err == nil && channel != nil {
			pricing = append(pricing, channel.ModelPricing...)
		}
	}
	candidates := map[string]struct{}{}
	for _, entry := range pricing {
		if entry.Platform != platform || string(entry.BillingMode) != kind {
			continue
		}
		for _, model := range entry.Models {
			model = strings.TrimSpace(model)
			if model != "" && !strings.ContainsAny(model, "*?") {
				candidates[model] = struct{}{}
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
	if err != nil {
		return nil
	}
	models := make([]string, 0, len(candidates))
	for model := range candidates {
		if override := group.GetModelPricing(model); override != nil && string(override.BillingMode) != kind {
			continue
		}
		routingModel := model
		if s.channelService != nil && platform != PlatformCustom {
			mapping, restricted := s.channelService.ResolveChannelMappingAndRestrict(ctx, &group.ID, model)
			if restricted {
				continue
			}
			if mapping.MappedModel != "" {
				routingModel = mapping.MappedModel
			}
		}
		for _, account := range accounts {
			if account.Platform != platform {
				continue
			}
			supported := account.IsModelSupported(routingModel)
			if platform == PlatformCustom {
				supported = account.Type == AccountTypeAPIKey &&
					strings.TrimSpace(account.GetOpenAIApiKey()) != "" && strings.TrimSpace(account.GetOpenAIBaseURL()) != ""
			}
			if supported {
				models = append(models, model)
				break
			}
		}
	}
	sort.Strings(models)
	return models
}
