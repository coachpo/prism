CREATE TABLE "public"."email_outbox" (
    "id" uuid NOT NULL,
    "kind" text NOT NULL,
    "recipient_email" text NOT NULL,
    "template" text NOT NULL,
    "payload_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
    "email_secret_ciphertext" text,
    "idempotency_key" text NOT NULL,
    "status" text NOT NULL DEFAULT 'queued',
    "attempt_count" integer NOT NULL DEFAULT 0,
    "max_attempts" integer NOT NULL DEFAULT 8,
    "next_attempt_at" timestamptz NOT NULL DEFAULT now(),
    "locked_by" text,
    "locked_until" timestamptz,
    "sent_at" timestamptz,
    "dead_lettered_at" timestamptz,
    "last_error" text,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT "email_outbox_status_check" CHECK (status IN ('queued', 'sending', 'sent', 'dead')),
    CONSTRAINT "email_outbox_attempts_check" CHECK (attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts)
);

ALTER TABLE ONLY "public"."email_outbox" ADD CONSTRAINT "email_outbox_pkey" PRIMARY KEY (id);
CREATE UNIQUE INDEX "idx_email_outbox_idempotency_key" ON "public"."email_outbox" USING btree (idempotency_key);
CREATE INDEX "idx_email_outbox_due" ON "public"."email_outbox" USING btree (next_attempt_at, created_at, id) WHERE status = 'queued';
CREATE INDEX "idx_email_outbox_stale_locks" ON "public"."email_outbox" USING btree (locked_until) WHERE status = 'sending';
CREATE INDEX "idx_email_outbox_dead_letters" ON "public"."email_outbox" USING btree (dead_lettered_at DESC) WHERE status = 'dead';
CREATE INDEX "idx_email_outbox_kind" ON "public"."email_outbox" USING btree (kind);
