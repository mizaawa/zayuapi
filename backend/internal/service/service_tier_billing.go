package service

// resolveOpenAIBillingServiceTier follows upstream sub2api's downgrade-only
// contract. requested must come from the final body sent upstream.
func resolveOpenAIBillingServiceTier(account *Account, requested, observed *string) *string {
	requestedTier, observedTier := "", ""
	if requested != nil {
		requestedTier = normalizedOpenAIServiceTierValue(*requested)
	}
	if observed != nil {
		observedTier = normalizedOpenAIServiceTierValue(*observed)
	}
	if observedTier == "" {
		return optionalTrimmedStringPtr(requestedTier)
	}
	// Codex commonly echoes default even when it processes an effective Fast turn.
	if account != nil && account.Platform == PlatformOpenAI && account.IsOAuth() && observedTier == "default" {
		return optionalTrimmedStringPtr(requestedTier)
	}
	if serviceTierCostMultiplier(observedTier) < serviceTierCostMultiplier(requestedTier) {
		return optionalTrimmedStringPtr(observedTier)
	}
	return optionalTrimmedStringPtr(requestedTier)
}
