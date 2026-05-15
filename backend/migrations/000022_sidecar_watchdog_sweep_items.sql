-- Clean-break watchdog runtime replacement. Parent sweeps stay authoritative,
-- child sweep items become the executable unit of work, and legacy quota scan
-- runs are demoted to projection/history-only rows.

ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD COLUMN "restart_requested_at" timestamptz;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD COLUMN "restart_target_policy_revision_id" bigint;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD COLUMN "restart_reason" text;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD COLUMN "cancel_requested_at" timestamptz;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD COLUMN "cancel_reason" text;

ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "sidecar_watchdog_sweeps_restart_target_revision_fkey" FOREIGN KEY (restart_target_policy_revision_id) REFERENCES "public"."sidecar_watchdog_policy_revisions"(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "ck_sidecar_watchdog_sweeps_restart_intent" CHECK (restart_requested_at IS NULL OR restart_target_policy_revision_id IS NOT NULL);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "ck_sidecar_watchdog_sweeps_cancel_intent" CHECK (cancel_reason IS NULL OR cancel_requested_at IS NOT NULL);

UPDATE "public"."sidecar_watchdog_sweeps"
SET status = 'cancelled',
    completed_at = COALESCE(completed_at, now()),
    lease_expires_at = NULL,
    cancel_requested_at = COALESCE(cancel_requested_at, now()),
    cancel_reason = COALESCE(cancel_reason, 'legacy_runtime_discarded'),
    failure_reason = COALESCE(failure_reason, 'legacy_runtime_discarded'),
    updated_at = now()
WHERE status IN ('running', 'paused');

UPDATE "public"."sidecar_quota_scan_runs"
SET status = 'cancelled',
    cancel_requested_at = COALESCE(cancel_requested_at, now()),
    completed_at = COALESCE(completed_at, now()),
    last_error_code = COALESCE(last_error_code, 'legacy_runtime_discarded'),
    updated_at = now()
WHERE status IN ('queued', 'running');

DROP INDEX IF EXISTS "public"."uq_sidecar_quota_scan_runs_active_sidecar";
ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" DROP CONSTRAINT "ck_sidecar_quota_scan_runs_status";
ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" ADD CONSTRAINT "ck_sidecar_quota_scan_runs_status" CHECK (status IN ('completed', 'cancelled', 'failed'));
CREATE INDEX "idx_sidecar_quota_scan_runs_sidecar_history" ON "public"."sidecar_quota_scan_runs" USING btree (sidecar_id, created_at DESC, id DESC);
COMMENT ON TABLE "public"."sidecar_quota_scan_runs" IS 'Projection/history only. Executable watchdog work is owned by sidecar_watchdog_sweeps and sidecar_watchdog_sweep_items.';

CREATE TABLE "public"."sidecar_watchdog_sweep_items" (
    "id" BIGSERIAL NOT NULL,
    "sweep_id" text NOT NULL,
    "sidecar_id" integer NOT NULL,
    "policy_revision_id" bigint NOT NULL,
    "item_index" integer NOT NULL,
    "source" text NOT NULL,
    "source_rank" integer NOT NULL,
    "priority" integer DEFAULT 0 NOT NULL,
    "due_at" timestamptz,
    "auth_id" text NOT NULL,
    "auth_index" text,
    "provider" text,
    "hold_id" integer,
    "auth_snapshot_id" integer,
    "selection_json" jsonb DEFAULT '{}'::jsonb NOT NULL,
    "status" text DEFAULT 'queued' NOT NULL,
    "lease_owner" text,
    "lease_expires_at" timestamptz,
    "attempt_token" integer DEFAULT 0 NOT NULL,
    "started_at" timestamptz,
    "completed_at" timestamptz,
    "result_observation_id" integer,
    "last_error_code" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "uq_sidecar_watchdog_sweep_items_sweep_index" UNIQUE (sweep_id, item_index);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_sweep_id_fkey" FOREIGN KEY (sweep_id) REFERENCES "public"."sidecar_watchdog_sweeps"(sweep_id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_policy_revision_id_fkey" FOREIGN KEY (policy_revision_id) REFERENCES "public"."sidecar_watchdog_policy_revisions"(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_hold_id_fkey" FOREIGN KEY (hold_id) REFERENCES "public"."sidecar_watchdog_holds"(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_auth_snapshot_id_fkey" FOREIGN KEY (auth_snapshot_id) REFERENCES "public"."sidecar_auth_snapshots"(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "sidecar_watchdog_sweep_items_result_observation_id_fkey" FOREIGN KEY (result_observation_id) REFERENCES "public"."sidecar_quota_probe_observations"(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "ck_sidecar_watchdog_sweep_items_status" CHECK (status IN ('queued', 'leased', 'succeeded', 'failed', 'cancelled', 'superseded'));
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "ck_sidecar_watchdog_sweep_items_shape" CHECK (
    item_index >= 0
    AND source_rank >= 0
    AND priority >= 0
    AND attempt_token >= 0
    AND btrim(auth_id) <> ''
    AND btrim(source) <> ''
    AND jsonb_typeof(selection_json) = 'object'
);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "ck_sidecar_watchdog_sweep_items_lease" CHECK (
    status <> 'leased'
    OR (lease_owner IS NOT NULL AND btrim(lease_owner) <> '' AND lease_expires_at IS NOT NULL AND attempt_token > 0)
);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweep_items" ADD CONSTRAINT "ck_sidecar_watchdog_sweep_items_completion" CHECK (
    (status IN ('queued', 'leased') AND completed_at IS NULL)
    OR (status IN ('succeeded', 'failed', 'cancelled', 'superseded') AND completed_at IS NOT NULL)
);

CREATE INDEX "idx_sidecar_watchdog_sweep_items_sweep_status" ON "public"."sidecar_watchdog_sweep_items" USING btree (sweep_id, status, item_index);
CREATE INDEX "idx_sidecar_watchdog_sweep_items_claimable" ON "public"."sidecar_watchdog_sweep_items" USING btree (sidecar_id, sweep_id, source_rank, priority DESC, due_at ASC, auth_id ASC, item_index ASC) WHERE status = 'queued';
CREATE INDEX "idx_sidecar_watchdog_sweep_items_leased" ON "public"."sidecar_watchdog_sweep_items" USING btree (lease_expires_at, sidecar_id, sweep_id) WHERE status = 'leased';
CREATE INDEX "idx_sidecar_watchdog_sweep_items_auth" ON "public"."sidecar_watchdog_sweep_items" USING btree (sidecar_id, auth_id, created_at DESC);
