package domain

// ReasoningEffortMapping is retained for legacy API and snapshot compatibility.
// Deprecated: group effort mappings are ignored.
type ReasoningEffortMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}
