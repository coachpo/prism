-- Drop retained-history indexes with no reachable consumer.
--
-- `request_logs` and `usage_request_events` are the two highest-write tables
-- in the system, and their index set had grown to roughly the size of the
-- heap. Every index is paid for on every insert and widens the planner's
-- candidate list on every read, so an index no query can use is pure cost.
--
-- Each drop below is justified by the query shapes in the code, not by a
-- guess about selectivity:
--
-- 1) idx_request_logs_ingress_chain
--    (profile_id, ingress_request_id, attempt_number, created_at, id)
--    is strictly dominated by idx_request_logs_ingress_created_id
--    (profile_id, ingress_request_id, created_at, id). Both answer the same
--    (profile_id, ingress_request_id) lookups; nothing orders retained rows
--    by attempt_number, so the extra key only makes the index wider. The
--    finalized-summary LATERAL orders by an expression over
--    final_attempt_number/is_winner, which no btree ordering can serve.
--
-- 2) idx_request_logs_ingress_request_id (ingress_request_id) has no
--    consumer: every ingress lookup in the read path, including the
--    correlated EXISTS and LATERAL joins, carries profile_id, and the
--    composite above leads with it.
--
-- 3) ix_request_logs_api_family and ix_usage_request_events_api_family
--    (api_family) cannot be used by their only filter. The API-family
--    selector is written as `NULLIF(api_family, '') IN (...)`, and a plain
--    btree on the bare column does not match that expression. Every other
--    reference to the column is a projection.
--
-- 4) ix_usage_request_events_created_at (created_at) indexes the partition
--    key on its own. Partition pruning already resolves the range, and every
--    consumer is profile-scoped and served by
--    idx_usage_request_events_profile_created_at. This is the same reasoning
--    000020 applied to ix_request_logs_created_at on the sibling table.
--
-- Single-column indexes on endpoint_id, connection_id, model_id, and id stay:
-- each still has a query shape no composite leads with, so removing them
-- needs live read statistics rather than a code argument.
--
-- The migration runner owns a single transaction, so CONCURRENTLY is not
-- available here; dropping a partitioned parent index removes its children in
-- the same transaction.

DROP INDEX IF EXISTS public.idx_request_logs_ingress_chain;
DROP INDEX IF EXISTS public.idx_request_logs_ingress_request_id;
DROP INDEX IF EXISTS public.ix_request_logs_api_family;
DROP INDEX IF EXISTS public.ix_usage_request_events_api_family;
DROP INDEX IF EXISTS public.ix_usage_request_events_created_at;
