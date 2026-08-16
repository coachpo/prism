-- 000017_openai_image_operations
-- OpenAI image generation and editing is modelled as a dimension that is
-- independent of the text wire-format dimension. `openai_accepted_format` and
-- `openai_text_capability` describe which text protocol a row speaks (Chat
-- Completions versus Responses); they never describe image support, and the
-- runtime text gate already ignores non-text operations by construction.
--
-- Image models such as `gpt-image-2` speak no text protocol at all, which the
-- previous CHECK constraints made unrepresentable: they required every `openai`
-- row to carry a non-null text mode. This migration adds the parallel image
-- dimension and relaxes both constraints from "a text mode is required for
-- openai" to "at least one openai dimension is present for openai".
--
-- NULL means the row does not serve that dimension. No `none` literal is
-- introduced: the non-openai branch already uses NULL for "not applicable", and
-- a second spelling of absence would split that meaning across two values. An
-- openai row that carries neither dimension is rejected, so NULL never has to
-- stand in for "the author forgot to choose".
--
-- The migration is additive and rewrites no data. Every existing row carries a
-- non-null text mode and a null image dimension, so all existing rows satisfy
-- the relaxed constraints unchanged. The constraints are renamed because they
-- now govern both dimensions rather than the text dimension alone.

ALTER TABLE public.model_configs
    ADD COLUMN IF NOT EXISTS openai_image_operations text;

ALTER TABLE public.connections
    ADD COLUMN IF NOT EXISTS openai_image_capability text;

ALTER TABLE public.model_configs
    DROP CONSTRAINT IF EXISTS ck_model_configs_openai_accepted_format;

ALTER TABLE public.model_configs
    ADD CONSTRAINT ck_model_configs_openai_dimensions CHECK (
        ((((api_family)::text = 'openai'::text)
            AND ((openai_accepted_format IS NULL) OR (openai_accepted_format = ANY (ARRAY['responses_only'::text, 'chat_completions_only'::text, 'dual_native'::text])))
            AND ((openai_image_operations IS NULL) OR (openai_image_operations = ANY (ARRAY['generations'::text, 'edits'::text, 'generations_and_edits'::text])))
            AND ((openai_accepted_format IS NOT NULL) OR (openai_image_operations IS NOT NULL)))
        OR (((api_family)::text <> 'openai'::text)
            AND (openai_accepted_format IS NULL)
            AND (openai_image_operations IS NULL)))
    );

ALTER TABLE public.connections
    DROP CONSTRAINT IF EXISTS ck_connections_openai_text_capability;

ALTER TABLE public.connections
    ADD CONSTRAINT ck_connections_openai_dimensions CHECK (
        ((((api_family)::text = 'openai'::text)
            AND ((openai_text_capability IS NULL) OR (openai_text_capability = ANY (ARRAY['responses_only'::text, 'chat_completions_only'::text, 'dual_native'::text])))
            AND ((openai_image_capability IS NULL) OR (openai_image_capability = ANY (ARRAY['generations'::text, 'edits'::text, 'generations_and_edits'::text])))
            AND ((openai_text_capability IS NOT NULL) OR (openai_image_capability IS NOT NULL)))
        OR (((api_family)::text <> 'openai'::text)
            AND (openai_text_capability IS NULL)
            AND (openai_image_capability IS NULL)))
    );
