\set ON_ERROR_STOP on
\pset pager off

BEGIN READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '1s';

SELECT current_database() AS database,
       pg_size_pretty(pg_database_size(current_database())) AS database_size;

SELECT relname AS table_name,
       pg_size_pretty(pg_table_size(relid)) AS table_size,
       pg_size_pretty(pg_indexes_size(relid)) AS indexes_size,
       pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
       n_live_tup, n_dead_tup, last_autovacuum, last_autoanalyze
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(relid) DESC
LIMIT 20;

SELECT t.relname AS table_name, idx.relname AS index_name,
       i.indisvalid, i.indisready,
       pg_size_pretty(pg_relation_size(idx.oid)) AS index_size,
       pg_get_indexdef(i.indexrelid) AS definition
FROM pg_index i
JOIN pg_class t ON t.oid = i.indrelid
JOIN pg_class idx ON idx.oid = i.indexrelid
WHERE t.relname IN ('usage_logs', 'users', 'api_keys')
ORDER BY t.relname, idx.relname;

SELECT state, wait_event_type, wait_event, COUNT(*) AS connections,
       MAX(NOW() - query_start) AS oldest_query,
       MAX(NOW() - xact_start) AS oldest_transaction
FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid()
GROUP BY state, wait_event_type, wait_event
ORDER BY connections DESC;

SELECT name, setting, unit
FROM pg_settings
WHERE name IN ('max_connections', 'shared_buffers', 'work_mem', 'effective_cache_size',
               'autovacuum', 'autovacuum_vacuum_scale_factor', 'autovacuum_analyze_scale_factor')
ORDER BY name;

-- Supply -v login_user_id=123 to inspect a representative user's indexed plan.
-- EXPLAIN without ANALYZE only plans the query; it does not scan the log table.
\if :{?login_user_id}
EXPLAIN (FORMAT TEXT)
SELECT id, created_at, model
FROM usage_logs
WHERE user_id = :'login_user_id'::bigint
  AND created_at >= date_trunc('day', NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'
ORDER BY created_at DESC, id DESC
LIMIT 6;

EXPLAIN (FORMAT TEXT)
SELECT COUNT(*), COALESCE(SUM(actual_cost), 0)
FROM usage_logs
WHERE user_id = :'login_user_id'::bigint
  AND created_at >= date_trunc('day', NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai';
\endif

ROLLBACK;
