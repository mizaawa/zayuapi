//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageCleanupRepositoryPartitionedTableStorageStatsCountLeafStorageOnce(t *testing.T) {
	ctx := context.Background()
	schema := fmt.Sprintf("storage_stats_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})

	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.usage_probe (
			id bigint NOT NULL,
			created_at date NOT NULL,
			payload text NOT NULL
		) PARTITION BY RANGE (created_at);
		CREATE TABLE %s.usage_probe_202601 PARTITION OF %s.usage_probe
			FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
		CREATE TABLE %s.usage_probe_202602 PARTITION OF %s.usage_probe
			FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
		CREATE INDEX usage_probe_payload_idx ON %s.usage_probe (payload);
		INSERT INTO %s.usage_probe (id, created_at, payload)
		SELECT i,
			CASE WHEN i %% 2 = 0 THEN DATE '2026-01-15' ELSE DATE '2026-02-15' END,
			md5(i::text) || repeat(md5(i::text), 16)
		FROM generate_series(1, 1000) AS i
	`, schema, schema, schema, schema, schema, schema, schema))
	require.NoError(t, err)

	var expectedTableBytes, expectedIndexBytes, expectedTotalBytes int64
	err = integrationDB.QueryRowContext(ctx, `
		SELECT SUM(pg_table_size(child.oid))::bigint,
			SUM(pg_indexes_size(child.oid))::bigint,
			SUM(pg_total_relation_size(child.oid))::bigint
		FROM pg_class child
		JOIN pg_inherits inheritance ON inheritance.inhrelid = child.oid
		WHERE inheritance.inhparent = $1::regclass
	`, schema+".usage_probe").Scan(&expectedTableBytes, &expectedIndexBytes, &expectedTotalBytes)
	require.NoError(t, err)
	require.Greater(t, expectedIndexBytes, int64(0))

	stats, err := (&usageCleanupRepository{sql: integrationDB}).GetDatabaseStorageStats(ctx)
	require.NoError(t, err)

	partitionedTableIndex := -1
	var tableNames []string
	var summedTableBytes, summedIndexBytes, summedTotalBytes int64
	for i := range stats.Tables {
		table := &stats.Tables[i]
		tableNames = append(tableNames, table.TableName)
		summedTableBytes += table.TableBytes
		summedIndexBytes += table.IndexBytes
		summedTotalBytes += table.TotalBytes
		if table.TableName == schema+".usage_probe" {
			if partitionedTableIndex >= 0 {
				t.Fatalf("partitioned parent appeared more than once")
			}
			partitionedTableIndex = i
		}
	}
	require.NotEqual(t, -1, partitionedTableIndex, "partitioned parent should be listed")
	parentStats := stats.Tables[partitionedTableIndex]
	require.Equal(t, expectedTableBytes, parentStats.TableBytes)
	require.Equal(t, expectedIndexBytes, parentStats.IndexBytes)
	require.Equal(t, expectedTotalBytes, parentStats.TotalBytes)
	require.NotContains(t, tableNames, schema+".usage_probe_202601")
	require.NotContains(t, tableNames, schema+".usage_probe_202602")
	require.Equal(t, summedTableBytes, stats.TableBytes)
	require.Equal(t, summedIndexBytes, stats.IndexBytes)
	require.Equal(t, summedTotalBytes, stats.TotalBytes)

	var expectedDatabaseBytes int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT pg_database_size(current_database())::bigint`).Scan(&expectedDatabaseBytes))
	require.Equal(t, expectedDatabaseBytes, stats.DatabaseBytes)
}
