# Prism Proxy Routing: Quick Reference & Recommendations

**Date**: 2026-03-03  
**Purpose**: Executive summary, quick reference tables, and actionable recommendations

---

## 1. EXECUTIVE SUMMARY

### What Prism Proxy Does

Prism is a **stateless, multi-provider LLM gateway** that:
1. Accepts requests on `/v1/*` and `/v1beta/*` catch-all routes
2. Resolves the model ID from request body (JSON) or URL path (Gemini-style)
3. Looks up the model configuration and provider in PostgreSQL
4. Selects a connection (endpoint) based on load balancing strategy
5. Forwards the request with provider-specific auth headers
6. Implements automatic failover with optional recovery cooldown
7. Logs all requests with token usage and cost calculations
8. Optionally captures request/response bodies for audit

### Key Architectural Decisions

| Decision | Rationale |
|----------|-----------|
| **Catch-all routes** | Supports any provider API path without hardcoding |
| **Model ID from body/path** | Handles both OpenAI-style (body) and Gemini-style (path) APIs |
| **No path restrictions** | Delegates validation to upstream providers |
| **In-memory recovery state** | Fast failover without database round-trips |
| **Streaming accumulation** | Enables logging of full streamed responses |
| **Proxy model redirect** | Allows model aliasing (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`) |

---

## 2. QUICK REFERENCE: REQUEST ROUTING

### Model ID Resolution Priority

```
1. JSON body "model" field (if body exists and is valid JSON)
   └─ Example: {"model": "gpt-4o", ...}

2. URL path /models/{model_id} pattern (Gemini-style)
   └─ Example: /v1beta/models/gemini-pro:generateContent

3. Error: 400 Bad Request
   └─ "Cannot determine model for routing"
```

### Provider Detection

```
Provider Type → Determined by → Auth Header
─────────────────────────────────────────────
openai        ModelConfig.provider_type  Authorization: Bearer {api_key}
anthropic     ModelConfig.provider_type  x-api-key: {api_key}
gemini        ModelConfig.provider_type  Authorization: Bearer {api_key}
```

### Load Balancing Strategies

```
Strategy: single
├─ Always use first active connection (priority 0)
├─ No failover
└─ Use case: Single endpoint, no redundancy

Strategy: failover (without recovery)
├─ Try all active connections in priority order
├─ No cooldown between failures
└─ Use case: Multiple endpoints, quick recovery

Strategy: failover (with recovery)
├─ Try healthy connections first
├─ Probe-eligible connections after cooldown expires
├─ Cooldown: default 60s, configurable per model
└─ Use case: Multiple endpoints, prevent thundering herd
```

### Failover Trigger Status Codes

```
403 Forbidden          → Auth/quota issue
429 Too Many Requests  → Rate limited
500 Internal Error     → Server error
502 Bad Gateway        → Upstream unavailable
503 Service Unavailable → Overloaded
529 Site Overloaded    → Google-specific
```

**Also triggers on**:
- `httpx.ConnectError` (connection refused, DNS failure)
- `httpx.TimeoutException` (request timeout)

---

## 3. QUICK REFERENCE: HEADER HANDLING

### Header Processing Order

```
1. Client headers (filtered)
   ├─ Remove hop-by-hop headers (connection, keep-alive, etc.)
   ├─ Remove client auth headers (Authorization, x-api-key, x-goog-api-key)
   ├─ Remove proxy-controlled headers (anthropic-version, etc.)
   └─ Apply blocklist rules

2. Provider auth headers (added)
   └─ Authorization: Bearer {api_key} (or x-api-key for Anthropic)

3. Provider extra headers (added)
   └─ anthropic-version: 2023-06-01 (Anthropic only)

4. Endpoint custom headers (added)
   └─ JSON-encoded custom headers from connection config

5. Final blocklist sanitization
   └─ Remove blocked headers (except protected auth headers)
```

### Blocklist Rule Types

```
Match Type: exact
├─ Matches header name exactly (case-insensitive)
└─ Example: "x-custom-auth" matches only "x-custom-auth"

Match Type: prefix
├─ Matches header name prefix (case-insensitive)
└─ Example: "x-" matches "x-custom-auth", "x-api-key", etc.
```

---

## 4. QUICK REFERENCE: STREAMING RESPONSES

### Streaming Detection

```python
# Extracted from request body
extract_stream_flag(raw_body) → bool(parsed.get("stream", False))
```

### Streaming Response Handling

```
1. Send request with stream=True
2. Return StreamingResponse immediately
3. Accumulate chunks in background
4. After stream completes:
   a. Extract token usage from accumulated payload
   b. Log request with full payload
   c. Record audit log (if enabled)
   d. Mark connection recovered (if success)
```

### Important Limitations

```
❌ Cannot failover mid-stream
   └─ Response already started, cannot retry

❌ Full payload accumulated in memory
   └─ Risk for large streaming responses (100MB+)

✅ Streaming cancellation handled gracefully
   └─ Detached task tracks finalization
```

---

## 5. QUICK REFERENCE: PROXY MODELS

### Proxy Model Redirect

```
Request: model_id = "claude-sonnet-4-5"
         ↓
Lookup: ModelConfig(model_id="claude-sonnet-4-5", model_type="proxy")
        redirect_to = "claude-sonnet-4-5-20250929"
         ↓
Lookup: ModelConfig(model_id="claude-sonnet-4-5-20250929", model_type="native")
         ↓
Use: Native model's connections and provider
```

### Body & Path Rewriting

```
If proxy model resolves to different native model:

Body rewriting:
  {"model": "claude-sonnet-4-5", ...}
  ↓
  {"model": "claude-sonnet-4-5-20250929", ...}

Path rewriting:
  /v1/models/claude-sonnet-4-5:generateContent
  ↓
  /v1/models/claude-sonnet-4-5-20250929:generateContent
```

### Limitations

```
❌ No recursive proxy chains
   └─ Only one level of redirect supported
   └─ Proxy → Proxy → Native not supported

❌ No proxy connections
   └─ Proxy models have no connections of their own
   └─ Must redirect to native model with connections
```

---

## 6. QUICK REFERENCE: RECOVERY STATE

### State Transitions

```
HEALTHY
  ↓ (request fails)
BLOCKED (cooldown active)
  ├─ Skipped in connection selection
  ├─ (cooldown expires)
  ↓
PROBE_ELIGIBLE
  ├─ Included in connection selection (after healthy)
  ├─ (request succeeds)
  ↓
HEALTHY (recovered)
```

### Recovery State Storage

```
Location: In-memory dict in loadbalancer.py
Key: (profile_id, connection_id)
Value: (blocked_until_mono, cooldown_seconds)

Scope: Process-scoped
Lifetime: Lost on process restart
Cleanup: None (dict grows unbounded)
```

### Implications

```
✅ Fast failover (no database queries)
✅ Per-profile isolation (different profiles independent)

❌ Lost on restart (thundering herd risk)
❌ Memory unbounded (no cleanup)
❌ Not shared across processes (horizontal scaling issue)
```

---

## 7. QUICK REFERENCE: COSTING & LOGGING

### Cost Calculation

```
Cost = (input_tokens × input_rate) + (output_tokens × output_rate)
       + (cache_read_tokens × cache_read_rate)
       + (cache_creation_tokens × cache_creation_rate)
       + (reasoning_tokens × reasoning_rate)

Storage: Integer micros (e.g., $0.01 = 10000 micros)
Lookup: FX rates from EndpointFxRateSetting table
```

### Request Log Fields

```
model_id              Model identifier
profile_id            Profile scope
provider_type         openai, anthropic, or gemini
endpoint_id           Endpoint (global credential)
connection_id         Connection (model-scoped routing)
endpoint_base_url     Snapshot of endpoint URL
status_code           HTTP response status
response_time_ms      Latency in milliseconds
is_stream             Boolean: streaming or non-streaming
request_path          Full request path (e.g., /v1/chat/completions)
error_detail          First 500 chars of error response
input_tokens          Extracted from response
output_tokens         Extracted from response
total_tokens          Extracted from response
*_micros              Cost fields (input_cost_micros, output_cost_micros, etc.)
```

### Audit Log Fields (if enabled)

```
request_log_id        Link to request log
request_method        HTTP method (GET, POST, etc.)
request_url           Full upstream URL
request_headers       Headers sent to upstream
request_body          Request body (if capture_bodies=true)
response_status       HTTP response status
response_headers      Headers from upstream
response_body         Response body (if capture_bodies=true)
duration_ms           Request duration
```

---

## 8. RECOMMENDATIONS FOR IMPROVEMENTS

### High Priority

#### 1. Persist Recovery State

**Problem**: Recovery state lost on process restart → thundering herd

**Solution**: Store in Redis or database
```python
# Pseudo-code
async def mark_connection_failed(profile_id, connection_id, cooldown_seconds, now_mono):
    blocked_until = now_mono + cooldown_seconds
    await redis.set(
        f"recovery:{profile_id}:{connection_id}",
        json.dumps({"blocked_until": blocked_until, "cooldown": cooldown_seconds}),
        ex=int(cooldown_seconds) + 60,  # TTL
    )
```

**Impact**: Prevents thundering herd on restart, enables horizontal scaling

#### 2. Add Streaming Size Limits

**Problem**: Full payload accumulated in memory → OOM risk for large streams

**Solution**: Add configurable size limit
```python
MAX_STREAMING_PAYLOAD_SIZE = 100 * 1024 * 1024  # 100MB

async def _iter_and_log(resp: httpx.Response) -> AsyncGenerator[bytes, None]:
    accumulated = bytearray()
    for chunk in resp.aiter_bytes():
        accumulated.extend(chunk)
        if len(accumulated) > MAX_STREAMING_PAYLOAD_SIZE:
            logger.warning("Streaming payload exceeded limit, disabling logging")
            # Disable logging for this stream
            break
        yield chunk
```

**Impact**: Prevents OOM, enables safe streaming of large responses

#### 3. Add Per-Provider Path Allowlisting (Optional)

**Problem**: No validation of paths → could forward invalid requests

**Solution**: Optional allowlist per provider
```python
PROVIDER_ALLOWED_PATHS = {
    "openai": {
        "patterns": [
            "/v1/chat/completions",
            "/v1/embeddings",
            "/v1/images/generations",
            "/v1/audio/speech",
            "/v1/files",
            "/v1/fine_tuning/jobs",
        ],
        "allow_unknown": False,  # Reject paths not in list
    },
    "anthropic": {
        "patterns": ["/v1/messages", "/v1/models"],
        "allow_unknown": False,
    },
    "gemini": {
        "patterns": ["/v1beta/projects/*/locations/*/publishers/google/models/*"],
        "allow_unknown": False,
    },
}
```

**Impact**: Prevents accidental forwarding of invalid paths, improves security

### Medium Priority

#### 4. Support Recursive Proxy Chains

**Problem**: Only one level of proxy redirect supported

**Solution**: Recursive lookup with cycle detection
```python
async def get_model_config_with_connections(
    db: AsyncSession, profile_id: int, model_id: str, visited: set[str] | None = None
) -> ModelConfig | None:
    if visited is None:
        visited = set()
    
    if model_id in visited:
        logger.error("Proxy chain cycle detected: %s", visited)
        return None
    
    visited.add(model_id)
    
    # ... lookup logic ...
    
    if config.model_type == "proxy" and config.redirect_to:
        return await get_model_config_with_connections(
            db, profile_id, config.redirect_to, visited
        )
```

**Impact**: Enables multi-level model aliasing

#### 5. Add Recovery State Cleanup

**Problem**: Recovery state dict grows unbounded

**Solution**: Periodic cleanup of expired entries
```python
async def cleanup_recovery_state():
    while True:
        await asyncio.sleep(300)  # Every 5 minutes
        now = time.monotonic()
        expired = [
            k for k, (blocked_until, _) in _recovery_state.items()
            if blocked_until < now
        ]
        for k in expired:
            del _recovery_state[k]
        if expired:
            logger.debug("Cleaned up %d expired recovery entries", len(expired))
```

**Impact**: Prevents memory leak, improves observability

#### 6. Add Metrics & Observability

**Problem**: Limited visibility into proxy behavior

**Solution**: Add Prometheus metrics
```python
from prometheus_client import Counter, Histogram, Gauge

request_count = Counter(
    "prism_requests_total",
    "Total requests",
    ["model", "provider", "status_code"],
)

request_latency = Histogram(
    "prism_request_latency_ms",
    "Request latency",
    ["model", "provider"],
)

failover_count = Counter(
    "prism_failovers_total",
    "Total failovers",
    ["model", "connection_id"],
)

recovery_state_size = Gauge(
    "prism_recovery_state_size",
    "Recovery state dict size",
)
```

**Impact**: Enables monitoring, alerting, and debugging

### Low Priority

#### 7. Support Proxy Model Connections

**Problem**: Proxy models cannot have their own connections

**Solution**: Allow proxy models to have connections (forward to target)
```python
# Current: Proxy models have no connections
# Proposed: Proxy models can have connections that forward to target

# Example:
# Proxy model "claude-sonnet-4-5" (connections: [conn1, conn2])
#   → redirect_to: "claude-sonnet-4-5-20250929"
#   → Use proxy model's connections, but forward to target model
```

**Impact**: More flexible routing, but adds complexity

#### 8. Support Request/Response Transformation

**Problem**: Only model ID rewriting supported

**Solution**: Allow custom transformations per connection
```python
# Example: Transform request format
# OpenAI format → Anthropic format
# {"model": "gpt-4", "messages": [...]}
# →
# {"model": "claude-3-sonnet", "messages": [...]}
```

**Impact**: Enables cross-provider compatibility, but adds complexity

---

## 9. TESTING CHECKLIST

### Unit Tests

- [ ] Model ID resolution (body, path, fallback)
- [ ] Proxy model redirect (single level, missing target, cycle detection)
- [ ] Failover trigger status codes (403, 429, 5xx)
- [ ] Recovery state transitions (healthy → blocked → probe → healthy)
- [ ] Header blocklist application (exact, prefix, protected headers)
- [ ] Body rewriting (model ID substitution)
- [ ] Path rewriting (model ID substitution)
- [ ] Streaming flag detection (true, false, missing)

### Integration Tests

- [ ] OpenAI request flow (model resolution → auth → forward → log)
- [ ] Anthropic request flow (with extra headers)
- [ ] Gemini request flow (path-based model extraction)
- [ ] Failover scenario (connection 1 fails → connection 2 succeeds)
- [ ] Recovery cooldown (blocked → probe → healthy)
- [ ] Streaming response (accumulation → logging)
- [ ] Audit logging (request/response capture)
- [ ] Cost calculation (token extraction → pricing → logging)

### End-to-End Tests

- [ ] Multi-provider routing (same model ID, different providers)
- [ ] Proxy model aliasing (proxy → native)
- [ ] Load balancing (single vs failover)
- [ ] Header injection prevention (blocklist enforcement)
- [ ] Query parameter preservation
- [ ] Response header filtering (hop-by-hop removal)
- [ ] Error handling (400, 404, 502, 503)

---

## 10. DEPLOYMENT CHECKLIST

### Pre-Deployment

- [ ] Database migrations applied (`alembic upgrade head`)
- [ ] Providers seeded (openai, anthropic, gemini)
- [ ] At least one profile created
- [ ] At least one model configured per provider
- [ ] At least one endpoint created per provider
- [ ] At least one connection created per model
- [ ] API keys configured for all endpoints
- [ ] Blocklist rules configured (if needed)
- [ ] Costing settings configured (if needed)

### Post-Deployment

- [ ] Health check endpoint responds (GET /health)
- [ ] Proxy endpoint responds (POST /v1/chat/completions)
- [ ] Request logs created (GET /api/stats/requests)
- [ ] Audit logs created (if enabled)
- [ ] Cost calculations correct (GET /api/stats/spending)
- [ ] Failover works (simulate endpoint failure)
- [ ] Recovery cooldown works (verify connection blocked then recovered)
- [ ] Streaming works (test with stream=true)

---

## 11. TROUBLESHOOTING GUIDE

### Issue: 400 Bad Request - "Cannot determine model for routing"

**Cause**: Model ID not found in body or path

**Solution**:
1. Check request body has `"model"` field (JSON)
2. Check request path matches `/models/{model_id}` pattern (Gemini)
3. Verify model_id is not empty string

### Issue: 404 Not Found - "Model not configured or disabled"

**Cause**: Model not found in database or disabled

**Solution**:
1. Verify model exists: `GET /api/models`
2. Verify model is enabled: `is_enabled=true`
3. Verify model is in correct profile: `profile_id={active_profile_id}`

### Issue: 503 Service Unavailable - "No active connections available"

**Cause**: All connections disabled or in recovery cooldown

**Solution**:
1. Check connections are enabled: `GET /api/models/{id}/connections`
2. Check recovery state: Look for blocked connections
3. Wait for cooldown to expire (default 60s)
4. Manually trigger health check: `POST /api/connections/{id}/health`

### Issue: 502 Bad Gateway - "All connections failed"

**Cause**: All connections tried, all failed

**Solution**:
1. Check endpoint URLs are correct
2. Check API keys are valid
3. Check upstream provider is healthy
4. Check network connectivity
5. Review request logs: `GET /api/stats/requests?status_code=502`

### Issue: Streaming response incomplete

**Cause**: Stream cancelled or connection failed mid-stream

**Solution**:
1. Check client didn't cancel request
2. Check upstream provider didn't close connection
3. Review audit logs for error details
4. Check network stability

### Issue: High memory usage

**Cause**: Recovery state or streaming accumulation

**Solution**:
1. Monitor recovery state size (add metrics)
2. Add streaming size limits
3. Restart process to clear recovery state
4. Consider persisting recovery state to Redis

---

## 12. QUICK LOOKUP: FILE LOCATIONS

| Component | File | Lines |
|-----------|------|-------|
| Catch-all routes | `proxy.py` | 748-769 |
| Model resolution | `proxy.py` | 90-97 |
| Model lookup | `loadbalancer.py` | 14-57 |
| Connection selection | `loadbalancer.py` | 75-121 |
| Recovery state | `loadbalancer.py` | 11, 124-149 |
| Header building | `proxy_service.py` | 119-199 |
| Failover trigger | `proxy_service.py` | 32, 276-277 |
| Upstream URL | `proxy_service.py` | 82-94 |
| Streaming | `proxy.py` | 256-517 |
| Request logging | `stats_service.py` | (log_request function) |
| Audit logging | `audit_service.py` | (record_audit_log function) |
| Costing | `costing_service.py` | (compute_cost_fields function) |
| Dependencies | `dependencies.py` | 78-87 |

---

## 13. SUMMARY TABLE: ROUTING DECISION TREE

```
Request arrives at /v1/{path}
│
├─ Extract model_id
│  ├─ From body JSON "model" field? → Use it
│  ├─ From path /models/{id}? → Use it
│  └─ Not found? → 400 Bad Request
│
├─ Lookup ModelConfig(profile_id, model_id)
│  ├─ Not found? → 404 Not Found
│  ├─ Is proxy model? → Follow redirect_to
│  └─ Found native model? → Continue
│
├─ Get provider info (openai/anthropic/gemini)
│
├─ Build attempt plan (connection selection)
│  ├─ Strategy: single? → Use first connection
│  ├─ Strategy: failover? → Try all in priority order
│  └─ No active connections? → 503 Service Unavailable
│
├─ For each connection:
│  ├─ Build upstream URL
│  ├─ Build headers (auth + blocklist)
│  ├─ Rewrite body (model ID if proxy)
│  ├─ Send request
│  ├─ Status 2xx-3xx? → Log and return
│  ├─ Status 403/429/5xx? → Mark failed, try next
│  ├─ ConnectError/TimeoutError? → Mark failed, try next
│  └─ Continue to next connection
│
└─ All connections exhausted? → 502 Bad Gateway
```

