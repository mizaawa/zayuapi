package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinalizeOperationalOrDegradedUsesTenAndSixtySecondBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		latency  time.Duration
		expected string
	}{
		{name: "below ten seconds", latency: 10*time.Second - time.Millisecond, expected: MonitorStatusOperational},
		{name: "at ten seconds", latency: 10 * time.Second, expected: MonitorStatusDegraded},
		{name: "below sixty seconds", latency: 60*time.Second - time.Millisecond, expected: MonitorStatusDegraded},
		{name: "at sixty seconds", latency: 60 * time.Second, expected: MonitorStatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := int(tt.latency.Milliseconds())
			result := finalizeOperationalOrDegraded(&CheckResult{LatencyMs: &ms})
			require.Equal(t, tt.expected, result.Status)
		})
	}
}
