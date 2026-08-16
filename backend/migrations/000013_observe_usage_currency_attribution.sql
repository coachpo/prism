-- 000013_observe_usage_currency_attribution
-- Additive completion of the Observe owner usage-event schema (Observe SPEC
-- wire: currency_attribution: "identified" | "legacy_unknown"). The column
-- records whether the reporting currency on a usage event was identified from
-- an authoritative source at capture time. It is additive only: no column is
-- dropped or repurposed, and existing rows are conservatively classified as
-- legacy_unknown.

ALTER TABLE public.usage_request_events
    ADD COLUMN currency_attribution character varying(24) NOT NULL DEFAULT 'legacy_unknown';

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_currency_attribution
        CHECK (currency_attribution IN ('identified', 'legacy_unknown'));
