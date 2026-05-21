-- Hard-delete retained sidecar auth snapshots now that CLIProxyAPI live
-- auth-file rows are the mutation authority. Historical migrations remain
-- intact; this terminal cleanup removes the snapshot table and any legacy
-- snapshot-id references left by partially migrated local databases.

ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_actions"
    DROP CONSTRAINT IF EXISTS "sidecar_watchdog_actions_auth_snapshot_id_fkey";
ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_sweep_items"
    DROP CONSTRAINT IF EXISTS "sidecar_watchdog_sweep_items_auth_snapshot_id_fkey";

ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_actions"
    DROP COLUMN IF EXISTS "auth_snapshot_id";
ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_sweep_items"
    DROP COLUMN IF EXISTS "auth_snapshot_id";

ALTER TABLE IF EXISTS ONLY "public"."sidecar_auth_snapshots"
    DROP CONSTRAINT IF EXISTS "uq_sidecar_auth_snapshots_key";
DROP INDEX IF EXISTS "public"."idx_sidecar_auth_snapshots_sidecar_id";
DROP TABLE IF EXISTS "public"."sidecar_auth_snapshots";
