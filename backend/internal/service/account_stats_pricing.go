package service

import (
	"context"
	"strings"
)

// Account statistics ("上游消耗" on the operations panel) estimate what the
// upstream charged for a request. They never read what the customer pays:
// group or channel selling prices, rate multipliers and customer-side usage
// rewrites stay out of them. The panel multiplies the value by the account's
// billing rate multiplier (the upstream's own ratio).
//
// resolveAccountStatsCostWithUnits 计算账号统计定价费用。
// 返回 nil 表示不覆盖，使用默认公式（total_cost × account_rate_multiplier）。
//
// 优先级（先命中为准）：
//  1. 渠道自定义账号统计规则（始终尝试，不依赖 ApplyPricingToAccountStats 开关）
//  2. ApplyPricingToAccountStats 启用时，按渠道自己为上游模型配置的价格计算；
//     分组管理里的单独定价属于客户售价，不参与（渠道未给该模型定价时继续往下）
//  3. 模型定价文件（LiteLLM / 内置兜底）中上游模型的标准价格，与客户计费同一套
//     token 拆分（图片 token、服务档位、5m/1h 缓存、长上下文）；不论分组是否绑定渠道
//  4. nil → 走默认公式（只剩价格表也算不出的未知模型）
//
// upstreamModel 是最终发往上游的模型 ID；tokens 是上游回报的原始用量。
type accountStatsRequestUnits struct {
	requestCount    int
	imageCount      int
	imageSize       string
	videoCount      int
	videoSeconds    int
	videoResolution string
}

// accountStatsPricingOptions carries upstream-side pricing dimensions that do
// not change the customer's price but do change what the upstream bills.
type accountStatsPricingOptions struct {
	// serviceTier is the tier the request was billed at (priority, flex, ...).
	serviceTier string
	// longContextBilling mirrors the account's long-context billing policy.
	longContextBilling bool
	// fallbackModels are priced, in order, when the catalog has no entry for
	// the upstream model (for example the billing or requested model).
	fallbackModels []string
	// imageModel prices the image tokens a hosted image tool (Responses
	// image_generation) reported next to a different text model.
	imageModel string
	// webSearchCalls are hosted web searches billed per call; they report no
	// tokens of their own.
	webSearchCalls int
}

// catalogAbove200kContextThreshold is the prompt size (input + cache tokens)
// above which Gemini Pro and Claude 1M-context price a whole request at their
// above-200k tier.
const catalogAbove200kContextThreshold = 200000

func defaultAccountStatsPricingOptions() accountStatsPricingOptions {
	return accountStatsPricingOptions{longContextBilling: true}
}

func resolveAccountStatsCostWithUnits(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	accountID int64,
	groupID int64,
	upstreamModel string,
	tokens UsageTokens,
	units accountStatsRequestUnits,
	opts accountStatsPricingOptions,
) *float64 {
	if !validUsageTokens(tokens) {
		// An invalid usage snapshot must not fall through to the historical
		// total_cost fallback, which could otherwise make account statistics
		// charge an unvalidated amount. A non-nil zero explicitly suppresses
		// that fallback for this row.
		zero := 0.0
		return &zero
	}
	upstreamModel = strings.TrimSpace(upstreamModel)

	if channelService != nil && upstreamModel != "" && groupID > 0 {
		if channel, err := channelService.GetChannelForGroup(ctx, groupID); err == nil && channel != nil {
			platform := channelService.GetGroupPlatform(ctx, groupID)

			// 优先级 1：自定义规则（始终尝试）
			if cost := tryCustomRulesWithUnits(channel, accountID, groupID, platform, upstreamModel, tokens, units); cost != nil {
				return cost
			}

			// 优先级 2：渠道自己的定价（不含分组单独定价与任何倍率）
			if channel.ApplyPricingToAccountStats {
				if cost := channelOwnStatsCost(ctx, channelService, billingService, groupID, upstreamModel, tokens, units, opts); cost != nil {
					return cost
				}
			}
		}
	}

	// 优先级 3：价格表中上游实际计费的标准价
	if billingService != nil {
		return upstreamCatalogStatsCost(billingService, upstreamModel, tokens, units, opts)
	}
	return nil
}

