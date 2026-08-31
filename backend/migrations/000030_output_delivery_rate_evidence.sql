-- 000030_output_delivery_rate_evidence
-- Additive output-rate measurability evidence for the output-rate metric fix
-- (Observe/Requests SPEC: authoritative tok/s). The runtime writer classifies
-- every request once at capture time and writes the same verdict to the final
-- attempt row of request_logs and to usage_request_events:
--   * output_rate_state: measured | unmeasurable | not_applicable | unknown
--   * output_rate_reason: why the state holds (NULL only for measured)
--   * output_delivery_event_count: observed visible text/tool-output events
--   * output_delivery_span_ms: first-to-last visible-output event span
--
-- The authoritative per-request rate is derived on read only from
-- state = 'measured' rows as output_tokens * 1000.0 / output_delivery_span_ms;
-- historical rows keep NULL evidence and every reader projects them as
-- unknown, so old TTFT-derived artifacts can never re-enter an average.
-- Purely additive: no column is dropped or repurposed, no existing row is
-- rewritten. The schema enforces only that measured evidence is calculable
-- (output tokens, at least two events, and a positive span); the calibratable
-- 50ms threshold remains writer policy and is not frozen into a constraint.

ALTER TABLE public.request_logs
    ADD COLUMN output_rate_state character varying(20),
    ADD COLUMN output_rate_reason character varying(64),
    ADD COLUMN output_delivery_event_count integer,
    ADD COLUMN output_delivery_span_ms integer;

ALTER TABLE public.usage_request_events
    ADD COLUMN output_rate_state character varying(20),
    ADD COLUMN output_rate_reason character varying(64),
    ADD COLUMN output_delivery_event_count integer,
    ADD COLUMN output_delivery_span_ms integer;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_output_rate_state
        CHECK (output_rate_state IS NULL OR output_rate_state IN
            ('measured', 'unmeasurable', 'not_applicable', 'unknown')),
    ADD CONSTRAINT ck_request_logs_output_rate_reason
        CHECK (((output_rate_state IS NULL AND output_rate_reason IS NULL)
            OR (output_rate_state = 'measured' AND output_rate_reason IS NULL)
            OR (output_rate_state IN ('unmeasurable', 'not_applicable', 'unknown')
                AND output_rate_reason IS NOT NULL AND btrim(output_rate_reason) <> '')) IS TRUE),
    ADD CONSTRAINT ck_request_logs_output_rate_delivery_facts
        CHECK (((output_rate_state IS NULL
                AND output_delivery_event_count IS NULL AND output_delivery_span_ms IS NULL)
            OR (output_rate_state = 'measured'
                AND output_tokens IS NOT NULL AND output_tokens >= 0
                AND output_delivery_event_count IS NOT NULL AND output_delivery_event_count >= 2
                AND output_delivery_span_ms IS NOT NULL AND output_delivery_span_ms > 0)
            OR output_rate_state = 'unmeasurable'
            OR (output_rate_state IN ('not_applicable', 'unknown')
                AND output_delivery_event_count IS NULL AND output_delivery_span_ms IS NULL)) IS TRUE),
    ADD CONSTRAINT ck_request_logs_output_delivery_event_count
        CHECK (output_delivery_event_count IS NULL OR output_delivery_event_count >= 0),
    ADD CONSTRAINT ck_request_logs_output_delivery_span
        CHECK (output_delivery_span_ms IS NULL OR output_delivery_span_ms >= 0);

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_output_rate_state
        CHECK (output_rate_state IS NULL OR output_rate_state IN
            ('measured', 'unmeasurable', 'not_applicable', 'unknown')),
    ADD CONSTRAINT ck_usage_request_events_output_rate_reason
        CHECK (((output_rate_state IS NULL AND output_rate_reason IS NULL)
            OR (output_rate_state = 'measured' AND output_rate_reason IS NULL)
            OR (output_rate_state IN ('unmeasurable', 'not_applicable', 'unknown')
                AND output_rate_reason IS NOT NULL AND btrim(output_rate_reason) <> '')) IS TRUE),
    ADD CONSTRAINT ck_usage_request_events_output_rate_delivery_facts
        CHECK (((output_rate_state IS NULL
                AND output_delivery_event_count IS NULL AND output_delivery_span_ms IS NULL)
            OR (output_rate_state = 'measured'
                AND output_tokens IS NOT NULL AND output_tokens >= 0
                AND output_delivery_event_count IS NOT NULL AND output_delivery_event_count >= 2
                AND output_delivery_span_ms IS NOT NULL AND output_delivery_span_ms > 0)
            OR output_rate_state = 'unmeasurable'
            OR (output_rate_state IN ('not_applicable', 'unknown')
                AND output_delivery_event_count IS NULL AND output_delivery_span_ms IS NULL)) IS TRUE),
    ADD CONSTRAINT ck_usage_request_events_output_delivery_event_count
        CHECK (output_delivery_event_count IS NULL OR output_delivery_event_count >= 0),
    ADD CONSTRAINT ck_usage_request_events_output_delivery_span
        CHECK (output_delivery_span_ms IS NULL OR output_delivery_span_ms >= 0);
