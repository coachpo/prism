UPDATE request_logs
SET audit_enabled_at_request = FALSE
WHERE audit_enabled_at_request IS NULL;

ALTER TABLE request_logs
ALTER COLUMN audit_enabled_at_request SET DEFAULT FALSE;

ALTER TABLE request_logs
ALTER COLUMN audit_enabled_at_request SET NOT NULL;
