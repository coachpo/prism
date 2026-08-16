-- Proxy API key immutable attribution surface (Proxy Key SPEC §5.1/§5.2).
--
-- Additive half: adds request-time immutable key ID/name snapshot columns, the
-- attribution state (identified/none/unknown), and the request-time auth
-- enforcement provenance to request_logs and usage_request_events. Backfills
-- existing rows from the legacy FK-backed proxy_api_key_id column without
-- guessing identity for NULL legacy IDs. Readers/writers switch in the same
-- release gate; 000004 drops the legacy column after this surface is live.
--
-- Retention rule: deleting or renaming a proxy_api_keys row must never rewrite
-- these snapshot columns. There is deliberately no FK on proxy_api_key_id_snapshot.

-- ---------------------------------------------------------------------------
-- request_logs
-- ---------------------------------------------------------------------------

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS proxy_api_key_id_snapshot bigint;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS proxy_api_key_attribution_state character varying(24) NOT NULL DEFAULT 'unknown';

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS proxy_api_key_auth_enforced_at_request boolean;

-- Backfill: non-null legacy ID becomes identified with the retained name
-- snapshot; a missing name falls back to a deterministic #<id> label (the same
-- fallback the filter options render for deleted configurations). NULL legacy
-- ID stays unknown: it may mean no key, pre-upgrade auth-off, telemetry loss,
-- or a key cleared by the old ON DELETE SET NULL — identity is never guessed
-- from the name. Existing name snapshots on unknown rows are preserved as
-- orphaned legacy display evidence.
UPDATE public.request_logs
SET proxy_api_key_id_snapshot = proxy_api_key_id,
    proxy_api_key_attribution_state = 'identified',
    proxy_api_key_name_snapshot = COALESCE(proxy_api_key_name_snapshot, '#' || proxy_api_key_id::text)
WHERE proxy_api_key_id IS NOT NULL;

-- Rows with NULL legacy ID keep the column DEFAULT 'unknown' (no-op here,
-- stated for contract clarity): identity is never reconstructed from name.

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_proxy_key_attribution_state
    CHECK (proxy_api_key_attribution_state IN ('identified', 'none', 'unknown'));

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_proxy_key_snapshot_consistent
    CHECK (
        (proxy_api_key_attribution_state = 'identified'
            AND proxy_api_key_id_snapshot IS NOT NULL
            AND proxy_api_key_name_snapshot IS NOT NULL)
        OR
        (proxy_api_key_attribution_state = 'none'
            AND proxy_api_key_id_snapshot IS NULL
            AND proxy_api_key_name_snapshot IS NULL)
        OR
        (proxy_api_key_attribution_state = 'unknown'
            AND proxy_api_key_id_snapshot IS NULL)
    );

-- Recursive index: applies to existing daily partitions and future ones
-- (partitioned-parent template; see Observe/R pair convention in 000005).
CREATE INDEX IF NOT EXISTS ix_request_logs_proxy_api_key_snapshot
    ON public.request_logs (profile_id, proxy_api_key_id_snapshot, created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- usage_request_events
-- ---------------------------------------------------------------------------

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS proxy_api_key_id_snapshot bigint;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS proxy_api_key_attribution_state character varying(24) NOT NULL DEFAULT 'unknown';

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS proxy_api_key_auth_enforced_at_request boolean;

UPDATE public.usage_request_events
SET proxy_api_key_id_snapshot = proxy_api_key_id,
    proxy_api_key_attribution_state = 'identified',
    proxy_api_key_name_snapshot = COALESCE(proxy_api_key_name_snapshot, '#' || proxy_api_key_id::text)
WHERE proxy_api_key_id IS NOT NULL;

-- Rows with NULL legacy ID keep the column DEFAULT 'unknown' (no-op here,
-- stated for contract clarity): identity is never reconstructed from name.

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_proxy_key_attribution_state
    CHECK (proxy_api_key_attribution_state IN ('identified', 'none', 'unknown'));

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_proxy_key_snapshot_consistent
    CHECK (
        (proxy_api_key_attribution_state = 'identified'
            AND proxy_api_key_id_snapshot IS NOT NULL
            AND proxy_api_key_name_snapshot IS NOT NULL)
        OR
        (proxy_api_key_attribution_state = 'none'
            AND proxy_api_key_id_snapshot IS NULL
            AND proxy_api_key_name_snapshot IS NULL)
        OR
        (proxy_api_key_attribution_state = 'unknown'
            AND proxy_api_key_id_snapshot IS NULL)
    );

CREATE INDEX IF NOT EXISTS ix_usage_request_events_proxy_api_key_snapshot
    ON public.usage_request_events (profile_id, proxy_api_key_id_snapshot, created_at DESC);
