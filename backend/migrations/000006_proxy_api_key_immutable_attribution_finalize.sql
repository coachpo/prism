-- Proxy API key immutable attribution finalize (Proxy Key SPEC §5.2 step 6).
--
-- After 000003 and the same-release writer/reader switch, the legacy
-- FK-backed proxy_api_key_id column is removed: final target has one ID truth
-- (proxy_api_key_id_snapshot), not dual-read compatibility. Deleting or
-- renaming a proxy_api_keys row cannot cascade into retained history because
-- snapshot columns carry no FK and the legacy FK is dropped here.

-- request_logs: drop legacy FK, legacy column and its single-column index.
ALTER TABLE public.request_logs
    DROP CONSTRAINT IF EXISTS request_logs_proxy_api_key_id_fkey;

DROP INDEX IF EXISTS ix_request_logs_proxy_api_key_id;

ALTER TABLE public.request_logs
    DROP COLUMN IF EXISTS proxy_api_key_id;

-- usage_request_events: drop legacy FK, legacy column and its index.
ALTER TABLE public.usage_request_events
    DROP CONSTRAINT IF EXISTS usage_request_events_proxy_api_key_id_fkey;

DROP INDEX IF EXISTS ix_usage_request_events_proxy_api_key_id;

ALTER TABLE public.usage_request_events
    DROP COLUMN IF EXISTS proxy_api_key_id;
