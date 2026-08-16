-- 000012_auth_session_control
-- Additive operator-auth session control columns on the app-auth singleton.
--
-- effective_auth_generation is a monotonic positive decimal used as the
-- canonical auth-mode/session generation shared by tagged PublicAuthStatus,
-- refresh outcomes, cross-tab payload fences and setup projections.
--
-- auth_transition_state holds a real fail-closed/rollback/enforced-disable
-- transition while the settings writer publishes a new effective mode; the
-- typed 503 problem codes (auth_transition_in_progress /
-- auth_transition_recovery_required) and the bounded public operation-status
-- route are backed by these columns, never by client guessing.
--
-- Additive only: no column is dropped or repurposed.

ALTER TABLE public.app_auth_settings
    ADD COLUMN effective_auth_generation BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN auth_transition_state TEXT NULL,
    ADD COLUMN auth_transition_operation_id UUID NULL,
    ADD COLUMN auth_transition_retry_after_at TIMESTAMP WITH TIME ZONE NULL,
    ADD COLUMN auth_transition_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE public.app_auth_settings
    ADD CONSTRAINT ck_app_auth_settings_effective_generation_positive
        CHECK (effective_auth_generation >= 1);

ALTER TABLE public.app_auth_settings
    ADD CONSTRAINT ck_app_auth_settings_transition_state
        CHECK (
            auth_transition_state IS NULL OR
            auth_transition_state IN ('enabling_fail_closed', 'rollback_required', 'disabling_enforced')
        );

ALTER TABLE public.app_auth_settings
    ADD CONSTRAINT ck_app_auth_settings_transition_shape
        CHECK (
            (auth_transition_state IS NULL AND auth_transition_operation_id IS NULL) OR
            (auth_transition_state IS NOT NULL AND auth_transition_operation_id IS NOT NULL)
        );