// channelOwnStatsCost prices the request with the channel's entry for the
// upstream model only. The resolver is given no group, so group-level selling
// prices (分组管理) cannot leak in; a model the channel does not price yields
// nil and falls through to the catalog.
func channelOwnStatsCost(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	groupID int64,
	model string,
	tokens UsageTokens,
	units accountStatsRequestUnits,
	opts accountStatsPricingOptions,
) *float64 {
	if billingService == nil {
		entry := channelService.GetChannelModelPricing(ctx, groupID, model)
		if entry == nil {
			return nil
		}
		return calculateStatsCostWithUnits(entry, tokens, units)
	}
	resolver := NewModelPricingResolver(channelService, billingService)
	gid := groupID
	resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &gid})
	if resolved == nil || resolved.Source != PricingSourceChannel {
		return nil
	}
	requestCount, sizeTier := accountStatsUnitsForMode(resolved.Mode, units)
	longContextBilling := opts.longContextBilling
	cost, err := billingService.CalculateCostUnified(CostInput{
		Ctx:                       ctx,
		Model:                     model,
		GroupID:                   &gid,
		Tokens:                    tokens,
		RequestCount:              requestCount,
		SizeTier:                  sizeTier,
		RateMultiplier:            1,
		ServiceTier:               opts.serviceTier,
		Resolver:                  resolver,
		Resolved:                  resolved,
		LongContextBillingEnabled: &longContextBilling,
	})
	return positiveStatsCost(cost, err)
}

// accountStatsUnitsForMode picks the billable unit count and tier label the
// same way calculateStatsCostWithUnits does for each billing mode.
func accountStatsUnitsForMode(mode BillingMode, units accountStatsRequestUnits) (int, string) {
	switch mode {
	case BillingModeVideo:
		count := units.videoSeconds
		if count <= 0 {
			count = units.videoCount
		}
		if count <= 0 {
			count = units.requestCount
		}
		return count, units.videoResolution
	case BillingModePerRequest:
		if units.videoCount > 0 {
			return units.videoCount, units.videoResolution
		}
		return units.requestCount, units.imageSize
	case BillingModeImage:
		return units.requestCount, units.imageSize
	default:
		return units.requestCount, ""
	}
}

func positiveStatsCost(cost *CostBreakdown, err error) *float64 {
	if err != nil || cost == nil || cost.TotalCost <= 0 {
		return nil
	}
	value := cost.TotalCost
	return &value
}

// upstreamCatalogStatsCost prices what the upstream reported at the catalog's
// standard rates. Token usage goes through the same breakdown as customer
// billing (image tokens are a subset of input/output tokens, service tiers,
// 5m/1h cache writes, long context). Media that reported no tokens use the
// catalog's real per-image or per-second price, never a site default.
func upstreamCatalogStatsCost(
	billingService *BillingService,
	upstreamModel string,
	tokens UsageTokens,
	units accountStatsRequestUnits,
	opts accountStatsPricingOptions,
) *float64 {
	models := accountStatsCandidateModels(upstreamModel, opts.fallbackModels)
	if hasAccountStatsTokens(tokens) {
		if cost := hostedImageToolStatsCost(billingService, models, tokens, opts); cost != nil {
			return cost
		}
		for _, model := range models {
			if cost := catalogTokenStatsCost(billingService, model, tokens, opts); cost != nil {
				return cost
			}
		}
		return nil
	}
	if opts.webSearchCalls > 0 && validBillableRequestCount(opts.webSearchCalls) {
		// Hosted web search reports no tokens; the default per-call price is
		// the official one ($10 per 1,000 calls).
		cost := defaultWebSearchPricePerCall * float64(opts.webSearchCalls)
		return &cost
	}
	for _, model := range models {
		if cost := catalogMediaStatsCost(billingService, model, units); cost != nil {
			return cost
		}
	}
	return nil
}

