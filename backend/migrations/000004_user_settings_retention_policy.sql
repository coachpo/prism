ALTER TABLE public.user_settings
    ADD COLUMN request_logs_retention_days integer,
    ADD COLUMN statistics_retention_days integer,
    ADD COLUMN audit_logs_retention_days integer;

ALTER TABLE ONLY public.user_settings
    ADD CONSTRAINT user_settings_request_logs_retention_days_check CHECK (request_logs_retention_days IS NULL OR request_logs_retention_days >= 1),
    ADD CONSTRAINT user_settings_statistics_retention_days_check CHECK (statistics_retention_days IS NULL OR statistics_retention_days >= 1),
    ADD CONSTRAINT user_settings_audit_logs_retention_days_check CHECK (audit_logs_retention_days IS NULL OR audit_logs_retention_days >= 1);
