-- Runtime latency semantics: rename live_p95_latency_ms to last_success_response_headers_latency_ms.
--
-- The value stored in this column is the latency of the last successful upstream
-- attempt measured from request start to response-headers receipt. It is not a
-- percentile, not TTFT, and does not include body/SSE consumption time.
-- The rename preserves existing values (ALTER COLUMN RENAME keeps data in place).
ALTER TABLE public.routing_connection_runtime_state
    RENAME COLUMN live_p95_latency_ms TO last_success_response_headers_latency_ms;
