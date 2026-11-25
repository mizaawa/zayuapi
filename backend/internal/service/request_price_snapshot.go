package service

import "context"

type requestPriceKey struct{}

// Both published tables are immutable. Pin the references, not a copy of the
// catalog per request. Routing and restriction checks still use live data.
type requestPriceSnapshot struct {
	resolver *ModelPricingResolver
	channels *channelCache
	catalog  *BillingService
}

func freezeRequestPrices(ctx context.Context, resolver *ModelPricingResolver) context.Context {
	view := &requestPriceSnapshot{resolver: resolver, catalog: resolver.billingService.catalogSnapshot()}
	if resolver.channelService != nil {
		if channels, err := resolver.channelService.loadCache(ctx); err == nil && !channels.loadFailed {
			view.channels = channels
		}
	}
	return context.WithValue(ctx, requestPriceKey{}, view)
}

func requestPrices(ctx context.Context) *requestPriceSnapshot {
	if ctx == nil {
		return nil
	}
	view, _ := ctx.Value(requestPriceKey{}).(*requestPriceSnapshot)
	return view
}

// WithUsagePriceSnapshot transfers prices to a detached worker without copying
// cancellation, routing overrides or authentication values from the client.
func WithUsagePriceSnapshot(base, parent context.Context) context.Context {
	if view := requestPrices(parent); view != nil {
		return context.WithValue(base, requestPriceKey{}, view)
	}
	return base
}

func (s *BillingService) catalogSnapshot() *BillingService {
	if s == nil || s.pricingService == nil {
		return s
	}
	p := s.pricingService
	p.mu.RLock()
	defer p.mu.RUnlock()
	if cached := s.catalogView.Load(); cached != nil && cached.pricingService.revision == p.revision {
		return cached
	}
	view := &BillingService{cfg: s.cfg, fallbackPrices: s.fallbackPrices, pricingService: &PricingService{cfg: p.cfg, pricingData: p.pricingData, revision: p.revision}}
	s.catalogView.Store(view)
	return view
}

func (s *BillingService) forRequest(ctx context.Context) *BillingService {
	if view := requestPrices(ctx); view != nil && view.resolver.billingService == s {
		return view.catalog
	}
	return s
}

func (s *ChannelService) billingChannel(ctx context.Context, groupID int64) (*channelLookup, error) {
	if view := requestPrices(ctx); view != nil && view.resolver.channelService == s {
		if view.channels == nil {
			return nil, nil
		}
		return lookupCachedGroupChannel(ctx, view.channels, groupID), nil
	}
	return s.lookupGroupChannel(ctx, groupID)
}