func accountStatsCandidateModels(upstreamModel string, fallbacks []string) []string {
	models := make([]string, 0, 1+len(fallbacks))
	seen := make(map[string]struct{}, 1+len(fallbacks))
	for _, model := range append([]string{upstreamModel}, fallbacks...) {
		model = strings.TrimSpace(model)
		key := strings.ToLower(model)
		if model == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		models = append(models, model)
	}
	return models
}

func hasAccountStatsTokens(tokens UsageTokens) bool {
	return tokens.InputTokens > 0 || tokens.OutputTokens > 0 || tokens.CacheCreationTokens > 0 ||
		tokens.CacheReadTokens > 0 || tokens.CacheCreation5mTokens > 0 || tokens.CacheCreation1hTokens > 0 ||
		tokens.ImageOutputTokens > 0 || tokens.ImageInputTokens > 0
}

func catalogTokenStatsCost(billingService *BillingService, model string, tokens UsageTokens, opts accountStatsPricingOptions) *float64 {
	if cost := catalogAbove200kStatsCost(billingService, model, tokens, opts); cost != nil {
		return cost
	}
	return positiveStatsCost(billingService.calculateCostInternalWithPolicy(
		model, tokens, 1.0, opts.serviceTier, nil, opts.longContextBilling,
	))
}

// catalogAbove200kStatsCost prices a prompt above 200k tokens at the catalog's
// above-200k tier for the whole request, the way Google (Gemini Pro) and
// Anthropic (1M context) bill it. These catalog prices feed statistics only;
// customer billing keeps its own long-context rule. Models that carry an
// explicit long-context policy, and priority-tier requests, use the regular
// breakdown instead.
func catalogAbove200kStatsCost(billingService *BillingService, model string, tokens UsageTokens, opts accountStatsPricingOptions) *float64 {
	if billingService == nil || billingService.pricingService == nil ||
		normalizeBillingServiceTier(opts.serviceTier) == "priority" {
		return nil
	}
	contextTokens, ok := sumNonNegativeInts(tokens.InputTokens, tokens.CacheCreationTokens, tokens.CacheReadTokens)
	if !ok || contextTokens <= catalogAbove200kContextThreshold {
		return nil
	}
	entry := billingService.pricingService.GetModelPricing(model)
	if entry == nil || entry.InputCostPerTokenAbove200k <= 0 {
		return nil
	}
	base, err := billingService.GetModelPricing(model)
	if err != nil || base == nil || base.LongContextInputThreshold > 0 {
		return nil
	}
	tiered := *base
	tiered.InputPricePerToken = entry.InputCostPerTokenAbove200k
	if entry.OutputCostPerTokenAbove200k > 0 {
		tiered.OutputPricePerToken = entry.OutputCostPerTokenAbove200k
	}
	if entry.CacheReadInputTokenCostAbove200k > 0 {
		tiered.CacheReadPricePerToken = entry.CacheReadInputTokenCostAbove200k
	}
	if above := entry.CacheCreationInputTokenCostAbove200k; above > 0 {
		if base.CacheCreationPricePerToken > 0 {
			// Scale the 5m/1h cache write prices by the same tier step.
			ratio := above / base.CacheCreationPricePerToken
			tiered.CacheCreation5mPrice *= ratio
			tiered.CacheCreation1hPrice *= ratio
		}
		tiered.CacheCreationPricePerToken = above
	}
	return positiveStatsCost(billingService.computeTokenBreakdown(&tiered, tokens, 1.0, opts.serviceTier, false), nil)
}

