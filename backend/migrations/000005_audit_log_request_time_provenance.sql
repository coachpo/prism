ALTER TABLE request_logs
ADD COLUMN IF NOT EXISTS audit_capture_bodies_at_request boolean;

UPDATE request_logs
SET audit_capture_bodies_at_request = EXISTS (
    SELECT 1
    FROM audit_logs
    WHERE audit_logs.request_log_id = request_logs.id
      AND audit_logs.profile_id = request_logs.profile_id
      AND (
          audit_logs.request_body IS NOT NULL
          OR audit_logs.response_body IS NOT NULL
      )
)
WHERE audit_capture_bodies_at_request IS NULL;

ALTER TABLE request_logs
ALTER COLUMN audit_capture_bodies_at_request SET DEFAULT FALSE;

ALTER TABLE request_logs
ALTER COLUMN audit_capture_bodies_at_request SET NOT NULL;

ALTER TABLE audit_logs
ADD COLUMN IF NOT EXISTS request_body_stored boolean;

ALTER TABLE audit_logs
ADD COLUMN IF NOT EXISTS response_body_stored boolean;

ALTER TABLE audit_logs
ADD COLUMN IF NOT EXISTS audit_enabled_at_request boolean;
ALTER TABLE audit_logs
ADD COLUMN IF NOT EXISTS audit_capture_bodies_at_request boolean;

UPDATE audit_logs
SET request_body_stored = request_body IS NOT NULL,
    response_body_stored = response_body IS NOT NULL,
    audit_enabled_at_request = COALESCE((
        SELECT request_logs.audit_enabled_at_request
        FROM request_logs
        WHERE request_logs.id = audit_logs.request_log_id
          AND request_logs.profile_id = audit_logs.profile_id
        LIMIT 1
    ), TRUE),
    audit_capture_bodies_at_request = (
        request_body IS NOT NULL
        OR response_body IS NOT NULL
    )
WHERE request_body_stored IS NULL
   OR response_body_stored IS NULL
   OR audit_enabled_at_request IS NULL
   OR audit_capture_bodies_at_request IS NULL;

ALTER TABLE audit_logs
ALTER COLUMN request_body_stored SET DEFAULT FALSE;
ALTER TABLE audit_logs
ALTER COLUMN response_body_stored SET DEFAULT FALSE;

ALTER TABLE audit_logs
ALTER COLUMN audit_enabled_at_request SET DEFAULT FALSE;

ALTER TABLE audit_logs
ALTER COLUMN audit_capture_bodies_at_request SET DEFAULT FALSE;

ALTER TABLE audit_logs
ALTER COLUMN request_body_stored SET NOT NULL;

ALTER TABLE audit_logs
ALTER COLUMN response_body_stored SET NOT NULL;

ALTER TABLE audit_logs
ALTER COLUMN audit_enabled_at_request SET NOT NULL;

ALTER TABLE audit_logs
ALTER COLUMN audit_capture_bodies_at_request SET NOT NULL;
