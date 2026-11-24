-- Read-only diagnostics. No identifiers or private event payloads are selected.
-- Run with an operations account; do not expose results through the public API.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SELECT count(*) AS pending,
       coalesce(greatest(extract(epoch FROM now()-min(occurred_at)),0),0) AS oldest_pending_seconds,
       coalesce(sum(greatest(attempt_count-1,0)),0) AS pending_retry_attempts,
       count(*) FILTER (WHERE lease_until <= now()) AS expired_leases,
       count(*) FILTER (WHERE last_error = 'fanout_failed') AS pending_fanout_failed
FROM realtime_events WHERE dispatched_at IS NULL;
SELECT pg_size_pretty(pg_total_relation_size('realtime_events')) AS table_and_indexes;
-- This manual historical aggregation can be expensive; it is NOT on the API
-- request path or the five-second monitoring sampler.
SELECT date_trunc('day',dispatched_at) AS dispatched_day, count(*) AS rows
FROM realtime_events WHERE dispatched_at IS NOT NULL
GROUP BY 1 ORDER BY 1;
COMMIT;