// hostedImageToolStatsCost splits a Responses request whose hosted
// image_generation tool reported image tokens next to the text model's usage:
// the upstream bills the text tokens at the text model's rates and the image
// tokens at the image model's rates. It returns nil when there is nothing to
// split or either side has no catalog price.
func hostedImageToolStatsCost(billingService *BillingService, models []string, tokens UsageTokens, opts accountStatsPricingOptions) *float64 {
	imageModel := strings.TrimSpace(opts.imageModel)
	if imageModel == "" || len(models) == 0 || strings.EqualFold(imageModel, models[0]) {
		return nil
	}
	if tokens.ImageOutputTokens <= 0 && tokens.ImageInputTokens <= 0 {
		return nil
	}
	// The tool's image tokens come from tool_usage.image_gen and are not part
	// of the text model's input/output counts.
	textTokens := tokens
	textTokens.ImageInputTokens = 0
	textTokens.ImageOutputTokens = 0
	imageTokens := UsageTokens{
		InputTokens:       tokens.ImageInputTokens,
		ImageInputTokens:  tokens.ImageInputTokens,
		OutputTokens:      tokens.ImageOutputTokens,
		ImageOutputTokens: tokens.ImageOutputTokens,
	}
	imageCost := catalogTokenStatsCost(billingService, imageModel, imageTokens, opts)
	if imageCost == nil {
		return nil
	}
	total := *imageCost
	if hasAccountStatsTokens(textTokens) {
		var textCost *float64
		for _, model := range models {
			if textCost = catalogTokenStatsCost(billingService, model, textTokens, opts); textCost != nil {
				break
			}
		}
		if textCost == nil {
			return nil
		}
		total += *textCost
	}
	return &total
}

// catalogMediaStatsCost prices media that reported no tokens with the
// catalog's real unit price (per image, or per generated second for video).
func catalogMediaStatsCost(billingService *BillingService, model string, units accountStatsRequestUnits) *float64 {
	if units.videoCount > 0 {
		price, ok := getDefaultGrokImagineVideoPrice(model, units.videoResolution)
		if !ok || price <= 0 {
			return nil
		}
		seconds := units.videoSeconds
		if seconds <= 0 {
			seconds = units.videoCount
		}
		if !validBillableRequestCount(seconds) {
			return nil
		}
		cost := price * float64(seconds)
		return &cost
	}
	if units.imageCount <= 0 || !validBillableRequestCount(units.imageCount) {
		return nil
	}
	price, ok := getDefaultGrokImagineImagePrice(model, units.imageSize)
	if !ok && billingService.pricingService != nil {
		if pricing := billingService.pricingService.GetModelPricing(model); pricing != nil && pricing.OutputCostPerImage > 0 {
			price, ok = pricing.OutputCostPerImage, true
		}
	}
	if !ok || price <= 0 {
		return nil
	}
	cost := price * float64(units.imageCount)
	return &cost
}

// tryCustomRulesWithUnits 遍历自定义规则，按数组顺序先命中为准。
func tryCustomRulesWithUnits(
	channel *Channel, accountID, groupID int64,
	platform, model string, tokens UsageTokens, units accountStatsRequestUnits,
) *float64 {
	modelLower := strings.ToLower(model)
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		pricing := findPricingForModel(rule.Pricing, platform, modelLower)
		if pricing == nil {
			continue // 规则匹配但模型不在规则定价中，继续下一条
		}
		return calculateStatsCostWithUnits(pricing, tokens, units)
	}
	return nil
}

// matchAccountStatsRule 检查规则是否匹配指定的 accountID 和 groupID。
// 匹配条件：accountID ∈ rule.AccountIDs 或 groupID ∈ rule.GroupIDs。
// 如果规则的 AccountIDs 和 GroupIDs 都为空，视为不匹配。
func matchAccountStatsRule(rule *AccountStatsPricingRule, accountID, groupID int64) bool {
	if len(rule.AccountIDs) == 0 && len(rule.GroupIDs) == 0 {
		return false
	}
	for _, id := range rule.AccountIDs {
		if id == accountID {
			return true
		}
	}
	for _, id := range rule.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

// findPricingForModel 在定价列表中查找匹配的模型定价。
// 先精确匹配，再通配符匹配（按配置顺序，先匹配先使用）。
func findPricingForModel(pricingList []ChannelModelPricing, platform, modelLower string) *ChannelModelPricing {
	// 精确匹配优先
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			if strings.ToLower(m) == modelLower {
				return p
			}
		}
	}
	// 通配符匹配：按配置顺序，先匹配先使用
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			ml := strings.ToLower(m)
			if !strings.HasSuffix(ml, "*") {
				continue
			}
			prefix := strings.TrimSuffix(ml, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return p
			}
		}
	}
	return nil
}

