package service

// Shared by repository fixtures; production imports no longer merge CRS data.
func mergeMap(existing, updates map[string]any) map[string]any {
	merged := make(map[string]any, len(existing)+len(updates))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range updates {
		merged[key] = value
	}
	return merged
}
