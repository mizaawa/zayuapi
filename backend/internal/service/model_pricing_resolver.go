package service

import (
	"context"
	"log/slog"
	"strings"
)

// PricingSource 定价来源标识
const (
	PricingSourceGroup    = "group"
	PricingSourceChannel  = "channel"
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片/视频模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片/视频模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice float64

	// 来源标识
	Source string // "group", "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool

	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
	channelPricing *ChannelModelPricing

	// catalogBase 表示 BasePricing 来自 LiteLLM 或内置兜底价卡。
	catalogBase bool
	// flatTokenPriced 表示自定义条目显式填写了至少一个 token 价（含显式 0）。
	// 没有价卡、条目也一个价都没填时，BasePricing 只是个全 0 的空壳，不能拿来计费。
	flatTokenPriced bool
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Group → Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model   string
	GroupID *int64 // nil 表示不检查渠道
	Group   *Group // authenticated billing group, including custom model prices
}

// Resolve 解析模型定价。
// Group pricing takes precedence over channel pricing and built-in prices.
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	var chPricing *ChannelModelPricing
	pricingSource := PricingSourceChannel
	if input.Group != nil && (input.GroupID == nil || *input.GroupID == input.Group.ID) {
		chPricing = input.Group.GetModelPricing(input.Model)
		if chPricing != nil {
			pricingSource = PricingSourceGroup
		}
	}
	if chPricing == nil && input.GroupID != nil && r.channelService != nil {
		chPricing = r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
	}
	if chPricing != nil {
		mode := chPricing.BillingMode
		if mode == "" {
			mode = BillingModeToken
		}
		if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
			resolved := &ResolvedPricing{
				Mode:           mode,
				Source:         pricingSource,
				channelPricing: chPricing,
			}
			r.applyRequestTierOverrides(chPricing, resolved)
			return resolved
		}
	}

	// 1. 获取基础定价
	basePricing, source := r.resolveBasePricing(input.Model)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
		catalogBase:            basePricing != nil,
	}

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = pricingSource
		resolved.channelPricing = chPricing
		r.applyTokenOverrides(chPricing, resolved)
	}

	return resolved
}

// GetModelPricing applies the same exact-before-prefix matching as channel pricing.
// Group prices belong to the billing group even when routing selects another platform.
func (g *Group) GetModelPricing(model string) *ChannelModelPricing {
	if g == nil {
		return nil
	}
	return matchPlatformModelPricing(g.ModelPricing, g.Platform, model)
}

func matchPlatformModelPricing(entries []ChannelModelPricing, platform, model string) *ChannelModelPricing {
	model = normalizeChannelPricingModelName(model)
	var wildcard *ChannelModelPricing
	for i := range entries {
		pricing := &entries[i]
		if pricing.Platform != "" && !isPlatformPricingMatch(platform, pricing.Platform) {
			continue
		}
		for _, pattern := range pricing.Models {
			pattern = normalizeChannelPricingModelName(pattern)
			if pattern == model {
				cloned := pricing.Clone()
				return &cloned
			}
			if wildcard == nil && strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
				wildcard = pricing
			}
		}
	}
	if wildcard != nil {
		cloned := wildcard.Clone()
		return &cloned
	}
	return nil
}

func (p *ResolvedPricing) hasCustomPricing() bool {
	return p != nil && (p.Source == PricingSourceGroup || p.Source == PricingSourceChannel)
}

// Billable reports whether the resolved pricing can turn usage into a charge.
// A custom entry without any price (and without a catalog price to fall back
// to) resolves to an all-zero card; billing it would silently record $0, so it
// counts as unpriced exactly like a model that no source knows.
func (p *ResolvedPricing) Billable() bool {
	if p == nil {
		return false
	}
	switch p.Mode {
	case BillingModePerRequest, BillingModeImage, BillingModeVideo:
		if p.hasDefaultPerRequestPrice() {
			return true
		}
		for _, tier := range p.RequestTiers {
			if tier.PerRequestPrice != nil {
				return true
			}
		}
		return false
	default:
		return p.basePriced() || len(p.Intervals) > 0
	}
}

// basePriced reports whether BasePricing is a real price card rather than the
// empty shell synthesized for a custom entry that only configured intervals.
func (p *ResolvedPricing) basePriced() bool {
	return p.catalogBase || p.flatTokenPriced || modelPricingHasTokenPrice(p.BasePricing)
}

func (p *ResolvedPricing) hasDefaultPerRequestPrice() bool {
	if p.channelPricing == nil {
		// Pre-resolved pricing built outside Resolve carries the price directly.
		return p.DefaultPerRequestPrice > 0
	}
	return p.channelPricing.PerRequestPrice != nil
}

