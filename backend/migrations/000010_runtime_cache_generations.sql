CREATE TABLE IF NOT EXISTS runtime_cache_generations (
    domain TEXT NOT NULL,
    scope_type TEXT NOT NULL DEFAULT 'global',
    scope_id TEXT NOT NULL DEFAULT '*',
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by TEXT NULL,
    reason TEXT NULL,
    PRIMARY KEY (domain, scope_type, scope_id)
);

CREATE INDEX IF NOT EXISTS idx_runtime_cache_generations_domain_scope
    ON runtime_cache_generations (domain, scope_type, scope_id, version);

INSERT INTO runtime_cache_generations (domain, scope_type, scope_id, version, reason)
VALUES
    ('auth', 'global', '*', 0, 'bootstrap'),
    ('runtime_planning', 'global', '*', 0, 'bootstrap'),
    ('profile_runtime', 'global', '*', 0, 'bootstrap'),
    ('model_catalog', 'global', '*', 0, 'bootstrap')
ON CONFLICT (domain, scope_type, scope_id) DO NOTHING;
