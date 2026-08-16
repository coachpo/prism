-- Endpoint direct-reference metadata: API-key fingerprint, independent key time and
-- config revision; name/base_url length contract; removal of display-only position.
--
-- This migration is additive-safe except for the deliberate hard-delete of the
-- display-only `position` column and its index, which have no runtime meaning.

-- Preflight: narrowing `name` to 128 Unicode code points must never truncate
-- existing rows. Fail the migration with details instead of silently cutting data.
DO $$
DECLARE
    violation_count bigint;
BEGIN
    SELECT count(*) INTO violation_count
      FROM endpoints
     WHERE char_length(name) > 128;

    IF violation_count > 0 THEN
        RAISE EXCEPTION
            'endpoint name narrowing to 128 characters would truncate % row(s); manual remediation required before upgrading',
            violation_count;
    END IF;
END $$;

-- Lossless widening of base_url (500 -> 512) and guarded narrowing of name
-- (200 -> 128, preflighted above).
ALTER TABLE public.endpoints ALTER COLUMN name TYPE character varying(128);
ALTER TABLE public.endpoints ALTER COLUMN base_url TYPE character varying(512);

-- Secret identity metadata. `api_key_fingerprint` is a 48-bit instance-local
-- display token derived from the plaintext key ("fp_v1_" + 12 hex chars);
-- `api_key_updated_at` records real key-identity changes only (null for
-- historical/unknown); `config_revision` starts at 1 and bumps only when the
-- normalized base URL or the key identity actually changes.
ALTER TABLE public.endpoints
    ADD COLUMN api_key_fingerprint character varying(18),
    ADD COLUMN api_key_updated_at timestamp with time zone,
    ADD COLUMN config_revision bigint NOT NULL DEFAULT 1;

-- Hard-delete the display-only ordering contract. Runtime routing never reads
-- endpoint position; the authoritative target order stays model_access_targets.
ALTER TABLE public.endpoints DROP COLUMN "position";

DROP INDEX IF EXISTS public.idx_endpoints_profile_position;

-- Stable deterministic list order: lower(name), name, id.
CREATE INDEX idx_endpoints_profile_name_lower
    ON public.endpoints USING btree (profile_id, lower(name), name, id);