func modelPricingHasTokenPrice(pricing *ModelPricing) bool {
	return pricing != nil && (pricing.InputPricePerToken > 0 || pricing.OutputPricePerToken > 0 ||
		pricing.CacheCreationPricePerToken > 0 || pricing.CacheReadPricePerToken > 0 ||
		pricing.ImageInputPricePerToken > 0 || pricing.ImageOutputPricePerToken > 0)
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
	pricing, err := r.billingService.GetModelPricing(model)
	if err != nil {
		slog.Debug("failed to get model pricing from LiteLLM, using fallback",
			"model", model, "error", err)
		return nil, PricingSourceFallback
	}
	return pricing, PricingSourceLiteLLM
}

// applyTokenOverrides 应用 token 模式的渠道覆盖。
// 条目上的单价就是"未命中区间时使用"的默认价，所以有区间时同样要叠加到 BasePricing 上。
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}
	pricing := resolved.BasePricing

	if chPricing.InputPrice != nil {
		pricing.InputPricePerToken = *chPricing.InputPrice
		pricing.InputPricePerTokenPriority = *chPricing.InputPrice
	}
	if chPricing.OutputPrice != nil {
		pricing.OutputPricePerToken = *chPricing.OutputPrice
		pricing.OutputPricePerTokenPriority = *chPricing.OutputPrice
	}
	if chPricing.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *chPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *chPricing.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *chPricing.CacheWritePrice
		pricing.CacheCreation1hPrice = *chPricing.CacheWritePrice
	}
	if chPricing.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *chPricing.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *chPricing.CacheReadPrice
	}
	applyChannelImageOutputPrice(chPricing, pricing)
	applyChannelImageInputPrice(chPricing, pricing)
	resolved.flatTokenPriced = chPricing.InputPrice != nil || chPricing.OutputPrice != nil ||
		chPricing.CacheWritePrice != nil || chPricing.CacheReadPrice != nil ||
		chPricing.ImageInputPrice != nil || chPricing.ImageOutputPrice != nil
	if !resolved.catalogBase {
		applyMissingCachePrices(pricing, chPricing.CacheReadPrice != nil, chPricing.CacheWritePrice != nil)
	}

	resolved.Intervals = filterTokenIntervals(chPricing.Intervals)
}

// applyChannelImageOutputPrice 应用渠道图片输出价：显式配置（含 0）按配置值计费；
// 未配置时清掉 LiteLLM 的图片价（避免价卡来源混用），并让 computeTokenBreakdown
// 回退到这张价卡自己的输出价——留空不能等于图片 token 免费。
func applyChannelImageOutputPrice(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && chPricing.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
		pricing.ImageOutputPriceExplicit = true
		return
	}
	pricing.ImageOutputPricePerToken = 0
	pricing.ImageOutputPriceExplicit = false
}

// applyChannelImageInputPrice 应用渠道图片输入价：显式配置则用配置值；
// 未配置时归零，使 computeTokenBreakdown 回退到文本输入价（向后兼容，
// 避免 commit 引入的 LiteLLM 图片输入价泄漏进渠道自定义定价）。
// 与 image_output 不同，此处不设 Explicit 标志——图片输入未配置应回退文本价，
// 而非硬置 0。
func applyChannelImageInputPrice(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && chPricing.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
	} else {
		pricing.ImageInputPricePerToken = 0
	}
}

// applyMissingCachePrices 让没有价卡兜底、自定义价也没填的缓存价按同一张价卡的输入价计费。
// 缓存读写 token 已从 input 中拆出，按 0 计会让高缓存命中的请求几乎免费。
func applyMissingCachePrices(pricing *ModelPricing, cacheReadSet, cacheWriteSet bool) {
	if !cacheReadSet {
		pricing.CacheReadPricePerToken = pricing.InputPricePerToken
		pricing.CacheReadPricePerTokenPriority = pricing.InputPricePerTokenPriority
	}
	if !cacheWriteSet {
		pricing.CacheCreationPricePerToken = pricing.InputPricePerToken
		pricing.CacheCreationPricePerTokenPriority = pricing.InputPricePerTokenPriority
		pricing.CacheCreation5mPrice = pricing.InputPricePerToken
		pricing.CacheCreation1hPrice = pricing.InputPricePerToken
	}
}

