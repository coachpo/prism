CREATE TABLE public.alert_webhook_outbox (
    id uuid NOT NULL,
    event_type text NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    idempotency_key text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 8 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_by text,
    locked_until timestamp with time zone,
    sent_at timestamp with time zone,
    dead_lettered_at timestamp with time zone,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT alert_webhook_outbox_attempts_check CHECK (((attempt_count >= 0) AND (max_attempts > 0) AND (attempt_count <= max_attempts))),
    CONSTRAINT alert_webhook_outbox_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'sending'::text, 'sent'::text, 'dead'::text])))
);

ALTER TABLE ONLY public.alert_webhook_outbox
    ADD CONSTRAINT alert_webhook_outbox_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_alert_webhook_outbox_idempotency_key ON public.alert_webhook_outbox USING btree (idempotency_key);
CREATE INDEX idx_alert_webhook_outbox_due ON public.alert_webhook_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'queued'::text);
CREATE INDEX idx_alert_webhook_outbox_stale_locks ON public.alert_webhook_outbox USING btree (locked_until) WHERE (status = 'sending'::text);
CREATE INDEX idx_alert_webhook_outbox_dead_letters ON public.alert_webhook_outbox USING btree (dead_lettered_at DESC) WHERE (status = 'dead'::text);
