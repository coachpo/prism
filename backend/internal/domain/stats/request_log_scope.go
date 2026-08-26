package stats

// scopedRequestLogStatusSQL resolves the row-scoped HTTP status for a
// request_logs row: upstream rows use the real upstream HTTP status,
// planning/admission diagnostic rows use the synthesized gateway status, and
// legacy rows use the un-scoped legacy projection. No layer may COALESCE a
// numeric status across scopes (Requests SPEC §6.4/§6.9).
const scopedRequestLogStatusSQL = `CASE row_kind
	WHEN 'upstream' THEN upstream_status_code
	WHEN 'planning' THEN gateway_status_code
	WHEN 'admission' THEN gateway_status_code
	ELSE legacy_status_code
END`

// scopedRequestLogDurationSQL resolves the row-scoped end-to-end duration.
// Upstream rows prefer completion_duration_ms (the true stream finalization
// time); non-stream rows leave that column NULL and fall back to
// attempt_duration_ms; remaining rows use the legacy projection. Never fall
// back to attempt_duration_ms for streams: it only reaches response headers
// and understates real duration by one to two orders of magnitude.
const scopedRequestLogDurationSQL = `CASE WHEN row_kind = 'upstream'
	THEN COALESCE(completion_duration_ms, attempt_duration_ms)
	ELSE legacy_duration_ms END`
