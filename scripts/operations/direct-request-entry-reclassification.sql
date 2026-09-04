\set ON_ERROR_STOP on

\if :{?apply_token}
\else
  \set apply_token preview
\endif
\if :{?backup_token}
\else
  \set backup_token unverified
\endif
\if :{?quiesce_token}
\else
  \set quiesce_token running
\endif
\if :{?expected_database}
\else
  \set expected_database ''
\endif

SELECT :'apply_token' = 'APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1' AS apply_requested \gset

CREATE TEMP TABLE expected_models (
  model_id text PRIMARY KEY,
  direct_request_enabled boolean NOT NULL
) ON COMMIT PRESERVE ROWS;

INSERT INTO expected_models (model_id, direct_request_enabled) VALUES
  ('codex/codex-auto-review', true),
  ('codex/gpt-5.6-terra', true),
  ('deepseek-v4-flash', true),
  ('deepseek-v4-pro', true),
  ('glm-5.3-flash', true),
  ('codex/gpt-image-2', true),
  ('codex/gpt-5.4-mini', true),
  ('codex/gpt-5.5', true),
  ('codex/gpt-5.6-luna', true),
  ('gpt-5.6-luna', true),
  ('muse-spark-1.2-contributor', true),
  ('qwen3.8-flash', true),
  ('DeepSeek-V4-Flash', false),
  ('deepseek/deepseek-v4-flash-0731', false),
  ('deepseek/deepseek-v4-pro', false),
  ('z-ai/glm-5.3-flash', false);

CREATE TEMP TABLE expected_edges (
  parent_model_id text NOT NULL,
  child_model_id text NOT NULL,
  append_order integer NOT NULL,
  PRIMARY KEY (parent_model_id, child_model_id),
  UNIQUE (parent_model_id, append_order)
) ON COMMIT PRESERVE ROWS;

INSERT INTO expected_edges (parent_model_id, child_model_id, append_order) VALUES
  ('deepseek-v4-flash', 'DeepSeek-V4-Flash', 1),
  ('deepseek-v4-flash', 'deepseek/deepseek-v4-flash-0731', 2),
  ('deepseek-v4-pro', 'deepseek/deepseek-v4-pro', 1),
  ('glm-5.3-flash', 'z-ai/glm-5.3-flash', 1);

CREATE TEMP TABLE expected_flash_outbound (
  owner_model_id text PRIMARY KEY,
  upstream_model_id text NOT NULL
) ON COMMIT PRESERVE ROWS;

INSERT INTO expected_flash_outbound (owner_model_id, upstream_model_id) VALUES
  ('deepseek-v4-flash', 'deepseek-v4-flash'),
  ('DeepSeek-V4-Flash', 'DeepSeek-V4-Flash'),
  ('deepseek/deepseek-v4-flash-0731', 'deepseek/deepseek-v4-flash-0731');

CREATE TEMP TABLE reclassification_control AS
SELECT
  :'apply_token' = 'APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1' AS apply_requested,
  :'backup_token' = 'BACKUP_VERIFIED' AS backup_confirmed,
  :'quiesce_token' = 'PRISM_STOPPED' AS quiesce_confirmed,
  current_database() = :'expected_database' AS database_confirmed;

CREATE TEMP TABLE baseline_model_enablement (
  id integer PRIMARY KEY,
  is_enabled boolean NOT NULL
) ON COMMIT PRESERVE ROWS;

CREATE TEMP TABLE baseline_access_targets (
  id integer PRIMARY KEY,
  row_snapshot text NOT NULL
) ON COMMIT PRESERVE ROWS;

CREATE TEMP TABLE reclassification_changes (
  change_kind text NOT NULL,
  row_id integer NOT NULL
) ON COMMIT PRESERVE ROWS;

\if :apply_requested
BEGIN ISOLATION LEVEL SERIALIZABLE READ WRITE;
\else
BEGIN ISOLATION LEVEL SERIALIZABLE READ ONLY;
\endif

\if :apply_requested
DO $$
BEGIN
  PERFORM id
  FROM model_configs
  WHERE profile_id = 1
  ORDER BY id
  FOR UPDATE;

  PERFORM id
  FROM model_access_targets
  WHERE profile_id = 1
  ORDER BY source_model_config_id, position, id
  FOR UPDATE;
END
$$;
\endif

INSERT INTO baseline_model_enablement (id, is_enabled)
SELECT id, is_enabled
FROM model_configs
WHERE profile_id = 1;

