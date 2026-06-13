ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS endpoint_label_snapshot text;

WITH ranked_request_logs AS (
    SELECT
        profile_id,
        ingress_request_id,
        endpoint_id,
        NULLIF(BTRIM(endpoint_description), '') AS endpoint_description,
        NULLIF(BTRIM(endpoint_base_url), '') AS endpoint_base_url,
        ROW_NUMBER() OVER (
            PARTITION BY profile_id, ingress_request_id, endpoint_id
            ORDER BY attempt_number DESC NULLS LAST, created_at DESC, id DESC
        ) AS row_number
    FROM public.request_logs
    WHERE ingress_request_id IS NOT NULL
      AND endpoint_id IS NOT NULL
), selected_request_logs AS (
    SELECT profile_id, ingress_request_id, endpoint_id, endpoint_description, endpoint_base_url
    FROM ranked_request_logs
    WHERE row_number = 1
), backfill AS (
    SELECT
        usage_events.created_at,
        usage_events.id,
        COALESCE(
            request_logs.endpoint_description,
            request_logs.endpoint_base_url,
            NULLIF(BTRIM(endpoints.name), ''),
            NULLIF(BTRIM(endpoints.base_url), ''),
            CASE
                WHEN usage_events.endpoint_id IS NOT NULL THEN 'Endpoint ' || usage_events.endpoint_id::text
                ELSE NULL
            END,
            'Unknown Endpoint'
        ) AS endpoint_label_snapshot
    FROM public.usage_request_events usage_events
    LEFT JOIN selected_request_logs request_logs
      ON request_logs.profile_id = usage_events.profile_id
     AND request_logs.ingress_request_id = usage_events.ingress_request_id
     AND request_logs.endpoint_id = usage_events.endpoint_id
    LEFT JOIN public.endpoints endpoints
      ON endpoints.id = usage_events.endpoint_id
    WHERE usage_events.endpoint_label_snapshot IS NULL
)
UPDATE public.usage_request_events usage_events
SET endpoint_label_snapshot = backfill.endpoint_label_snapshot
FROM backfill
WHERE usage_events.created_at = backfill.created_at
  AND usage_events.id = backfill.id
  AND usage_events.endpoint_label_snapshot IS NULL;

ALTER TABLE public.usage_request_events
    ALTER COLUMN endpoint_label_snapshot SET NOT NULL;
