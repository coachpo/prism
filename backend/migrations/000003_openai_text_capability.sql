ALTER TABLE public.connections
    ADD COLUMN openai_text_capability text;

UPDATE public.connections
SET openai_text_capability = CASE
    WHEN btrim(COALESCE(openai_probe_endpoint_variant, '')) LIKE 'chat_completions_%' THEN 'chat_completions_only'
    ELSE 'responses_only'
END
WHERE api_family = 'openai';

UPDATE public.connections
SET openai_text_capability = NULL
WHERE api_family <> 'openai';

ALTER TABLE public.connections
    ADD CONSTRAINT ck_connections_openai_text_capability CHECK (
        (api_family = 'openai' AND openai_text_capability IS NOT NULL AND openai_text_capability IN ('responses_only', 'chat_completions_only', 'dual_native'))
        OR (api_family <> 'openai' AND openai_text_capability IS NULL)
    ) NOT VALID;

ALTER TABLE public.connections
    VALIDATE CONSTRAINT ck_connections_openai_text_capability;
