ALTER TABLE public.model_configs
    ADD COLUMN openai_accepted_format text;

UPDATE public.model_configs
SET openai_accepted_format = 'dual_native'
WHERE api_family = 'openai';

UPDATE public.model_configs
SET openai_accepted_format = NULL
WHERE api_family <> 'openai';

ALTER TABLE public.model_configs
    ADD CONSTRAINT ck_model_configs_openai_accepted_format CHECK (
        (api_family = 'openai' AND openai_accepted_format IS NOT NULL AND openai_accepted_format IN ('responses_only', 'chat_completions_only', 'dual_native'))
        OR (api_family <> 'openai' AND openai_accepted_format IS NULL)
    ) NOT VALID;

ALTER TABLE public.model_configs
    VALIDATE CONSTRAINT ck_model_configs_openai_accepted_format;
