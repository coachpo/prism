-- Routing defaults: cover expired credentials, and stop one blip from taking a
-- model offline for a full minute.
--
-- Two independent problems, one migration because both live on the same column
-- family of loadbalance_strategies:
--
-- 1. failure_status_codes carried 403 but not 401. An expired or revoked upstream
--    key is the most common self-hosted failure and it is a 401, so the gateway
--    returned the first target's 401 to the caller without trying any of the
--    healthy backups, and without recording anything against that connection's
--    health. 408 joins for the same reason.
--
-- 2. retry_base_delay_ms defaulted to 60000. Because the runtime writes
--    next_retry_at on the very first failure, a single transient blip made every
--    terminal target of a model ineligible for a full minute; requests in that
--    window were answered 503 in milliseconds without ever contacting an
--    upstream. The runtime now falls back to trying cooled-down candidates when
--    nothing else is left, and the default drops to 5s so ordinary backoff is
--    proportionate. Explicit bans are unaffected.
--
-- Existing rows are only rewritten when they still carry the exact previous
-- canonical values; anything an operator customised is left alone.

ALTER TABLE public.loadbalance_strategies
    ALTER COLUMN failure_status_codes
    SET DEFAULT ARRAY[401, 403, 408, 422, 429, 500, 502, 503, 504, 529];

UPDATE public.loadbalance_strategies
SET failure_status_codes = ARRAY[401, 403, 408, 422, 429, 500, 502, 503, 504, 529]::integer[]
WHERE failure_status_codes = ARRAY[403, 422, 429, 500, 502, 503, 504, 529]::integer[];

ALTER TABLE public.loadbalance_strategies
    ALTER COLUMN retry_base_delay_ms
    SET DEFAULT 5000;

UPDATE public.loadbalance_strategies
SET retry_base_delay_ms = 5000
WHERE retry_base_delay_ms = 60000;
