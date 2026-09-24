-- 000035_request_scoped_reroute
-- Request-scoped reroute: statuses that reject only the current request.
--
-- A provider answers 400 when this target cannot serve this particular request,
-- for example an image sent to a text-only model. Another target of the same
-- model may accept it, and the rejection says nothing about the first target's
-- health. failure_status_codes cannot express that: every code in it also
-- writes the target's retry window, so one such request would cool a healthy
-- target down and push later ordinary requests onto other targets.
--
-- reroute_status_codes is the separate, request-scoped set. The runtime moves
-- the request to the next candidate without runtime failure feedback. Only 4xx
-- statuses qualify, 429 stays a target-health signal, and the two sets are
-- disjoint. Existing strategies receive the default {400}, except a strategy
-- that already fails over on 400, which keeps that behavior with an empty set.
--
-- The launch after such a rejection records the new `reroute` attempt trigger,
-- so the three trigger constraints on retained request history widen to accept
-- it. Every existing row satisfies the widened constraints.

ALTER TABLE public.loadbalance_strategies
    ADD COLUMN reroute_status_codes integer[];

UPDATE public.loadbalance_strategies
SET reroute_status_codes = CASE
        WHEN 400 = ANY (failure_status_codes) THEN ARRAY[]::integer[]
        ELSE ARRAY[400]::integer[]
    END
WHERE reroute_status_codes IS NULL;

ALTER TABLE public.loadbalance_strategies
    ALTER COLUMN reroute_status_codes SET DEFAULT ARRAY[400],
    ALTER COLUMN reroute_status_codes SET NOT NULL,
    ADD CONSTRAINT chk_loadbalance_strategies_reroute_status_codes
        CHECK (400 <= ALL (reroute_status_codes) AND 499 >= ALL (reroute_status_codes) AND 429 <> ALL (reroute_status_codes)),
    ADD CONSTRAINT chk_loadbalance_strategies_reroute_failure_disjoint
        CHECK (NOT (reroute_status_codes && failure_status_codes));

ALTER TABLE public.request_logs
    DROP CONSTRAINT ck_request_logs_attempt_trigger;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_attempt_trigger
    CHECK (attempt_trigger IS NULL OR attempt_trigger IN (
        'initial','retry_same_target','hedge','failover','reroute'
    ));

ALTER TABLE public.usage_request_events
    DROP CONSTRAINT ck_usage_request_events_final_attempt_trigger;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_final_attempt_trigger
    CHECK (final_attempt_trigger IS NULL OR final_attempt_trigger IN (
        'initial','retry_same_target','hedge','failover','reroute'
    ));

ALTER TABLE public.usage_request_events
    DROP CONSTRAINT ck_usage_request_events_final_target_entry_trigger;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_final_target_entry_trigger
    CHECK (final_target_entry_trigger IS NULL OR final_target_entry_trigger IN (
        'initial','failover','hedge','reroute','unknown'
    ));
