ALTER TABLE request_logs
    ADD COLUMN IF NOT EXISTS stream_outcome varchar(50),
    ADD COLUMN IF NOT EXISTS stream_error_kind varchar(50),
    ADD COLUMN IF NOT EXISTS stream_error_detail text;

DO $$
DECLARE
    has_is_stream boolean;
    has_ttft boolean;
    has_completion boolean;
    backfill_case text;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'request_logs'
          AND column_name = 'is_stream'
    ) INTO has_is_stream;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'request_logs'
          AND column_name = 'ttft_ms'
    ) INTO has_ttft;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'request_logs'
          AND column_name = 'completion_duration_ms'
    ) INTO has_completion;

    backfill_case := CASE
        WHEN has_is_stream THEN
            'CASE WHEN is_stream = FALSE THEN ''not_streaming'' WHEN completion_duration_ms IS NOT NULL THEN ''completed'' ELSE ''unknown'' END'
        WHEN has_ttft AND has_completion THEN
            'CASE WHEN ttft_ms IS NULL THEN ''not_streaming'' WHEN completion_duration_ms IS NOT NULL THEN ''completed'' ELSE ''unknown'' END'
        WHEN has_ttft THEN
            'CASE WHEN ttft_ms IS NULL THEN ''not_streaming'' ELSE ''unknown'' END'
        WHEN has_completion THEN
            'CASE WHEN completion_duration_ms IS NOT NULL THEN ''completed'' ELSE ''not_streaming'' END'
        ELSE
            '''not_streaming'''
    END;

    EXECUTE 'UPDATE request_logs SET stream_outcome = ' || backfill_case || ' WHERE stream_outcome IS NULL';
END $$;

ALTER TABLE request_logs
    ALTER COLUMN stream_outcome SET DEFAULT 'not_streaming',
    ALTER COLUMN stream_outcome SET NOT NULL;

DO $$
DECLARE
    has_usage_request_events boolean;
    has_usage_completion boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = 'public'
          AND table_name = 'usage_request_events'
    ) INTO has_usage_request_events;

    IF NOT has_usage_request_events THEN
        RETURN;
    END IF;

    ALTER TABLE usage_request_events
        ADD COLUMN IF NOT EXISTS stream_outcome varchar(50),
        ADD COLUMN IF NOT EXISTS stream_error_kind varchar(50);

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'usage_request_events'
          AND column_name = 'completion_duration_ms'
    ) INTO has_usage_completion;

    EXECUTE '
        UPDATE usage_request_events usage_event
        SET stream_outcome = request_log.stream_outcome,
            stream_error_kind = request_log.stream_error_kind
        FROM request_logs request_log
        WHERE usage_event.profile_id = request_log.profile_id
          AND usage_event.ingress_request_id = request_log.ingress_request_id
          AND usage_event.attempt_count = request_log.attempt_number
          AND usage_event.stream_outcome IS NULL
    ';

    IF has_usage_completion THEN
        EXECUTE '
            UPDATE usage_request_events
            SET stream_outcome = CASE
                WHEN completion_duration_ms IS NOT NULL THEN ''completed''
                ELSE ''unknown''
            END
            WHERE stream_outcome IS NULL
        ';
    ELSE
        EXECUTE '
            UPDATE usage_request_events
            SET stream_outcome = ''not_streaming''
            WHERE stream_outcome IS NULL
        ';
    END IF;

    ALTER TABLE usage_request_events
        ALTER COLUMN stream_outcome SET DEFAULT 'not_streaming',
        ALTER COLUMN stream_outcome SET NOT NULL;
END $$;
