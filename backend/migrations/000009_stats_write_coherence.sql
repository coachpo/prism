UPDATE public.request_logs
SET
    priced_flag = CASE
        WHEN NULLIF(btrim(unpriced_reason), '') IS NOT NULL THEN FALSE
        WHEN COALESCE(success_flag, status_code BETWEEN 200 AND 299) AND total_cost_user_currency_micros IS NOT NULL THEN TRUE
        WHEN COALESCE(success_flag, status_code BETWEEN 200 AND 299) AND priced_flag IS TRUE AND total_cost_user_currency_micros IS NULL THEN FALSE
        ELSE priced_flag
    END,
    unpriced_reason = CASE
        WHEN NULLIF(btrim(unpriced_reason), '') IS NOT NULL THEN NULLIF(btrim(unpriced_reason), '')
        WHEN COALESCE(success_flag, status_code BETWEEN 200 AND 299) AND priced_flag IS TRUE AND total_cost_user_currency_micros IS NULL THEN 'MISSING_PRICE_DATA'
        ELSE NULL
    END;

UPDATE public.request_logs
SET
    fx_rate_used = CASE
        WHEN NOT (COALESCE(success_flag, status_code BETWEEN 200 AND 299) AND total_cost_user_currency_micros IS NOT NULL AND priced_flag IS TRUE AND NULLIF(btrim(unpriced_reason), '') IS NULL) THEN NULL
        WHEN NULLIF(btrim(fx_rate_used), '') IS NOT NULL OR NULLIF(btrim(fx_rate_source), '') IS NOT NULL THEN NULLIF(btrim(fx_rate_used), '')
        WHEN NULLIF(btrim(currency_code_original), '') IS NOT NULL AND NULLIF(btrim(currency_code_original), '') = NULLIF(btrim(report_currency_code), '') THEN '1'
        ELSE NULL
    END,
    fx_rate_source = CASE
        WHEN NOT (COALESCE(success_flag, status_code BETWEEN 200 AND 299) AND total_cost_user_currency_micros IS NOT NULL AND priced_flag IS TRUE AND NULLIF(btrim(unpriced_reason), '') IS NULL) THEN NULL
        WHEN NULLIF(btrim(fx_rate_used), '') IS NOT NULL OR NULLIF(btrim(fx_rate_source), '') IS NOT NULL THEN NULLIF(btrim(fx_rate_source), '')
        WHEN NULLIF(btrim(currency_code_original), '') IS NOT NULL AND NULLIF(btrim(currency_code_original), '') = NULLIF(btrim(report_currency_code), '') THEN 'DEFAULT_1_TO_1'
        ELSE NULL
    END;

UPDATE public.usage_request_events
SET
    priced_flag = CASE
        WHEN NULLIF(btrim(unpriced_reason), '') IS NOT NULL THEN FALSE
        WHEN success_flag AND COALESCE(billable_flag, FALSE) AND total_cost_user_currency_micros IS NOT NULL THEN TRUE
        WHEN success_flag AND COALESCE(billable_flag, FALSE) AND priced_flag IS TRUE AND total_cost_user_currency_micros IS NULL THEN FALSE
        ELSE priced_flag
    END,
    unpriced_reason = CASE
        WHEN NULLIF(btrim(unpriced_reason), '') IS NOT NULL THEN NULLIF(btrim(unpriced_reason), '')
        WHEN success_flag AND COALESCE(billable_flag, FALSE) AND priced_flag IS TRUE AND total_cost_user_currency_micros IS NULL THEN 'MISSING_PRICE_DATA'
        ELSE NULL
    END;

UPDATE public.usage_request_events
SET
    fx_rate_used = CASE
        WHEN NOT (success_flag AND COALESCE(billable_flag, FALSE) AND total_cost_user_currency_micros IS NOT NULL AND priced_flag IS TRUE AND NULLIF(btrim(unpriced_reason), '') IS NULL) THEN NULL
        WHEN NULLIF(btrim(fx_rate_used), '') IS NOT NULL OR NULLIF(btrim(fx_rate_source), '') IS NOT NULL THEN NULLIF(btrim(fx_rate_used), '')
        WHEN NULLIF(btrim(currency_code_original), '') IS NOT NULL AND NULLIF(btrim(currency_code_original), '') = NULLIF(btrim(report_currency_code), '') THEN '1'
        ELSE NULL
    END,
    fx_rate_source = CASE
        WHEN NOT (success_flag AND COALESCE(billable_flag, FALSE) AND total_cost_user_currency_micros IS NOT NULL AND priced_flag IS TRUE AND NULLIF(btrim(unpriced_reason), '') IS NULL) THEN NULL
        WHEN NULLIF(btrim(fx_rate_used), '') IS NOT NULL OR NULLIF(btrim(fx_rate_source), '') IS NOT NULL THEN NULLIF(btrim(fx_rate_source), '')
        WHEN NULLIF(btrim(currency_code_original), '') IS NOT NULL AND NULLIF(btrim(currency_code_original), '') = NULLIF(btrim(report_currency_code), '') THEN 'DEFAULT_1_TO_1'
        ELSE NULL
    END;
