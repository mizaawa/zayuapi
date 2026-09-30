package service

import "context"

// Legacy group policy fields remain readable for older clients and snapshots,
// but reasoning effort is now always controlled by the client and upstream.
func NormalizeMaxReasoningEffort(_ string) string {
	return ""
}

func normalizeMaxReasoningEffortForPlatform(_, _ string) (string, error) {
	return "", nil
}

func NormalizeReasoningEffortMappings(_ string, _ []ReasoningEffortMapping) ([]ReasoningEffortMapping, error) {
	return []ReasoningEffortMapping{}, nil
}

func sanitizeGroupReasoningEffortPolicy(group *Group) {
	if group == nil {
		return
	}
	group.MaxReasoningEffort = ""
	group.ReasoningEffortMappings = []ReasoningEffortMapping{}
}

// Keep these entry points inert so stale auth caches and WS session hooks
// cannot apply a ceiling or mapping during an upgrade.
func WithOpenAIReasoningEffortPolicy(ctx context.Context, _ string, _ []ReasoningEffortMapping) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ApplyOpenAIReasoningEffortPolicyFromContext(_ context.Context, body []byte) ([]byte, bool) {
	return body, false
}

func ApplyOpenAIReasoningEffortPolicy(body []byte, _ string, _ []ReasoningEffortMapping) ([]byte, bool) {
	return body, false
}
