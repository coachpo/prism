-- 000031_terminal_target_upstream_model_identity
-- Data-preserving upstream model identity decoupling. Clients keep addressing
-- the gateway by the stable entry model_id; every Terminal Target now owns an
-- explicit, persisted upstream_model_id that runtime request rewriting uses
-- verbatim per upstream attempt, and every real upstream request-log row plus
-- the final winner usage event captures the request-time snapshot.
--
-- Three additive, nullable columns and one precise backfill:
--   * connections.upstream_model_id: the explicit upstream identity of the
--     Terminal Target. Owned connections (exactly one owner edge in
--     model_access_targets) are backfilled from their owner model's current
--     model_id; orphan connections without any owner edge keep NULL until a
--     write gives them one. Later model renames never cascade into this column.
--   * request_logs.upstream_model_id and usage_request_events.upstream_model_id:
--     nullable request-time snapshots written only by the runtime writer.
--     Historical rows and non-upstream rows keep NULL and are never rewritten,
--     so retained evidence stays honest instead of being back-filled from the
--     live configuration.
--
-- Purely additive: no column is dropped or repurposed, no existing row is
-- rewritten except the owned-connection backfill below, and no table or
-- reference is lost. Model IDs never exceed 200 Unicode characters
-- (model_configs.model_id is varchar(200)); the management contract enforces
-- trimming and 422 rejections, while PostgreSQL enforces the stored length.

-- The current schema owns uq_model_access_targets_connection_owner, but an
-- upgrade must still fail closed if retained state was modified outside that
-- contract. UPDATE ... FROM does not define which source row wins when more
-- than one owner matches, so reject the anomaly before adding any columns.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.model_access_targets
        WHERE target_connection_id IS NOT NULL
        GROUP BY target_connection_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'terminal target upstream model identity migration requires unique connection owners';
    END IF;
END
$$;

ALTER TABLE public.connections
    ADD COLUMN upstream_model_id character varying(200);

ALTER TABLE public.request_logs
    ADD COLUMN upstream_model_id character varying(200);

ALTER TABLE public.usage_request_events
    ADD COLUMN upstream_model_id character varying(200);

-- Backfill owned connections with their owner model's current entry model_id.
-- The guard above and uq_model_access_targets_connection_owner make the match
-- single-valued; connections without an owner edge are not matched and stay
-- NULL.
UPDATE public.connections
SET upstream_model_id = owner_models.model_id
FROM public.model_access_targets owner_edges
JOIN public.model_configs owner_models ON owner_models.id = owner_edges.source_model_config_id
WHERE owner_edges.target_connection_id = connections.id
  AND connections.upstream_model_id IS NULL;

ALTER TABLE public.connections
    ADD CONSTRAINT ck_connections_upstream_model_id_blank
        CHECK (
            upstream_model_id IS NULL
            OR public.prism_pricing_trim_unicode_whitespace(upstream_model_id) <> ''
        );

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_upstream_model_id_blank
        CHECK (
            upstream_model_id IS NULL
            OR public.prism_pricing_trim_unicode_whitespace(upstream_model_id) <> ''
        );

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_upstream_model_id_blank
        CHECK (
            upstream_model_id IS NULL
            OR public.prism_pricing_trim_unicode_whitespace(upstream_model_id) <> ''
        );