// applyRequestTierOverrides 应用按次/图片/视频模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(chPricing.Intervals)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// filterTokenIntervals keeps the intervals that carry at least one token price.
// An interval with only a per-request price means nothing in token mode and
// would otherwise price every matching request at $0.
func filterTokenIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 命中区间时用区间价（未填的字段继承默认价）；未命中时用默认价，
// 默认价也没有（条目只配了区间且模型没有价卡）时取最接近的区间，绝不落到全 0 的空价。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	iv := FindMatchingInterval(resolved.Intervals, totalContextTokens)
	if iv == nil {
		if resolved.basePriced() {
			return resolved.BasePricing
		}
		iv = nearestPricingInterval(resolved.Intervals, totalContextTokens)
	}

	return intervalToModelPricing(iv, resolved)
}

// nearestPricingInterval 返回上下文落在所有区间之外（或区间缝隙里）时最接近的区间：
// 起点低于上下文的区间里取起点最高的一个，都高于上下文时取起点最低的一个。
func nearestPricingInterval(intervals []PricingInterval, totalContextTokens int) *PricingInterval {
	var below, lowest *PricingInterval
	for i := range intervals {
		iv := &intervals[i]
		if lowest == nil || iv.MinTokens < lowest.MinTokens {
			lowest = iv
		}
		if iv.MinTokens < totalContextTokens && (below == nil || iv.MinTokens > below.MinTokens) {
			below = iv
		}
	}
	if below != nil {
		return below
	}
	return lowest
}

// intervalToModelPricing 将区间定价转换为 ModelPricing：以默认价为底，区间里填写的字段覆盖对应单价。
func intervalToModelPricing(iv *PricingInterval, resolved *ResolvedPricing) *ModelPricing {
	pricing := &ModelPricing{}
	if resolved.BasePricing != nil {
		cloned := *resolved.BasePricing
		pricing = &cloned
	}
	pricing.SupportsCacheBreakdown = resolved.SupportsCacheBreakdown
	if iv.InputPrice != nil {
		pricing.InputPricePerToken = *iv.InputPrice
		pricing.InputPricePerTokenPriority = *iv.InputPrice
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerToken = *iv.OutputPrice
		pricing.OutputPricePerTokenPriority = *iv.OutputPrice
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPrice = *iv.CacheWritePrice
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *iv.CacheReadPrice
	}
	// 渠道定价存在时，图片输出/输入价用渠道级配置（区间不携带图片价）。
	if chPricing := resolved.channelPricing; chPricing != nil {
		applyChannelImageOutputPrice(chPricing, pricing)
		applyChannelImageInputPrice(chPricing, pricing)
		if !resolved.catalogBase {
			applyMissingCachePrices(pricing,
				iv.CacheReadPrice != nil || chPricing.CacheReadPrice != nil,
				iv.CacheWritePrice != nil || chPricing.CacheWritePrice != nil)
		}
	}
	return pricing
}

// GetRequestTierPrice 根据层级标签获取按次价格（未命中返回 0）
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	price, _ := requestTierPriceByLabel(resolved.RequestTiers, tierLabel)
	return price
}

// GetRequestTierPriceByContext 根据 context token 数获取按次价格（未命中返回 0）
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	price, _ := requestTierPriceByContext(resolved.RequestTiers, totalContextTokens)
	return price
}

// requestTierPriceByLabel 按层级标签查按次价格。标签是管理员手填的自由文本，
// 而请求侧的尺寸/分辨率已归一化（1K、720p），所以忽略大小写和首尾空白。
func requestTierPriceByLabel(tiers []PricingInterval, tierLabel string) (float64, bool) {
	tierLabel = strings.TrimSpace(tierLabel)
	if tierLabel == "" {
		return 0, false
	}
	for _, tier := range tiers {
		if tier.PerRequestPrice != nil && strings.EqualFold(strings.TrimSpace(tier.TierLabel), tierLabel) {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

func requestTierPriceByContext(tiers []PricingInterval, totalContextTokens int) (float64, bool) {
	iv := FindMatchingInterval(tiers, totalContextTokens)
	if iv == nil || iv.PerRequestPrice == nil {
		return 0, false
	}
	return *iv.PerRequestPrice, true
}

// highestRequestTierPrice 是层级都没命中、又没有默认单价时的兜底：按配置里最贵的层级收费。
func highestRequestTierPrice(tiers []PricingInterval) (float64, bool) {
	price, ok := 0.0, false
	for _, tier := range tiers {
		if tier.PerRequestPrice != nil && (!ok || *tier.PerRequestPrice > price) {
			price, ok = *tier.PerRequestPrice, true
		}
	}
	return price, ok
}
