-- Connection/Terminal Target custom request parameters.
--
-- Adds an optional static top-level JSON object that Prism overlays onto the
-- provider-native upstream request body of every actual attempt selecting the
-- Connection. Existing rows migrate to NULL (not configured) and keep the
-- existing request behavior byte-for-byte.

ALTER TABLE connections
    ADD COLUMN custom_request_parameters JSONB NULL;

ALTER TABLE connections
    ADD CONSTRAINT connections_custom_request_parameters_object
    CHECK (
        custom_request_parameters IS NULL
        OR jsonb_typeof(custom_request_parameters) = 'object'
    );
