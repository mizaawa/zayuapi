package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// 模型定价准入门（fail-closed）。
//
// 用量只能按已知价格计费：分组逐模型定价 → 渠道定价 → LiteLLM 价格表 → 内置兜底价。
// 一个模型在这些来源里都查不到价时，转发出去的请求只会被记成 $0——上游照样消耗，
// 用户一分不付（例如分组里有空映射账号放行了 grok-4.7-fast 这种未定价模型）。
// 因此 token 计费的文本入口在选号前装门：
//
//   - 请求模型或渠道映射后的模型能定价：不装门，调度与计费完全不变。
//   - 都不能定价：装门。候选账号必须把模型映射成一个能定价的上游模型才合格
//     （与记账时的候选模型回退同口径），一个都没有就拒绝请求，不再转发上游。
//
// 门随 ctx 进入各调度器，复用利润门的逐账号否决点（accountAdmissionVetoReason），
// 粘性会话、previous_response、抢槽后终检都覆盖到。handler 在选号失败时经
// ModelPricingGateBlocked 区分"没有价"与"暂时没有可用账号"，前者回 403，不计入容量受限，
// 也不触发 API Key failover。
//
// 豁免：Simple 模式（不计费）、渠道监控 key（用量本就免费）、媒体生成模型（按张/按秒计费，
// 有内置默认价或价格表里的每张价）。count_tokens、图片/视频端点、Live 不调用装门函数。

const modelPricingFilterReason = "model_pricing_unavailable"

type modelPricingGateCtxKey struct{}

type modelPricingGate struct {
	resolver *ModelPricingResolver
	group    *Group
	// model 是客户端请求的模型，用于错误提示；forwardModel 是渠道映射后真正交给账号映射的模型。
	model        string
	forwardModel string

	mu   sync.Mutex
	memo map[string]bool

	vetoed   atomic.Int64
	admitted atomic.Int64
}

// WithModelPricingGate 为 token 计费请求装配定价准入门，见文件头说明。
func (s *GatewayService) WithModelPricingGate(ctx context.Context, apiKey *APIKey, requestedModel string, mapping ChannelMappingResult) context.Context {
	if s == nil {
		return ctx
	}
	return withModelPricingGate(ctx, s.cfg, s.resolver, apiKey, requestedModel, mapping)
}

// WithModelPricingGate 为 token 计费请求装配定价准入门，见文件头说明。
func (s *OpenAIGatewayService) WithModelPricingGate(ctx context.Context, apiKey *APIKey, requestedModel string, mapping ChannelMappingResult) context.Context {
	if s == nil {
		return ctx
	}
	return withModelPricingGate(ctx, s.cfg, s.resolver, apiKey, requestedModel, mapping)
}

// ModelPricingBillableForAccount reports whether a request for requestedModel,
// served by account, can be billed. Long-lived connections use it for turns
// that switch models after the account was already selected.
func (s *OpenAIGatewayService) ModelPricingBillableForAccount(ctx context.Context, apiKey *APIKey, requestedModel string, mapping ChannelMappingResult, account *Account) bool {
	if s == nil {
		return true
	}
	gateCtx := withModelPricingGate(ctx, s.cfg, s.resolver, apiKey, requestedModel, mapping)
	vetoed, _ := modelPricingGateVetoReason(gateCtx, account)
	return !vetoed
}

func withModelPricingGate(ctx context.Context, cfg *config.Config, resolver *ModelPricingResolver, apiKey *APIKey, requestedModel string, mapping ChannelMappingResult) context.Context {
	if ctx == nil {
		return ctx
	}
	// 一个请求可能换 key / 换兜底分组 / 换 turn 重新装门，先清掉旧门。
	if existing, _ := ctx.Value(modelPricingGateCtxKey{}).(*modelPricingGate); existing != nil {
		ctx = context.WithValue(ctx, modelPricingGateCtxKey{}, (*modelPricingGate)(nil))
	}
	if resolver == nil || resolver.billingService == nil || apiKey == nil || apiKey.IsManaged() {
		return ctx
	}
	if cfg != nil && cfg.RunMode == config.RunModeSimple {
		return ctx
	}
	requested := strings.TrimSpace(requestedModel)
	if requested == "" {
		return ctx
	}
	forward := requested
	if mapped := strings.TrimSpace(mapping.MappedModel); mapping.Mapped && mapped != "" {
		forward = mapped
	}
	gate := &modelPricingGate{
		resolver:     resolver,
		group:        apiKey.Group,
		model:        requested,
		forwardModel: forward,
		memo:         make(map[string]bool, 4),
	}
	if gate.billable(ctx, requested) || gate.billable(ctx, forward) {
		return ctx
	}
	return context.WithValue(ctx, modelPricingGateCtxKey{}, gate)
}

