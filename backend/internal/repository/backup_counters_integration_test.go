//go:build integration

package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestBackupRestoreCounterCompatibility(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "current"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			tx := testTx(t)
			ctx := context.Background()
			execute := func(query string) {
				t.Helper()
				_, err := tx.ExecContext(ctx, query)
				require.NoError(t, err)
			}
			execute("CREATE SCHEMA counter_restore_test; SET LOCAL search_path TO counter_restore_test")
			before, err := os.ReadFile("testdata/usage_counters_before.sql")
			require.NoError(t, err)
			migration, err := migrations.FS.ReadFile(persistentCountersMigration)
			require.NoError(t, err)
			postlude, err := restoreCountersPostlude()
			require.NoError(t, err)
			postlude = strings.ReplaceAll(postlude, "search_path TO public", "search_path TO counter_restore_test")
			execute(string(before))
			execute(schemaMigrationsTableDDL)
			execute(string(migration))
			// A pre-media backup must discard reservations from the old target
			// database; they were charged against a different balance snapshot.
			mediaMigration, err := migrations.FS.ReadFile("214_media_precharge.sql")
			require.NoError(t, err)
			execute(string(mediaMigration))
			for _, filename := range []string{"215_media_billing_outcomes.sql", "216_media_payment_lifecycle.sql"} {
				mediaMigration, err = migrations.FS.ReadFile(filename)
				require.NoError(t, err)
				execute(string(mediaMigration))
			}
			execute(`INSERT INTO media_billing_reservations (id,user_id,api_key_id,account_id,quote) VALUES ('old-target',1,1,1,'{}')`)
			execute(`INSERT INTO media_billing_events (reservation_id,ordinal,stage,amount) VALUES ('old-target',1,'request_created',0)`)
			execute(`INSERT INTO usage_logs (id, request_id, billing_mode, account_stats_cost) VALUES (2, 'video:snapshot', 'video', 2);
				DELETE FROM usage_logs WHERE id = 2;
				UPDATE redeem_codes SET status = 'used', used_by = 1 WHERE id = 2;`)
			execute(strings.ReplaceAll(restoreCountersPrelude, "public.", "counter_restore_test."))
			if legacy {
				// A legacy dump replaces these tables but cannot drop the new counters.
				execute("DROP TABLE usage_logs, users, redeem_codes, usage_billing_dedup, usage_billing_dedup_archive")
				execute(string(before))
			} else {
				// Current pg_dump restores functions and triggers after loading table data.
				execute(string(migration))
				execute("INSERT INTO schema_migrations (filename, checksum) VALUES ('" + persistentCountersMigration + "', 'snapshot')")
			}
			execute(postlude)
			execute(postlude)
			var reservations int
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_billing_reservations").Scan(&reservations))
			require.Zero(t, reservations)
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_billing_events").Scan(&reservations))
			require.Zero(t, reservations)
			execute("SAVEPOINT incomplete_media_backup")
			execute("ALTER TABLE media_billing_reservations DROP COLUMN final_quote")
			_, err = tx.ExecContext(ctx, postlude)
			require.ErrorContains(t, err, "Backup is missing media billing")
			execute("ROLLBACK TO SAVEPOINT incomplete_media_backup")
			execute("SAVEPOINT incomplete_backup")
			execute("DROP TRIGGER usage_counters_write ON usage_logs")
			_, err = tx.ExecContext(ctx, postlude)
			require.ErrorContains(t, err, "Backup is missing persistent statistics")
			execute("ROLLBACK TO SAVEPOINT incomplete_backup")
			var requests int64
			var cost, recharged, balance float64
			readTotals := func() {
				t.Helper()
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(requests), 0), COALESCE(SUM(actual_cost), 0)
					FROM usage_counters WHERE bucket_date = DATE '0001-01-01'`).Scan(&requests, &cost))
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount) FILTER (WHERE metric = 'recharged'), 0),
					COALESCE(SUM(amount) FILTER (WHERE metric = 'balance'), 0) FROM dashboard_user_financials`).Scan(&recharged, &balance))
			}
			readTotals()
			require.Equal(t, 12.0, balance)
			if legacy {
				require.Zero(t, requests)
				require.Zero(t, cost)
				require.Zero(t, recharged)
				var receipts int
				require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_counter_refunds").Scan(&receipts))
				require.Zero(t, receipts)
			} else {
				require.EqualValues(t, 1, requests)
				require.Equal(t, 2.5, cost)
				require.Equal(t, 10.0, recharged)
				execute("SELECT reverse_missing_usage_counters('video:snapshot', 1)")
				readTotals()
				require.Zero(t, cost)
			}
			execute("INSERT INTO usage_logs (id) VALUES (3)")
			readTotals()
			require.Equal(t, 2.5, cost)
			if legacy {
				require.EqualValues(t, 1, requests)
			} else {
				require.EqualValues(t, 2, requests)
			}
		})
	}
}
