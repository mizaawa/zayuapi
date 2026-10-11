# Persistent Usage Counters

Migration `213_persistent_usage_counters.sql` starts lifetime and daily usage
counters at deployment. It does not scan or backfill historical usage or
credited recharge records. Current user balances are preserved as opening
balances; they are assets, not historical consumption.

`usage_counters` stores numeric counters per user, API key, route, and day.
The `0001-01-01` bucket stores lifetime totals, so lifetime queries do not sum
the daily history. Identifiers support the existing platform breakdown and
monitor visibility rules. No request or response content is copied.

Database triggers update counters in the log-writing transaction. Failed
transactions and duplicate inserts do not contribute. Billing deduplication
records and their archive retain a counter flag so billed request replays after
log cleanup cannot increment the totals again. Cost corrections update both
lifetime and daily counters. Asynchronous video charges retain a small refund
receipt so a later refund also works after the detailed log has been deleted.

`dashboard_user_financials` maintains current balances and newly credited
positive balance/admin recharges. Deleting a user excludes their balance but
preserves credited recharge and usage totals. Deleting or clearing usage logs
does not delete the counters. Legacy dashboard aggregation rebuilds and
retention jobs operate on their own tables and cannot reset these counters.

## Data management

S3 and WebDAV full database backups include the counters, billing deduplication
flags, financial totals, and outstanding refund receipts. PostgreSQL restores
table data before installing the dumped triggers, so restoring a current backup
preserves its totals without counting the imported logs again.

In-app restore first validates the decompressed backup in a temporary disk file
and then runs psql with `ON_ERROR_STOP`, `--file=-`, and `--single-transaction`.
The temporary file is removed afterward; memory usage does not grow with the
dump or psql output. Restores require temporary disk space for the unpacked SQL.
Any SQL error rolls back the restore, including counter compatibility changes.

For backups preceding migration 213, restoration removes target-database counter
leftovers and applies migration 213 in the same transaction. Usage and recharge
totals begin at zero without a historical backfill; balances come from the
restored users. The usage_logs row-type function is removed before restoration
to allow older dumps to replace that table. Current backups restore their own
function and totals. Unrelated older schema migrations still follow the normal
application upgrade process.

On success, the running process invalidates dashboard caches and reloads the
backup schedule and storage configuration. Backup and restore jobs are mutually
exclusive within the process. Storage measurement automatically includes the
new tables; manual, scheduled, and filtered log cleanup leave them intact.

User dashboard statistics and the usage page's top cards read these counters.
The usage cards show lifetime values; date and other filters still apply to
log pagination and detailed charts. The usage page omits the old headline
summary scan and reuses endpoint group counts for filtered pagination.
Admin headline amounts, consumption,
recharges, tokens, and requests use the same persistent sources regardless of
whether legacy chart aggregation is enabled. RPM/TPM retain the existing
bounded five-minute log query. Day boundaries follow the configured database
connection/application timezone.

## Verification

- Backend unit tests: `go test ./internal/repository ./internal/service ./internal/handler ./internal/handler/admin ./migrations`
- Database regressions (Docker): `go test -tags integration ./internal/repository -run 'TestPersistentUsage|TestDashboardStats_Balances'`
- Frontend: `pnpm test:run src/views/user/__tests__/UsageView.spec.ts src/components/admin/usage/__tests__/UsageStatsCards.spec.ts`
- Type checks: `pnpm typecheck`

The migration fixtures in `backend/internal/repository/testdata/usage_counters_*.sql`
cover the deployment boundary, day boundaries, idempotency, rollback, cost
correction, deletion, replay after cleanup, and refunds after cleanup.
