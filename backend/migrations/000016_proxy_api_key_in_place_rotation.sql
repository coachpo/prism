-- 000016_proxy_api_key_in_place_rotation
-- Rotation becomes an in-place secret replacement on the same proxy_api_keys
-- row instead of a lineage-creating successor insert. The row id is the stable
-- identity of a logical key: request_logs and usage_request_events attribute
-- through proxy_api_key_id_snapshot (000005/000006), so a rotation that changed
-- the id split one logical key's usage history into two unrelated ids.
--
-- rotated_from_id is replaced by rotation metadata carried on the row itself:
-- rotated_at is the most recent rotation instant (NULL before the first
-- rotation) and rotation_count is how many times the secret has been replaced.
--
-- Backfill preserves what the existing lineage still proves. Each chain head
-- inherits its visible ancestor depth as rotation_count and its own created_at
-- as rotated_at, because a successor row was inserted at the rotation instant.
-- The depth is a lower bound: rotated_from_id was ON DELETE SET NULL, so a
-- deleted predecessor already truncated the chain before this migration ran.
--
-- Historical predecessor rows are deliberately left in place. They are ordinary
-- inactive, expired keys after this migration and are not deleted here; nothing
-- in the product depends on them any more, so they can be removed manually from
-- the proxy-key ledger.

ALTER TABLE public.proxy_api_keys
    ADD COLUMN IF NOT EXISTS rotated_at timestamp with time zone;

ALTER TABLE public.proxy_api_keys
    ADD COLUMN IF NOT EXISTS rotation_count integer NOT NULL DEFAULT 0;

WITH RECURSIVE chain AS (
    SELECT id AS head_id, rotated_from_id AS ancestor_id, 1 AS depth
    FROM public.proxy_api_keys
    WHERE rotated_from_id IS NOT NULL
    UNION ALL
    SELECT c.head_id, ancestor.rotated_from_id, c.depth + 1
    FROM chain c
    JOIN public.proxy_api_keys ancestor ON ancestor.id = c.ancestor_id
    WHERE ancestor.rotated_from_id IS NOT NULL
), depths AS (
    SELECT head_id, MAX(depth) AS depth
    FROM chain
    GROUP BY head_id
)
UPDATE public.proxy_api_keys AS keys
SET rotation_count = depths.depth,
    rotated_at = keys.created_at
FROM depths
WHERE keys.id = depths.head_id;

ALTER TABLE public.proxy_api_keys
    DROP CONSTRAINT IF EXISTS proxy_api_keys_rotated_from_id_fkey;

ALTER TABLE public.proxy_api_keys
    DROP COLUMN IF EXISTS rotated_from_id;

-- rotation_count and rotated_at are two views of one fact: a key has either
-- never rotated (0 / NULL) or carries both a count and the latest instant.
ALTER TABLE public.proxy_api_keys
    ADD CONSTRAINT ck_proxy_api_keys_rotation_metadata_consistent
    CHECK (
        (rotation_count = 0 AND rotated_at IS NULL)
        OR
        (rotation_count > 0 AND rotated_at IS NOT NULL)
    );
