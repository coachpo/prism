-- 000029_pricing_catalog_source_live_uniqueness
-- Data-preserving index correction for source-linked pricing templates. The
-- 000024 uniqueness guard only required both catalog coordinates to be present,
-- so a soft-deleted template kept claiming its offering coordinates forever and
-- the same models.dev offering could never be imported again. Uniqueness is a
-- statement about live templates, so the guard now also excludes deleted rows.
--
-- Rebuilding the index scans pricing_templates, but no application row is
-- rewritten or removed: the template, its append-only revisions, its cards,
-- and every historical reference survive untouched, and the retired template
-- keeps its coordinates as provenance. Re-importing the same offering
-- afterwards creates a NEW live template instead of failing.

DROP INDEX IF EXISTS public.uq_pricing_templates_catalog_offering;

CREATE UNIQUE INDEX uq_pricing_templates_catalog_offering
    ON public.pricing_templates (catalog_provider_id, catalog_model_id)
    WHERE catalog_provider_id IS NOT NULL
      AND catalog_model_id IS NOT NULL
      AND deleted_at IS NULL;
