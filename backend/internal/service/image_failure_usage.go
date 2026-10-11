package service

// Keep reported usage on errors without treating a failed attempt as an image
// result. The workbench's trusted billing context decides whether to charge it.
func imageFailureUsageResult(usage OpenAIUsage, model, upstreamModel string) *OpenAIForwardResult {
	if !usage.HasTokens() {
		return nil
	}
	return &OpenAIForwardResult{Usage: usage, Model: model, UpstreamModel: upstreamModel}
}
