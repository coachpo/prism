-- 000032_model_direct_request_enabled
-- Add the explicit client-entry qualification to model configurations.
--
-- This is a data-preserving additive migration. Existing rows are deliberately
-- classified as direct request entries so upgrading a retained instance does
-- not change which model IDs clients can address. The controlled, instance-
-- specific reclassification plan is an operator action after deployment; it is
-- intentionally not encoded in this generic migration.

ALTER TABLE public.model_configs
    ADD COLUMN direct_request_enabled boolean;

UPDATE public.model_configs
SET direct_request_enabled = TRUE
WHERE direct_request_enabled IS NULL;

ALTER TABLE public.model_configs
    ALTER COLUMN direct_request_enabled SET DEFAULT TRUE,
    ALTER COLUMN direct_request_enabled SET NOT NULL;
