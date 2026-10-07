package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorSimulateRequestsMigration(t *testing.T) {
	content, err := FS.ReadFile("209_channel_monitor_simulate_requests.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE channel_monitors ADD COLUMN IF NOT EXISTS simulate_requests BOOLEAN NOT NULL DEFAULT FALSE")
}