// billable 与记账同口径判断一个模型能否定价，按模型名缓存结果。
func (g *modelPricingGate) billable(ctx context.Context, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	key := strings.ToLower(model)
	g.mu.Lock()
	priced, ok := g.memo[key]
	g.mu.Unlock()
	if ok {
		return priced
	}
	priced = isMediaBilledModel(model) || g.resolver.billingService.hasCatalogImagePrice(model) ||
		g.resolver.Resolve(billingPricingContext(ctx), PricingInput{
			Model:   model,
			GroupID: g.groupID(),
			Group:   g.group,
		}).Billable()
	g.mu.Lock()
	g.memo[key] = priced
	g.mu.Unlock()
	return priced
}

func (g *modelPricingGate) groupID() *int64 {
	if g.group == nil || g.group.ID <= 0 {
		return nil
	}
	id := g.group.ID
	return &id
}

// accountBillable 判断账号把请求映射后的上游模型能否定价（composite 路由出的具体模型同样算）。
func (g *modelPricingGate) accountBillable(ctx context.Context, account *Account) bool {
	models := []string{g.forwardModel}
	if routed, ok := ResolvedUpstreamModelFromContext(ctx); ok && !strings.EqualFold(routed, g.forwardModel) {
		models = append(models, routed)
	}
	for _, model := range models {
		if g.billable(ctx, model) {
			return true
		}
		if upstream := accountUpstreamBillingModel(account, model); upstream != "" && !strings.EqualFold(upstream, model) && g.billable(ctx, upstream) {
			return true
		}
	}
	return false
}

// accountUpstreamBillingModel 返回账号转发时实际使用、记账时会作为候选的上游模型。
func accountUpstreamBillingModel(account *Account, model string) string {
	if account == nil {
		return ""
	}
	if account.Platform == PlatformAntigravity {
		return mapAntigravityModel(account, model)
	}
	return account.GetMappedModel(model)
}

// billingPricingContext 去掉强制平台标记：异步记账的 ctx 不带 ForcePlatform，
// 渠道价按分组平台查，准入判断必须与之同口径。
func billingPricingContext(ctx context.Context) context.Context {
	if forced, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && strings.TrimSpace(forced) != "" {
		return context.WithValue(ctx, ctxkey.ForcePlatform, "")
	}
	return ctx
}

// isMediaBilledModel 识别按张/按秒计费且有内置默认价的媒体生成模型。
func isMediaBilledModel(model string) bool {
	if isOpenAIImageGenerationModel(model) || isImageGenerationModel(model) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "grok-imagine-video")
}

func modelPricingGateVetoReason(ctx context.Context, account *Account) (bool, string) {
	if ctx == nil || account == nil {
		return false, ""
	}
	gate, _ := ctx.Value(modelPricingGateCtxKey{}).(*modelPricingGate)
	if gate == nil {
		return false, ""
	}
	if gate.accountBillable(ctx, account) {
		gate.admitted.Add(1)
		return false, ""
	}
	gate.vetoed.Add(1)
	return true, modelPricingFilterReason
}

// accountAdmissionVetoReason 汇总每个候选账号都要过的请求级准入门：先定价门，再利润门。
// 调度候选过滤、粘性复用、previous_response 绑定与抢槽后终检共用这一个否决点。
func accountAdmissionVetoReason(ctx context.Context, account *Account) (bool, string) {
	if vetoed, reason := modelPricingGateVetoReason(ctx, account); vetoed {
		return true, reason
	}
	return openAIProfitControlVetoReason(ctx, account)
}

// ModelPricingGateBlocked 报告定价门是否否决了本请求评估过的全部账号；
// 是则返回客户端请求的模型名，供 handler 回"未配置价格"而不是"暂时无可用账号"。
func ModelPricingGateBlocked(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	gate, _ := ctx.Value(modelPricingGateCtxKey{}).(*modelPricingGate)
	if gate == nil {
		return "", false
	}
	return gate.model, gate.vetoed.Load() > 0 && gate.admitted.Load() == 0
}

// ModelPricingUnavailableMessage 是定价门拒绝请求时返回给客户端的说明。
func ModelPricingUnavailableMessage(model string) string {
	return fmt.Sprintf("Model %q has no price configured in this group, so the request was not sent upstream. Ask the administrator to set a price for it.", strings.TrimSpace(model))
}