INSERT INTO baseline_access_targets (id, row_snapshot)
SELECT id, to_jsonb(target_row)::text AS row_snapshot
FROM model_access_targets AS target_row
WHERE profile_id = 1;

DO $$
DECLARE
  control reclassification_control%ROWTYPE;
BEGIN
  SELECT * INTO STRICT control FROM reclassification_control;

  IF control.apply_requested AND NOT control.backup_confirmed THEN
    RAISE EXCEPTION 'apply refused: pass -v backup_token=BACKUP_VERIFIED only after validating a current backup';
  END IF;
  IF control.apply_requested AND NOT control.quiesce_confirmed THEN
    RAISE EXCEPTION 'apply refused: stop Prism while keeping PostgreSQL available, then pass -v quiesce_token=PRISM_STOPPED';
  END IF;
  IF control.apply_requested AND NOT control.database_confirmed THEN
    RAISE EXCEPTION 'apply refused: -v expected_database must exactly match current_database()';
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'model_configs'
      AND column_name = 'direct_request_enabled'
      AND data_type = 'boolean'
      AND is_nullable = 'NO'
      AND column_default = 'true'
  ) THEN
    RAISE EXCEPTION '000032 prerequisite missing: model_configs.direct_request_enabled must be BOOLEAN NOT NULL DEFAULT TRUE';
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM prism_schema_migrations
    WHERE version = '000032_model_direct_request_enabled'
  ) THEN
    RAISE EXCEPTION '000032 prerequisite missing: migration history does not contain 000032_model_direct_request_enabled';
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM profiles
    WHERE id = 1 AND is_default = true AND deleted_at IS NULL
  ) THEN
    RAISE EXCEPTION 'profile 1 is not the live Default profile';
  END IF;

  IF (SELECT count(*) FROM model_configs WHERE profile_id = 1) <> 16 THEN
    RAISE EXCEPTION 'inventory conflict: Default profile must contain exactly 16 ModelConfig rows';
  END IF;

  IF EXISTS (
    (SELECT model_id FROM model_configs WHERE profile_id = 1
     EXCEPT SELECT model_id FROM expected_models)
    UNION ALL
    (SELECT model_id FROM expected_models
     EXCEPT SELECT model_id FROM model_configs WHERE profile_id = 1)
  ) THEN
    RAISE EXCEPTION 'inventory conflict: exact case-sensitive 16-model set differs from the accepted set';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_edges AS expected
    JOIN model_configs AS parent
      ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
    JOIN model_configs AS child
      ON child.profile_id = 1 AND child.model_id = expected.child_model_id
    WHERE parent.profile_id <> child.profile_id
       OR parent.api_family <> child.api_family
       OR parent.openai_accepted_format IS DISTINCT FROM child.openai_accepted_format
       OR parent.openai_image_operations IS DISTINCT FROM child.openai_image_operations
  ) THEN
    RAISE EXCEPTION 'mapping conflict: parent and child profile/family/OpenAI dimensions must match exactly';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_edges AS expected
    JOIN model_configs AS parent
      ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
    JOIN model_configs AS child
      ON child.profile_id = 1 AND child.model_id = expected.child_model_id
    JOIN model_access_targets AS target
      ON target.profile_id = 1
     AND target.source_model_config_id = parent.id
     AND target.target_model_config_id = child.id
    GROUP BY expected.parent_model_id, expected.child_model_id
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'mapping conflict: a required parent-to-child Model Target is duplicated';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_edges AS expected
    JOIN model_configs AS parent
      ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
    JOIN model_configs AS child
      ON child.profile_id = 1 AND child.model_id = expected.child_model_id
    JOIN model_access_targets AS target
      ON target.profile_id = 1
     AND target.source_model_config_id = parent.id
     AND target.target_model_config_id = child.id
    WHERE target.target_type <> 'model' OR target.is_enabled = false
  ) THEN
    RAISE EXCEPTION 'mapping conflict: an existing required edge is not an enabled Model Target';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM (
      SELECT
        target.source_model_config_id,
        count(*) AS row_count,
        count(DISTINCT target.position) AS distinct_positions,
        min(target.position) AS minimum_position,
        max(target.position) AS maximum_position
      FROM model_access_targets AS target
      JOIN model_configs AS parent ON parent.id = target.source_model_config_id
      WHERE target.profile_id = 1
        AND parent.model_id IN (SELECT DISTINCT parent_model_id FROM expected_edges)
      GROUP BY target.source_model_config_id
    ) AS positions
    WHERE positions.minimum_position <> 0
       OR positions.maximum_position <> positions.row_count - 1
       OR positions.distinct_positions <> positions.row_count
  ) THEN
    RAISE EXCEPTION 'position conflict: each affected parent must have dense unique positions 0..N-1';
  END IF;

  IF EXISTS (
    WITH RECURSIVE prospective_edges(source_id, target_id) AS (
      SELECT source_model_config_id, target_model_config_id
      FROM model_access_targets
      WHERE profile_id = 1 AND target_model_config_id IS NOT NULL
      UNION
      SELECT parent.id, child.id
      FROM expected_edges AS expected
      JOIN model_configs AS parent
        ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
      JOIN model_configs AS child
        ON child.profile_id = 1 AND child.model_id = expected.child_model_id
    ), walk(origin_id, node_id, visited, cycle) AS (
      SELECT source_id, target_id, ARRAY[source_id, target_id], source_id = target_id
      FROM prospective_edges
      UNION ALL
      SELECT walk.origin_id,
             edge.target_id,
             walk.visited || edge.target_id,
             edge.target_id = ANY(walk.visited)
      FROM walk
      JOIN prospective_edges AS edge ON edge.source_id = walk.node_id
      WHERE walk.cycle = false AND cardinality(walk.visited) <= 16
    )
    SELECT 1 FROM walk WHERE cycle LIMIT 1
  ) THEN
    RAISE EXCEPTION 'graph conflict: prospective Model Target graph contains a cycle';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_flash_outbound AS expected
    JOIN model_configs AS owner
      ON owner.profile_id = 1 AND owner.model_id = expected.owner_model_id
    WHERE owner.is_enabled = false
       OR NOT EXISTS (
         SELECT 1
         FROM model_access_targets AS target
         JOIN connections AS connection
           ON connection.id = target.target_connection_id
          AND connection.profile_id = 1
         WHERE target.profile_id = 1
           AND target.source_model_config_id = owner.id
           AND target.target_type = 'connection'
           AND target.is_enabled = true
           AND connection.is_active = true
           AND connection.upstream_model_id = expected.upstream_model_id
       )
  ) THEN
    RAISE EXCEPTION 'DeepSeek identity conflict: each enabled Flash logical model must retain its exact active upstream identity';
  END IF;
