package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaGroupExposesOpenAIFastRate(t *testing.T) {
	group := &service.PlazaGroup{
		ID: 12, Platform: service.PlatformOpenAI,
		RateMultiplier: 1.25, FastRateEnabled: true, FastRateMultiplier: 3,
	}
	payload, err := json.Marshal(toModelPlazaGroupDTO(group, nil))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Equal(t, true, decoded["fast_rate_enabled"])
	require.Equal(t, 3.0, decoded["fast_rate_multiplier"])
	require.Equal(t, 1.25, decoded["rate_multiplier"])
}
