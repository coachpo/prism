-- Routing policy strategy defaults and load-balance event identity.
--
-- Strategy defaults:
--   * loadbalance_strategies.is_default (exactly one default per profile in
--     steady state, enforced by a partial unique index);
--   * one-time canonical-only backfill: only an exact full-payload match of
--     the canonical "Default fill-first routing" row may become the default;
--     a name conflict with an incompatible subtype/payload fails the whole
--     migration with actionable detail (profile id, strategy id/name/type).
--   * the partial unique index is created after the backfill so the backfill
--     itself cannot trip it.
--
-- Event identity:
--   * nullable admission_reason and nullable numeric model_config_id are added
--     to the partitioned loadbalance_events parent (cascades to partitions);
--   * legacy rows are normalized (stale failure_kind on admission rows and
--     stale admission_reason on non-admission rows become NULL) and are never
--     guessed from neighbouring rows or current configuration;
--   * CHECK constraints enforce the writer invariants while still allowing
--     legacy NULL evidence.

ALTER TABLE loadbalance_strategies
    ADD COLUMN is_default boolean NOT NULL DEFAULT false;

DO $$
DECLARE
    profile_record RECORD;
    matching_id INTEGER;
    name_occupied BOOLEAN;
    conflict_record RECORD;
BEGIN
    FOR profile_record IN
        SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC
    LOOP
        SELECT id INTO matching_id
        FROM loadbalance_strategies
        WHERE profile_id = profile_record.id
          AND name = 'Default fill-first routing'
          AND legacy_strategy_type = 'fill-first'
          AND failure_status_codes = ARRAY[403,422,429,500,502,503,504,529]::integer[]
          AND ban_mode = 'off'
          AND retry_base_delay_ms = 60000
          AND retry_backoff_multiplier = 2.0
          AND retry_jitter_ratio = 0.2
          AND retry_max_delay_ms = 900000
          AND cycle_retry_attempt_limit = 3
          AND ban_cumulative_retry_attempt_threshold = 0
          AND ban_duration_seconds = 0
        LIMIT 1;

        IF matching_id IS NOT NULL THEN
            UPDATE loadbalance_strategies SET is_default = TRUE WHERE id = matching_id;
            CONTINUE;
        END IF;

        SELECT EXISTS (
            SELECT 1 FROM loadbalance_strategies
            WHERE profile_id = profile_record.id AND name = 'Default fill-first routing'
        ) INTO name_occupied;

        IF NOT name_occupied THEN
            INSERT INTO loadbalance_strategies (
                profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
                retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
                retry_max_delay_ms, cycle_retry_attempt_limit,
                ban_cumulative_retry_attempt_threshold, ban_duration_seconds,
                created_at, updated_at, is_default
            ) VALUES (
                profile_record.id, 'Default fill-first routing', 'fill-first',
                ARRAY[403,422,429,500,502,503,504,529]::integer[], 'off',
                60000, 2.0, 0.2, 900000, 3, 0, 0,
                now(), now(), TRUE
            );
        ELSE
            SELECT id, name, legacy_strategy_type INTO conflict_record
            FROM loadbalance_strategies
            WHERE profile_id = profile_record.id AND name = 'Default fill-first routing'
            LIMIT 1;
            RAISE EXCEPTION
                'canonical_default_strategy_conflict profile_id=%, strategy_id=%, strategy_name=%, strategy_type=%: rename or restore the canonical payload then retry',
                profile_record.id, conflict_record.id, conflict_record.name, conflict_record.legacy_strategy_type;
        END IF;
    END LOOP;
END $$;

CREATE UNIQUE INDEX loadbalance_strategies_one_default_per_profile_idx
    ON loadbalance_strategies (profile_id)
    WHERE is_default;

-- Persisted admission reason and numeric model-row identity for events.
ALTER TABLE loadbalance_events
    ADD COLUMN admission_reason character varying(32);

ALTER TABLE loadbalance_events
    ADD COLUMN model_config_id bigint;

-- Legacy normalization: stale failure_kind on admission rows and stale
-- admission_reason on non-admission rows become NULL; never guessed.
UPDATE loadbalance_events
   SET failure_kind = NULL
 WHERE event_type = 'admission_rejected' AND failure_kind IS NOT NULL;

UPDATE loadbalance_events
   SET admission_reason = NULL
 WHERE event_type <> 'admission_rejected' AND admission_reason IS NOT NULL;

ALTER TABLE loadbalance_events
    ADD CONSTRAINT chk_loadbalance_events_admission_reason
    CHECK (admission_reason IS NULL OR admission_reason IN ('qps_limit', 'max_in_flight_stream', 'max_in_flight_non_stream'));

ALTER TABLE loadbalance_events
    ADD CONSTRAINT chk_loadbalance_events_admission_reason_scope
    CHECK (event_type = 'admission_rejected' OR admission_reason IS NULL);

ALTER TABLE loadbalance_events
    ADD CONSTRAINT chk_loadbalance_events_admission_failure_kind_null
    CHECK (event_type <> 'admission_rejected' OR failure_kind IS NULL);

ALTER TABLE loadbalance_events
    ADD CONSTRAINT chk_loadbalance_events_model_config_id_pair
    CHECK (model_config_id IS NULL OR (model_config_id > 0 AND model_id IS NOT NULL));
