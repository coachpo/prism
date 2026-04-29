ALTER TABLE request_logs
ADD COLUMN IF NOT EXISTS request_generation_params JSONB;

ALTER TABLE request_logs
ADD COLUMN IF NOT EXISTS request_generation_params_status VARCHAR(40);
