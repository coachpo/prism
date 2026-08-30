-- 000028_model_pi_binding_identity
-- Additive, data-preserving Pi binding identity snapshot.
--
-- Until now `catalog_model_id` carried two meanings at once: the pi.dev
-- directory model id and, because the application gate required it to equal
-- the current Prism `model_id`, the Prism identity that was frozen at bind
-- time. Explicit cross-directory binding separates those two meanings, so the
-- Prism identity gets its own column and the directory coordinate keeps only
-- its own.
--
-- Existing rows were all written under the same-id invariant, so their
-- `catalog_model_id` is exactly the Prism model id that was current when they
-- were bound. Backfilling from `catalog_model_id` therefore reproduces the
-- identity snapshot those rows already assert and preserves every rename that
-- already happened: a row whose `catalog_model_id` no longer equals
-- `model_configs.model_id` stays drifted after the backfill instead of being
-- silently healed. No row is dropped, rewritten, or re-pointed, and no
-- retained model, pricing, or log state is touched.

ALTER TABLE public.model_pi_catalog_bindings
    ADD COLUMN prism_model_id_at_bind character varying(200);

UPDATE public.model_pi_catalog_bindings
    SET prism_model_id_at_bind = catalog_model_id
    WHERE prism_model_id_at_bind IS NULL;

ALTER TABLE public.model_pi_catalog_bindings
    ALTER COLUMN prism_model_id_at_bind SET NOT NULL,
    ADD CONSTRAINT ck_mpcb_prism_model_id_at_bind
        CHECK (prism_model_id_at_bind::text <> ''::text);
