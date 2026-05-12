-- Split sidecar watchdog live repair from retained action history.
-- Retained action history is partitioned by created_at and owned by centralized
-- retention. Live repair rows move to a separate non-retained queue, and
-- legacy action rows are intentionally discarded in this clean-break migration.

ALTER TABLE ONLY "public"."sidecar_watchdog_holds"
    DROP CONSTRAINT IF EXISTS "sidecar_watchdog_holds_last_action_id_fkey";
ALTER TABLE ONLY "public"."sidecar_watchdog_holds"
    DROP COLUMN IF EXISTS "last_action_id";

DROP TABLE IF EXISTS "public"."sidecar_watchdog_pending_actions";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_actions";

CREATE TABLE "public"."sidecar_watchdog_actions" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "auth_snapshot_id" integer,
    "hold_id" integer,
    "auth_id" text,
    "auth_name" text,
    "auth_index" text,
    "provider" text,
    "action_type" text NOT NULL,
    "reason" text,
    "previous_priority" integer,
    "target_priority" integer,
    "hold_until" timestamptz,
    "status" text NOT NULL,
    "error_message" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "completed_at" timestamptz,
    CONSTRAINT "sidecar_watchdog_actions_pkey" PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE "public"."sidecar_watchdog_pending_actions" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "hold_id" integer,
    "action_history_created_at" timestamptz NOT NULL,
    "action_history_id" integer NOT NULL,
    "auth_id" text,
    "auth_name" text,
    "auth_index" text,
    "provider" text,
    "action_type" text NOT NULL,
    "reason" text,
    "previous_priority" integer,
    "target_priority" integer,
    "hold_until" timestamptz,
    "attempt_count" integer DEFAULT 0 NOT NULL,
    "last_attempt_at" timestamptz,
    "last_error_message" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT "sidecar_watchdog_pending_actions_pkey" PRIMARY KEY (id),
    CONSTRAINT "sidecar_watchdog_pending_actions_attempt_count_check" CHECK (attempt_count >= 0),
    CONSTRAINT "uq_sidecar_watchdog_pending_actions_action_history_key" UNIQUE (action_history_created_at, action_history_id)
);
ALTER TABLE "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_auth_snapshot_id_fkey" FOREIGN KEY (auth_snapshot_id) REFERENCES "public"."sidecar_auth_snapshots"(id) ON DELETE SET NULL;
ALTER TABLE "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_hold_id_fkey" FOREIGN KEY (hold_id) REFERENCES "public"."sidecar_watchdog_holds"(id) ON DELETE SET NULL;

ALTER TABLE ONLY "public"."sidecar_watchdog_pending_actions" ADD CONSTRAINT "sidecar_watchdog_pending_actions_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;

CREATE INDEX "ix_sidecar_watchdog_actions_id" ON "public"."sidecar_watchdog_actions" USING btree (id);
CREATE INDEX "idx_sidecar_watchdog_actions_sidecar_created" ON "public"."sidecar_watchdog_actions" USING btree (sidecar_id, created_at DESC, id DESC);
CREATE INDEX "idx_sidecar_watchdog_pending_actions_sidecar_created" ON "public"."sidecar_watchdog_pending_actions" USING btree (sidecar_id, created_at ASC, id ASC);

ALTER TABLE ONLY "public"."log_retention_settings" ADD COLUMN "sidecar_action_history_retention_days" integer;
ALTER TABLE ONLY "public"."log_retention_settings" ADD CONSTRAINT "log_retention_settings_sidecar_action_history_retention_days_check" CHECK (sidecar_action_history_retention_days IS NULL OR sidecar_action_history_retention_days >= 1);