END
$$;

SELECT
  model.id,
  model.model_id,
  model.api_family,
  model.openai_accepted_format,
  model.openai_image_operations,
  model.is_enabled,
  model.direct_request_enabled AS current_direct,
  expected.direct_request_enabled AS desired_direct,
  model.direct_request_enabled IS DISTINCT FROM expected.direct_request_enabled AS will_update
FROM expected_models AS expected
JOIN model_configs AS model
  ON model.profile_id = 1 AND model.model_id = expected.model_id
ORDER BY model.id;

SELECT
  expected.parent_model_id,
  expected.child_model_id,
  target.id AS existing_target_id,
  target.position AS existing_position,
  target.is_enabled AS existing_enabled,
  target.id IS NULL AS will_append
FROM expected_edges AS expected
JOIN model_configs AS parent
  ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
JOIN model_configs AS child
  ON child.profile_id = 1 AND child.model_id = expected.child_model_id
LEFT JOIN model_access_targets AS target
  ON target.profile_id = 1
 AND target.source_model_config_id = parent.id
 AND target.target_model_config_id = child.id
ORDER BY expected.parent_model_id, expected.append_order;

SELECT
  expected.owner_model_id,
  expected.upstream_model_id,
  connection.id AS terminal_target_id,
  connection.is_active,
  target.position,
  target.is_enabled
FROM expected_flash_outbound AS expected
JOIN model_configs AS owner
  ON owner.profile_id = 1 AND owner.model_id = expected.owner_model_id
JOIN model_access_targets AS target
  ON target.profile_id = 1
 AND target.source_model_config_id = owner.id
 AND target.target_type = 'connection'
JOIN connections AS connection
  ON connection.id = target.target_connection_id
 AND connection.profile_id = 1
 AND connection.upstream_model_id = expected.upstream_model_id
ORDER BY expected.owner_model_id, target.position, target.id;

\if :apply_requested
WITH updated AS (
  UPDATE model_configs AS model
  SET direct_request_enabled = expected.direct_request_enabled
  FROM expected_models AS expected
  WHERE model.profile_id = 1
    AND model.model_id = expected.model_id
    AND model.direct_request_enabled IS DISTINCT FROM expected.direct_request_enabled
  RETURNING model.id
)
INSERT INTO reclassification_changes (change_kind, row_id)
SELECT 'model_qualification', id FROM updated;

