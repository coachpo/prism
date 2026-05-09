CREATE TABLE "public"."management_stat_buckets" (
    "bucket_start" timestamptz NOT NULL,
    "bucket_size" text NOT NULL,
    "metric" text NOT NULL,
    "dimension_key" text NOT NULL DEFAULT '',
    "dimension_value" text NOT NULL DEFAULT '',
    "value" numeric NOT NULL,
    "source_high_water_mark" timestamptz NOT NULL,
    "generated_at" timestamptz NOT NULL,
    CONSTRAINT "management_stat_buckets_pkey" PRIMARY KEY (bucket_start, bucket_size, metric, dimension_key, dimension_value)
);

CREATE TABLE "public"."management_stat_refresh_state" (
    "job_name" text PRIMARY KEY,
    "last_source_high_water_mark" timestamptz NOT NULL,
    "last_success_at" timestamptz,
    "last_error" text,
    "updated_at" timestamptz NOT NULL
);

CREATE INDEX "idx_management_stat_buckets_dashboard_profile" ON "public"."management_stat_buckets" USING btree (dimension_key, dimension_value, bucket_size, metric);

CREATE TABLE "public"."management_jobs" (
    "id" text PRIMARY KEY,
    "type" text NOT NULL,
    "state" text NOT NULL,
    "requested_by" text NOT NULL,
    "requested_at" timestamptz NOT NULL,
    "started_at" timestamptz,
    "finished_at" timestamptz,
    "priority" text NOT NULL DEFAULT 'maintenance',
    "idempotency_key" text,
    "profile_id" integer NOT NULL,
    "scope_json" jsonb NOT NULL,
    "reason" text NOT NULL,
    "rows_matched_estimate" bigint,
    "rows_deleted" bigint NOT NULL DEFAULT 0,
    "batches_completed" bigint NOT NULL DEFAULT 0,
    "progress_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
    "cancel_requested" boolean NOT NULL DEFAULT false,
    "attempt_count" integer NOT NULL DEFAULT 0,
    "max_attempts" integer NOT NULL DEFAULT 8,
    "next_attempt_at" timestamptz NOT NULL DEFAULT now(),
    "locked_by" text,
    "locked_until" timestamptz,
    "last_heartbeat_at" timestamptz,
    "error_code" text,
    "error_message" text,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT "management_jobs_type_check" CHECK (type IN ('audit_delete', 'log_retention')),
    CONSTRAINT "management_jobs_state_check" CHECK (state IN ('queued', 'running', 'cancel_requested', 'cancelled', 'succeeded', 'failed')),
    CONSTRAINT "management_jobs_attempts_check" CHECK (attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts)
);

CREATE UNIQUE INDEX "idx_management_jobs_idempotency" ON "public"."management_jobs" USING btree (type, requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX "idx_management_jobs_type_state_updated" ON "public"."management_jobs" USING btree (type, state, updated_at DESC);
CREATE INDEX "idx_management_jobs_due" ON "public"."management_jobs" USING btree (next_attempt_at, created_at, id) WHERE state IN ('queued', 'running');

CREATE TABLE "public"."management_job_events" (
    "id" bigserial PRIMARY KEY,
    "job_id" text NOT NULL REFERENCES management_jobs(id) ON DELETE CASCADE,
    "event_type" text NOT NULL,
    "message" text NOT NULL DEFAULT '',
    "rows_deleted" bigint NOT NULL DEFAULT 0,
    "created_at" timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX "idx_management_job_events_job_created" ON "public"."management_job_events" USING btree (job_id, created_at, id);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'request_log_id')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_request_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, request_log_id, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'vendor_id')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_vendor_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, vendor_id, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'model_id')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_model_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, model_id, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'response_status')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_status_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, response_status, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'endpoint_id')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_endpoint_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, endpoint_id, created_at DESC, id DESC);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'connection_id')
        AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_logs' AND column_name = 'created_at') THEN
        CREATE INDEX "idx_audit_logs_profile_connection_created_id_desc" ON "public"."audit_logs" USING btree (profile_id, connection_id, created_at DESC, id DESC);
    END IF;
END $$;