// isPlatformMatch 判断平台是否匹配（空平台视为不限平台）。
func isPlatformMatch(queryPlatform, pricingPlatform string) bool {
	queryPlatform = normalizeAccountStatsPricingPlatform(queryPlatform)
	pricingPlatform = normalizeAccountStatsPricingPlatform(pricingPlatform)
	if queryPlatform == "" || pricingPlatform == "" {
		return true
	}
	return queryPlatform == pricingPlatform
}

func normalizeAccountStatsPricingPlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == PlatformComposite {
		return PlatformCustom
	}
	return platform
}

// calculateStatsCostWithUnits 使用给定的定价计算费用（不含任何倍率，原始费用）。
func calculateStatsCostWithUnits(pricing *ChannelModelPricing, tokens UsageTokens, units accountStatsRequestUnits) *float64 {
	if pricing == nil || !validUsageTokens(tokens) {
		return nil
	}
	switch pricing.BillingMode {
	case BillingModeVideo:
		requestCount := units.videoSeconds
		if requestCount <= 0 {
			requestCount = units.videoCount
		}
		if requestCount <= 0 {
			requestCount = units.requestCount
		}
		return calculateTieredPerRequestStatsCost(pricing, units.videoResolution, requestCount)
	case BillingModePerRequest, BillingModeImage:
		if pricing.BillingMode == BillingModePerRequest && units.videoCount > 0 {
			return calculateTieredPerRequestStatsCost(pricing, units.videoResolution, units.videoCount)
		}
		// Image rules may price only 1K/2K/4K tiers; pick the generated size.
		return calculateTieredPerRequestStatsCost(pricing, units.imageSize, units.requestCount)
	default:
		return calculateTokenStatsCost(pricing, tokens)
	}
}

func calculateTieredPerRequestStatsCost(pricing *ChannelModelPricing, tierLabel string, requestCount int) *float64 {
	if pricing == nil {
		return nil
	}
	tierLabel = strings.TrimSpace(tierLabel)
	if tierLabel != "" {
		for i := range pricing.Intervals {
			interval := &pricing.Intervals[i]
			if strings.EqualFold(strings.TrimSpace(interval.TierLabel), tierLabel) {
				return calculatePerRequestStatsCost(&ChannelModelPricing{PerRequestPrice: interval.PerRequestPrice}, requestCount)
			}
		}
	}
	return calculatePerRequestStatsCost(pricing, requestCount)
}

// calculatePerRequestStatsCost 按次/图片计费。
func calculatePerRequestStatsCost(pricing *ChannelModelPricing, requestCount int) *float64 {
	if pricing == nil || pricing.PerRequestPrice == nil || *pricing.PerRequestPrice <= 0 {
		return nil
	}
	if requestCount <= 0 {
		requestCount = 1
	}
	if !validBillableRequestCount(requestCount) {
		return nil
	}
	cost := *pricing.PerRequestPrice * float64(requestCount)
	return &cost
}