WITH missing AS (
  SELECT
    parent.id AS parent_id,
    child.id AS child_id,
    COALESCE((
      SELECT max(existing.position)
      FROM model_access_targets AS existing
      WHERE existing.profile_id = 1
        AND existing.source_model_config_id = parent.id
    ), -1)
    + row_number() OVER (
        PARTITION BY parent.id
        ORDER BY expected.append_order
      )::integer AS append_position
  FROM expected_edges AS expected
  JOIN model_configs AS parent
    ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
  JOIN model_configs AS child
    ON child.profile_id = 1 AND child.model_id = expected.child_model_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM model_access_targets AS existing
    WHERE existing.profile_id = 1
      AND existing.source_model_config_id = parent.id
      AND existing.target_model_config_id = child.id
  )
), inserted AS (
  INSERT INTO model_access_targets (
    profile_id,
    source_model_config_id,
    target_type,
    target_model_config_id,
    position,
    is_enabled,
    created_at,
    updated_at
  )
  SELECT
    1,
    parent_id,
    'model',
    child_id,
    append_position,
    true,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  FROM missing
  ORDER BY parent_id, append_position
  RETURNING id
)
INSERT INTO reclassification_changes (change_kind, row_id)
SELECT 'model_target_append', id FROM inserted;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM baseline_model_enablement AS baseline
    JOIN model_configs AS current_model USING (id)
    WHERE current_model.is_enabled IS DISTINCT FROM baseline.is_enabled
  ) THEN
    RAISE EXCEPTION 'postcondition failed: model is_enabled changed';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM baseline_access_targets AS baseline
    LEFT JOIN model_access_targets AS current_target USING (id)
    WHERE current_target.id IS NULL
       OR to_jsonb(current_target)::text IS DISTINCT FROM baseline.row_snapshot
  ) THEN
    RAISE EXCEPTION 'postcondition failed: an existing access target changed';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_models AS expected
    JOIN model_configs AS model
      ON model.profile_id = 1 AND model.model_id = expected.model_id
    WHERE model.direct_request_enabled IS DISTINCT FROM expected.direct_request_enabled
  ) THEN
    RAISE EXCEPTION 'postcondition failed: direct-entry exact set was not written';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM expected_edges AS expected
    JOIN model_configs AS parent
      ON parent.profile_id = 1 AND parent.model_id = expected.parent_model_id
    JOIN model_configs AS child
      ON child.profile_id = 1 AND child.model_id = expected.child_model_id
    LEFT JOIN model_access_targets AS target
      ON target.profile_id = 1
     AND target.source_model_config_id = parent.id
     AND target.target_model_config_id = child.id
     AND target.target_type = 'model'
     AND target.is_enabled = true
    GROUP BY expected.parent_model_id, expected.child_model_id
    HAVING count(target.id) <> 1
  ) THEN
    RAISE EXCEPTION 'postcondition failed: required parent-to-child edges are not unique and enabled';
  END IF;

  IF EXISTS (SELECT 1 FROM reclassification_changes) THEN
    INSERT INTO runtime_cache_generations (
      domain, scope_type, scope_id, version, updated_at, updated_by, reason
    ) VALUES
      ('profile_runtime', 'global', '*', 1, CURRENT_TIMESTAMP, 'operator:direct-entry-reclassification', 'controlled direct-entry reclassification'),
      ('runtime_planning', 'global', '*', 1, CURRENT_TIMESTAMP, 'operator:direct-entry-reclassification', 'controlled direct-entry reclassification'),
      ('profile_runtime', 'profile', '1', 1, CURRENT_TIMESTAMP, 'operator:direct-entry-reclassification', 'controlled direct-entry reclassification'),
      ('runtime_planning', 'profile', '1', 1, CURRENT_TIMESTAMP, 'operator:direct-entry-reclassification', 'controlled direct-entry reclassification')
    ON CONFLICT (domain, scope_type, scope_id) DO UPDATE
    SET version = runtime_cache_generations.version + 1,
        updated_at = EXCLUDED.updated_at,
        updated_by = EXCLUDED.updated_by,
        reason = EXCLUDED.reason;

    INSERT INTO route_witness_generations (profile_id, generation, updated_at)
    VALUES (1, 1, CURRENT_TIMESTAMP)
    ON CONFLICT (profile_id) DO UPDATE
    SET generation = route_witness_generations.generation + 1,
        updated_at = EXCLUDED.updated_at;
  END IF;
END
$$;

SELECT change_kind, count(*) AS changed_rows
FROM reclassification_changes
GROUP BY change_kind
ORDER BY change_kind;

COMMIT;
\else
ROLLBACK;
\endif
