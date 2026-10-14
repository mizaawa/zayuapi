package service

import (
	"context"
	"sort"
	"strings"
)

// Video workbench catalogues are authorization-sensitive and must reflect
// account mapping edits immediately rather than the generic /v1/models cache.
func (s *GatewayService) GetWorkbenchVideoAccountModels(ctx context.Context, group *Group, platform string) []string {
	if s == nil || s.accountRepo == nil || group == nil {
		return nil
	}
	if platform == PlatformComposite {
		platform = PlatformCustom
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
	if err != nil {
		return nil
	}
	var channel *Channel
	if s.channelService != nil {
		channel, err = s.channelService.GetChannelForGroup(ctx, group.ID)
		if err != nil {
			return nil
		}
	}
	seen := make(map[string]struct{})
	for i := range accounts {
		if accounts[i].Platform != platform {
			continue
		}
		for model := range accounts[i].GetModelMapping() {
			model = strings.TrimSpace(model)
			if workbenchVideoSourcesAllowModel(group, channel, accounts, model) {
				seen[model] = struct{}{}
			}
		}
		if group.ModelsListConfig.Enabled {
			for _, selected := range group.ModelsListConfig.Models {
				selected = strings.TrimSpace(selected)
				if workbenchVideoSourcesAllowModel(group, channel, accounts, selected) && accounts[i].IsModelSupported(selected) {
					seen[selected] = struct{}{}
				}
			}
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// Pricing can declare media models on Custom accounts that accept arbitrary names.
func (s *GatewayService) GetWorkbenchPricedModels(ctx context.Context, group *Group, platform, kind string) []string {
	if s == nil || s.accountRepo == nil || group == nil || (kind != "image" && kind != "video") {
		return nil
	}
	if platform == PlatformComposite {
		platform = PlatformCustom
	}
	pricing := append([]ChannelModelPricing(nil), group.ModelPricing...)
	var channel *Channel
	if s.channelService != nil {
		var err error
		channel, err = s.channelService.GetChannelForGroup(ctx, group.ID)
		if err != nil && kind == "video" {
			return nil
		}
		if err == nil && channel != nil {
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
		if kind == "video" && !workbenchVideoSourcesAllowModel(group, channel, accounts, model) {
			continue
		}
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
				if kind == "video" && channel != nil && len(channel.GroupIDs) > 1 && !GroupHasWorkbenchModelPricing(group, group.Platform, "video") &&
					!group.ModelsListConfig.Enabled && len(account.GetModelMapping()) > 0 {
					supported = supported && account.IsModelSupported(model)
				}
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

// GroupHasWorkbenchModelPricing reports whether a group explicitly declares
// models for this workbench. Such declarations scope its video catalogue when
// multiple groups share a channel with a broader pricing list.
func GroupHasWorkbenchModelPricing(group *Group, platform, kind string) bool {
	if group == nil {
		return false
	}
	return hasWorkbenchModelPricing(group.ModelPricing, platform, kind)
}

func hasWorkbenchModelPricing(pricing []ChannelModelPricing, platform, kind string) bool {
	if platform == PlatformComposite {
		platform = PlatformCustom
	}
	for _, entry := range pricing {
		if entry.Platform == platform && string(entry.BillingMode) == kind {
			for _, model := range entry.Models {
				if strings.TrimSpace(model) != "" {
					return true
				}
			}
		}
	}
	return false
}

func GroupAllowsWorkbenchVideoModel(group *Group, model string) bool {
	if group == nil || !GroupHasWorkbenchModelPricing(group, group.Platform, "video") {
		return true
	}
	pricing := group.GetModelPricing(model)
	return pricing != nil && pricing.BillingMode == BillingModeVideo
}

// Both catalogues and task admission use the same model ownership rules.
func workbenchVideoSourcesAllowModel(group *Group, channel *Channel, accounts []Account, model string) bool {
	if group == nil || model == "" || strings.ContainsAny(model, "*?") || !GroupAllowsWorkbenchVideoModel(group, model) {
		return false
	}
	if group.ModelsListConfig.Enabled {
		selected := false
		for _, name := range group.ModelsListConfig.Models {
			if strings.TrimSpace(name) == model {
				selected = true
				break
			}
		}
		if !selected {
			return false
		}
	}
	if GroupHasWorkbenchModelPricing(group, group.Platform, "video") {
		return true
	}
	if channel != nil && hasWorkbenchModelPricing(channel.ModelPricing, group.Platform, "video") {
		pricing := matchPlatformModelPricing(channel.ModelPricing, group.Platform, model)
		if pricing == nil || pricing.BillingMode != BillingModeVideo {
			return false
		}
	}
	if group.ModelsListConfig.Enabled || channel == nil || len(channel.GroupIDs) < 2 {
		return true
	}
	// A shared channel or shared account cannot assign its whole catalogue to
	// every group. Only an exclusive account's exact key establishes ownership.
	for i := range accounts {
		account := &accounts[i]
		if account.Platform == PlatformCustom && len(account.GroupIDs) == 1 && account.GroupIDs[0] == group.ID {
			if _, ok := account.GetModelMapping()[model]; ok {
				return true
			}
		}
	}
	return false
}

func (s *OpenAIGatewayService) workbenchVideoModelAllowed(ctx context.Context, group *Group, model string) bool {
	if group == nil {
		return false
	}
	var channel *Channel
	if s.channelService != nil {
		var err error
		channel, err = s.channelService.GetChannelForGroup(ctx, group.ID)
		if err != nil {
			return false
		}
	}
	var accounts []Account
	if channel != nil && len(channel.GroupIDs) > 1 && !GroupHasWorkbenchModelPricing(group, group.Platform, "video") && !group.ModelsListConfig.Enabled {
		if s.accountRepo == nil {
			return false
		}
		var err error
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
		if err != nil {
			return false
		}
	}
	return workbenchVideoSourcesAllowModel(group, channel, accounts, strings.TrimSpace(model))
}