// calculateTokenStatsCost Token 计费，口径与客户计费的 computeTokenBreakdown 一致：
//   - 图片输出 token 是输出 token 的子集，文本部分按 OutputPrice、图片部分按
//     ImageOutputPrice（未配置时回退 OutputPrice），不重复计算；
//   - 图片输入 token 同理从输入里拆出，按 ImageInputPrice（未配置回退 InputPrice）；
//   - 区间按上下文 token（input + cache_creation + cache_read）匹配，与计费相同；
//     命中区间后只覆盖区间填写的价格，其余（含图片单价）沿用条目价。
func calculateTokenStatsCost(pricing *ChannelModelPricing, tokens UsageTokens) *float64 {
	if pricing == nil || !validUsageTokens(tokens) {
		return nil
	}
	p := pricing
	if len(pricing.Intervals) > 0 {
		totalContext, ok := sumNonNegativeInts(
			tokens.InputTokens,
			tokens.CacheCreationTokens,
			tokens.CacheReadTokens,
		)
		if !ok {
			return nil
		}
		if iv := FindMatchingInterval(pricing.Intervals, totalContext); iv != nil {
			// The interval overrides only the prices it sets; the rest, image
			// prices included, come from the entry (as intervalToModelPricing does).
			tier := *pricing
			tier.Intervals = nil
			if iv.InputPrice != nil {
				tier.InputPrice = iv.InputPrice
			}
			if iv.OutputPrice != nil {
				tier.OutputPrice = iv.OutputPrice
			}
			if iv.CacheWritePrice != nil {
				tier.CacheWritePrice = iv.CacheWritePrice
			}
			if iv.CacheReadPrice != nil {
				tier.CacheReadPrice = iv.CacheReadPrice
			}
			if iv.PerRequestPrice != nil {
				tier.PerRequestPrice = iv.PerRequestPrice
			}
			p = &tier
		}
	}
	deref := func(ptr *float64) float64 {
		if ptr == nil {
			return 0
		}
		return *ptr
	}
	orFallback := func(ptr, fallback *float64) float64 {
		if ptr != nil {
			return *ptr
		}
		return deref(fallback)
	}

	imageInputTokens := tokens.ImageInputTokens
	textInputTokens := tokens.InputTokens - imageInputTokens
	if textInputTokens < 0 {
		textInputTokens = 0
		imageInputTokens = tokens.InputTokens
	}
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	if textOutputTokens < 0 {
		textOutputTokens = 0
	}

	cost := float64(textInputTokens)*deref(p.InputPrice) +
		float64(imageInputTokens)*orFallback(p.ImageInputPrice, p.InputPrice) +
		float64(textOutputTokens)*deref(p.OutputPrice) +
		float64(tokens.ImageOutputTokens)*orFallback(p.ImageOutputPrice, p.OutputPrice) +
		float64(tokens.CacheCreationTokens)*deref(p.CacheWritePrice) +
		float64(tokens.CacheReadTokens)*deref(p.CacheReadPrice)
	if cost <= 0 {
		return nil
	}
	return &cost
}

// applyAccountStatsCost resolves the account stats cost for a usage log entry.
// tokens must be the upstream's own report, before any customer-side rewrite
// (forced cache billing, cache TTL overrides). groupID 0 means the key has no
// group: only the catalog step can price it.
func applyAccountStatsCost(
	ctx context.Context,
	usageLog *UsageLog,
	cs *ChannelService, bs *BillingService,
	accountID int64, groupID int64,
	upstreamModel, requestedModel string,
	tokens UsageTokens,
	opts accountStatsPricingOptions,
) {
	if usageLog == nil {
		return
	}
	model := strings.TrimSpace(upstreamModel)
	if model == "" {
		model = requestedModel
	}
	opts.fallbackModels = append(append([]string(nil), opts.fallbackModels...), requestedModel)
	requestCount := 1
	if usageLog.ImageCount > 0 {
		requestCount = usageLog.ImageCount
	}
	units := accountStatsRequestUnits{requestCount: requestCount, imageCount: usageLog.ImageCount}
	if usageLog.ImageSize != nil {
		units.imageSize = strings.TrimSpace(*usageLog.ImageSize)
	}
	if usageLog.VideoCount > 0 {
		units.requestCount = usageLog.VideoCount
		units.videoCount = usageLog.VideoCount
		if usageLog.VideoResolution != nil {
			units.videoResolution = *usageLog.VideoResolution
		}
		if usageLog.VideoDurationSeconds != nil && *usageLog.VideoDurationSeconds > 0 &&
			usageLog.VideoCount <= maxBillableRequestCount/(*usageLog.VideoDurationSeconds) {
			units.videoSeconds = usageLog.VideoCount * *usageLog.VideoDurationSeconds
		}
	}
	usageLog.AccountStatsCost = resolveAccountStatsCostWithUnits(
		ctx, cs, bs, accountID, groupID, model, tokens, units, opts,
	)
}
