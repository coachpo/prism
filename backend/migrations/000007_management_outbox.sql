CREATE TABLE "public"."management_outbox" (
    "id" BIGSERIAL NOT NULL,
    "operation_id" text NOT NULL,
    "event_type" text NOT NULL,
    "aggregate_type" text NOT NULL,
    "aggregate_id" text NOT NULL,
    "aggregate_version" bigint NULL,
    "dedupe_key" text NOT NULL,
    "payload" jsonb NOT NULL,
    "status" text NOT NULL,
    "attempt_count" integer NOT NULL DEFAULT 0,
    "next_attempt_at" timestamp with time zone NOT NULL DEFAULT now(),
    "locked_by" text NULL,
    "locked_at" timestamp with time zone NULL,
    "last_error" text NULL,
    "actor_id" text NULL,
    "trace_id" text NULL,
    "created_at" timestamp with time zone NOT NULL DEFAULT now(),
    "processed_at" timestamp with time zone NULL,
    CONSTRAINT "management_outbox_status_check" CHECK (status IN ('pending', 'processing', 'retry', 'succeeded', 'failed_permanent'))
);

ALTER TABLE ONLY "public"."management_outbox" ADD CONSTRAINT "management_outbox_pkey" PRIMARY KEY (id);
CREATE UNIQUE INDEX "idx_management_outbox_dedupe_key" ON "public"."management_outbox" USING btree (dedupe_key);
CREATE INDEX "idx_management_outbox_polling" ON "public"."management_outbox" USING btree (status, next_attempt_at, created_at, id);
CREATE INDEX "idx_management_outbox_aggregate" ON "public"."management_outbox" USING btree (aggregate_type, aggregate_id, aggregate_version);
CREATE INDEX "idx_management_outbox_operation" ON "public"."management_outbox" USING btree (operation_id);
