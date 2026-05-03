ALTER TABLE model_configs
    ADD COLUMN proxy_selection_strategy varchar(50);

ALTER TABLE model_proxy_targets
    ADD COLUMN weight integer,
    ADD COLUMN target_priority integer;

UPDATE model_configs
SET proxy_selection_strategy = 'ordered_fallback'
WHERE model_type = 'proxy'
  AND proxy_selection_strategy IS NULL;

UPDATE model_proxy_targets
SET weight = COALESCE(weight, 1),
    target_priority = COALESCE(target_priority, position)
WHERE weight IS NULL
   OR target_priority IS NULL;

ALTER TABLE model_proxy_targets
    ALTER COLUMN weight SET NOT NULL,
    ALTER COLUMN target_priority SET NOT NULL;

ALTER TABLE ONLY model_configs
    DROP CONSTRAINT IF EXISTS chk_model_configs_strategy_attachment;

ALTER TABLE ONLY model_configs
    ADD CONSTRAINT chk_model_configs_proxy_selection_strategy_enum
        CHECK (
            proxy_selection_strategy IS NULL
            OR proxy_selection_strategy IN ('ordered_fallback', 'weighted_static', 'priority_static')
        ),
    ADD CONSTRAINT chk_model_configs_strategy_attachment
        CHECK (
            model_type = 'native' AND loadbalance_strategy_id IS NOT NULL AND proxy_selection_strategy IS NULL
            OR model_type = 'proxy' AND loadbalance_strategy_id IS NULL AND proxy_selection_strategy IS NOT NULL
        );

ALTER TABLE ONLY model_proxy_targets
    ADD CONSTRAINT chk_model_proxy_targets_weight_min
        CHECK (weight >= 1),
    ADD CONSTRAINT chk_model_proxy_targets_target_priority_min
        CHECK (target_priority >= 0);
