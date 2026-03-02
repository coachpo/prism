# Prism Proxy Routing: Supplementary Analysis

**Date**: 2026-03-03  
**Scope**: Flow diagrams, code examples, edge cases, and architectural insights

---

## 1. REQUEST FLOW DIAGRAMS

### High-Level Proxy Request Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ Client Request: POST /v1/chat/completions                       │
│ Body: {"model": "gpt-4o", "messages": [...], "stream": false}   │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │ FastAPI Route Handler              │
        │ proxy_catch_all()                  │
        │ - Extract path: "chat/completions" │
        │ - Read raw body                    │
        │ - Get active profile_id            │
        └────────────────┬───────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │ _handle_proxy()                    │
        │ - Resolve model_id from body/path  │
        │ - Lookup ModelConfig + connections│
        │ - Handle proxy model redirect      │
        └────────────────┬───────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │ build_attempt_plan()               │
        │ - Filter active connections        │
        │ - Apply load balancing strategy    │
        │ - Check recovery state             │
        └────────────────┬───────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │ For each connection (in order):    │
        │ 1. Build upstream URL              │
        │ 2. Build headers (auth + blocklist)│
        │ 3. Rewrite body (model ID)         │
        │ 4. Send request                    │
        └────────────────┬───────────────────┘
                         │
        ┌────────────────┴────────────────────┐
        │                                     │
        ▼                                     ▼
    Success (2xx-3xx)                  Failover Trigger (403,429,5xx)
        │                                     │
        ├─ Log request                        ├─ Log failure
        ├─ Mark recovered                     ├─ Mark connection failed
        ├─ Return response                    ├─ Continue to next
        │                                     │
        └─────────────────┬───────────────────┘
                          │
                          ▼
                  ┌──────────────────┐
                  │ All exhausted?    │
                  └────┬─────────┬────┘
                       │         │
                   Yes │         │ No
                       │         │
                       ▼         ▼
                    502 Error  Try next
```

### Model Resolution Flow (with Proxy Redirect)

```
Request: model_id = "claude-sonnet-4-5"
         profile_id = 1

         ▼
┌─────────────────────────────────────────┐
│ Query ModelConfig                       │
│ WHERE profile_id=1                      │
│   AND model_id="claude-sonnet-4-5"      │
│   AND is_enabled=True                   │
└────────────┬────────────────────────────┘
             │
             ▼
    ┌────────────────────┐
    │ Found?             │
    └────┬───────────┬───┘
         │           │
        No          Yes
         │           │
         ▼           ▼
      404 Error  ┌──────────────────────┐
                 │ Is proxy model?       │
                 │ (model_type="proxy")  │
                 └────┬───────────┬──────┘
                      │           │
                     No          Yes
                      │           │
                      ▼           ▼
                   Return    ┌──────────────────────┐
                   config    │ Query target model   │
                             │ model_id=redirect_to │
                             │ (e.g., "claude-...")│
                             └────┬───────────┬─────┘
                                  │           │
                                 No          Yes
                                  │           │
                                  ▼           ▼
                               404 Error   Return target
                                           config
```

### Failover Recovery State Machine

```
Connection State Transitions (with recovery enabled):

┌──────────────┐
│   HEALTHY    │  (not in _recovery_state)
└──────┬───────┘
       │
       │ Request fails (403, 429, 5xx, timeout, connect error)
       │
       ▼
┌──────────────────────────────────────┐
│ BLOCKED                              │
│ _recovery_state[(profile_id, conn_id)]│
│ = (blocked_until_mono, cooldown_sec) │
└──────┬───────────────────────────────┘
       │
       │ now_mono < blocked_until_mono
       │ (cooldown active)
       │
       ├─ Connection skipped in build_attempt_plan()
       │
       │ now_mono >= blocked_until_mono
       │ (cooldown expired)
       │
       ▼
┌──────────────────────────────────────┐
│ PROBE_ELIGIBLE                       │
│ (included in build_attempt_plan())   │
└──────┬───────────────────────────────┘
       │
       │ Request succeeds (2xx-3xx)
       │
       ▼
┌──────────────────────────────────────┐
│ HEALTHY (recovered)                  │
│ mark_connection_recovered()           │
│ Removed from _recovery_state          │
└──────────────────────────────────────┘
```

---

## 2. CODE EXAMPLES

### Example 1: OpenAI Request Flow

```python
# Client sends:
POST /v1/chat/completions
Content-Type: application/json

{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false
}

# Prism processing:
1. Extract model_id: "gpt-4o" (from body)
2. Lookup ModelConfig(profile_id=1, model_id="gpt-4o")
   → Found: native model, provider_type="openai"
3. Get active connections (sorted by priority)
   → [Connection(id=1, priority=0, endpoint_id=1)]
4. Build upstream URL:
   - Endpoint base_url: "https://api.openai.com/v1"
   - Request path: "/v1/chat/completions"
   - Result: "https://api.openai.com/v1/chat/completions"
5. Build headers:
   - Client headers: {"user-agent": "..."}
   - Auth: {"Authorization": "Bearer sk-..."}
   - Result: {"user-agent": "...", "Authorization": "Bearer sk-..."}
6. Forward request (non-streaming):
   - POST https://api.openai.com/v1/chat/completions
   - Headers: {...}
   - Body: {"model": "gpt-4o", ...}
7. Response: 200 OK
   - Log request
   - Mark connection recovered
   - Return response to client
```

### Example 2: Gemini Request with Proxy Model

```python
# Client sends:
POST /v1beta/projects/my-project/locations/us-central1/publishers/google/models/gemini-pro:generateContent
Content-Type: application/json

{
  "contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
}

# Prism processing:
1. Extract model_id: "gemini-pro" (from path via regex)
2. Lookup ModelConfig(profile_id=1, model_id="gemini-pro")
   → Found: proxy model, redirect_to="gemini-pro-vision"
3. Lookup target ModelConfig(profile_id=1, model_id="gemini-pro-vision")
   → Found: native model, provider_type="gemini"
4. Get active connections
   → [Connection(id=5, priority=0, endpoint_id=3)]
5. Rewrite path:
   - Original: "/v1beta/projects/.../models/gemini-pro:generateContent"
   - Target model: "gemini-pro-vision"
   - Result: "/v1beta/projects/.../models/gemini-pro-vision:generateContent"
6. Build upstream URL:
   - Endpoint base_url: "https://generativelanguage.googleapis.com"
   - Request path: "/v1beta/projects/.../models/gemini-pro-vision:generateContent"
   - Result: "https://generativelanguage.googleapis.com/v1beta/projects/.../models/gemini-pro-vision:generateContent"
7. Build headers:
   - Auth: {"Authorization": "Bearer {goog_api_key}"}
8. Forward request
9. Response: 200 OK
   - Log request (with target model_id="gemini-pro-vision")
   - Return response
```

### Example 3: Failover Scenario

```python
# Setup:
# Model "claude-3-sonnet" has 2 connections:
# - Connection 1 (priority=0): endpoint_id=1 (api.anthropic.com)
# - Connection 2 (priority=1): endpoint_id=2 (backup.anthropic.com)
# - failover_recovery_enabled=True, cooldown=60s

# Request 1: POST /v1/messages
# Connection 1 attempt:
#   - Send request to api.anthropic.com
#   - Response: 429 (rate limited)
#   - should_failover(429) → True
#   - mark_connection_failed(profile_id=1, connection_id=1, cooldown=60, now_mono=1000.0)
#   - _recovery_state[(1, 1)] = (1060.0, 60)
#   - Continue to next connection

# Connection 2 attempt:
#   - Send request to backup.anthropic.com
#   - Response: 200 OK
#   - mark_connection_recovered(profile_id=1, connection_id=1)
#   - _recovery_state[(1, 1)] deleted
#   - Return response

# Request 2 (at now_mono=1030.0): POST /v1/messages
# build_attempt_plan():
#   - Connection 1: state = (1060.0, 60)
#     - now_mono (1030.0) < blocked_until (1060.0)
#     - Not in healthy, not in probe_eligible
#     - Skipped
#   - Connection 2: state = None
#     - In healthy
#   - Return [Connection 2]

# Connection 2 attempt:
#   - Send request
#   - Response: 200 OK
#   - Return response

# Request 3 (at now_mono=1070.0): POST /v1/messages
# build_attempt_plan():
#   - Connection 1: state = (1060.0, 60)
#     - now_mono (1070.0) >= blocked_until (1060.0)
#     - In probe_eligible
#   - Connection 2: state = None
#     - In healthy
#   - Return [Connection 2, Connection 1]  (healthy first, then probe_eligible)

# Connection 2 attempt:
#   - Send request
#   - Response: 200 OK
#   - Return response
#   - (Connection 1 not tried because Connection 2 succeeded)
```

---

## 3. EDGE CASES & GOTCHAS

### Edge Case 1: Proxy Model Chain (Not Allowed)

```python
# Setup:
# Model A (proxy) → redirect_to: Model B
# Model B (proxy) → redirect_to: Model C
# Model C (native)

# Current behavior: Only ONE level of redirect is followed
# If Model B is a proxy, the code does NOT recursively follow to Model C
# Result: Model B's config is returned (which has no connections)
# Outcome: 404 error or 503 (no active connections)

# Code location: loadbalancer.py, lines 33-55
# The redirect is followed once, but not recursively
```

### Edge Case 2: Model ID Mismatch Between Body and Path

```python
# Request:
POST /v1/models/gpt-4:generateContent
Content-Type: application/json

{
  "model": "gpt-3.5-turbo",
  "messages": [...]
}

# Processing:
1. Extract model_id from body: "gpt-3.5-turbo"
2. Lookup ModelConfig for "gpt-3.5-turbo"
3. If found, use it (path model_id is ignored)
4. Path rewriting only happens if:
   - Path contains /models/{model_id}
   - AND upstream_model_id != path_model_id
   - Then rewrite path to use upstream_model_id

# Result: Body takes precedence over path
```

### Edge Case 3: Invalid JSON Body with Gemini Path

```python
# Request:
POST /v1/models/gemini-pro:generateContent
Content-Type: application/json

{invalid json}

# Processing:
1. extract_model_from_body() fails (JSONDecodeError)
2. Falls back to _extract_model_from_path()
3. Extracts "gemini-pro" from path
4. Proceeds normally

# Result: Graceful fallback to path-based extraction
```

### Edge Case 4: Streaming Request with Failover

```python
# Request:
POST /v1/chat/completions
{"model": "gpt-4o", "stream": true, ...}

# Connection 1 attempt:
1. Build streaming request
2. Send with stream=True
3. Receive response headers (status 200)
4. Start streaming chunks
5. After 5 chunks, upstream returns 500 error
6. Current behavior: Response already started, cannot failover
7. Client receives partial stream + error

# Note: Failover only works on initial response status code
# If error occurs mid-stream, it's too late to failover
```

### Edge Case 5: Recovery State Persistence

```python
# Scenario: Process restart

# Before restart:
# _recovery_state = {
#   (1, 1): (1060.0, 60),  # Connection 1 blocked until 1060.0
#   (1, 2): (1050.0, 60),  # Connection 2 blocked until 1050.0
# }

# After process restart:
# _recovery_state = {}  # Empty!

# Result: All connections immediately become healthy
# This can cause thundering herd if many connections were blocked
```

### Edge Case 6: Blocklist Rule Bypass via Custom Headers

```python
# Setup:
# Blocklist rule: block header "x-custom-auth" (exact match)

# Endpoint custom_headers: {"x-custom-auth": "secret"}

# Processing:
1. Client headers sanitized against blocklist
2. Provider auth headers added (protected)
3. Custom headers added (overwrites same-name)
4. Final blocklist applied (but protects auth headers)
5. Custom header "x-custom-auth" is blocked in final pass

# Result: Custom header is blocked (safe)
# But if custom header name differs from blocklist pattern, it passes
```

---

## 4. PERFORMANCE CONSIDERATIONS

### Database Query Optimization

```python
# Current: Uses selectinload for relationships
select(ModelConfig)
    .options(
        selectinload(ModelConfig.connections).selectinload(Connection.endpoint_rel),
        selectinload(ModelConfig.provider),
    )
    .where(...)

# Impact:
# - 1 query for ModelConfig
# - 1 query for connections (with endpoint_rel loaded)
# - 1 query for provider
# Total: 3 queries per request (or 4 if proxy redirect)

# Optimization opportunity: Use joinedload for single query
```

### Recovery State Memory Growth

```python
# Current: In-memory dict with no cleanup
_recovery_state: dict[tuple[int, int], tuple[float, float]] = {}

# Risk: If many connections fail, dict grows unbounded
# Mitigation: Could add periodic cleanup of expired entries
# Example: Remove entries where blocked_until < now_mono

# Recommended: Add cleanup in a background task
async def cleanup_recovery_state():
    now = time.monotonic()
    expired = [k for k, (blocked_until, _) in _recovery_state.items() if blocked_until < now]
    for k in expired:
        del _recovery_state[k]
```

### Streaming Payload Accumulation

```python
# Current: Accumulates entire stream in memory
async def _iter_and_log(resp: httpx.Response) -> AsyncGenerator[bytes, None]:
    accumulated = bytearray()
    async for chunk in resp.aiter_bytes():
        accumulated.extend(chunk)  # Grows unbounded
        yield chunk

# Risk: Large streaming responses (e.g., 100MB file) consume memory
# Mitigation: Could add size limit or disable logging for large streams
```

---

## 5. SECURITY IMPLICATIONS

### Header Injection Prevention

✅ **Protected**: Auth headers are replaced, not merged
- Client cannot inject `Authorization` header
- Provider auth is always set by proxy

⚠️ **Risk**: Custom headers from endpoint config
- If endpoint custom_headers are user-controlled, could inject headers
- Mitigation: Validate custom_headers JSON on create/update

### API Key Exposure

✅ **Protected**: API keys not logged in request logs
- Only stored in database
- Audit logs can be disabled per-provider

⚠️ **Risk**: Audit logs with `capture_bodies=true`
- Response bodies might contain sensitive data
- Mitigation: Disable audit capture for sensitive models

### Path Traversal

✅ **Safe**: No path normalization or traversal checks needed
- Paths are forwarded as-is to upstream
- Upstream provider validates paths

### Model ID Injection

⚠️ **Risk**: If model_id is user-controlled and used in queries
- Current: model_id comes from request body or path (user-controlled)
- Mitigation: Database uses parameterized queries (SQLAlchemy)

---

## 6. ARCHITECTURAL OBSERVATIONS

### Strengths

1. **Stateless proxy**: No session state, easy to scale horizontally
2. **Flexible model resolution**: Supports both JSON body and URL path patterns
3. **Graceful failover**: Automatic retry with recovery cooldown
4. **Audit trail**: Optional request/response capture for compliance
5. **Header control**: Blocklist rules prevent header injection

### Weaknesses

1. **No per-provider path validation**: All paths forwarded blindly
2. **Recovery state not persistent**: Lost on restart (thundering herd risk)
3. **Streaming failover limitation**: Cannot failover mid-stream
4. **Memory unbounded**: Recovery state and streaming accumulation
5. **Single-level proxy redirect**: No recursive proxy chains

### Recommendations

1. **Add path allowlisting** (optional per-provider):
   ```python
   # Example: OpenAI only allows /v1/chat/completions, /v1/embeddings, etc.
   PROVIDER_ALLOWED_PATHS = {
       "openai": ["/v1/chat/completions", "/v1/embeddings", ...],
       "anthropic": ["/v1/messages", ...],
   }
   ```

2. **Persist recovery state** to Redis or database:
   ```python
   # Survive process restarts
   # Prevent thundering herd on restart
   ```

3. **Add streaming size limits**:
   ```python
   # Prevent memory exhaustion
   # Disable logging for large streams
   ```

4. **Support recursive proxy chains**:
   ```python
   # Allow proxy → proxy → native model
   # Useful for model aliasing chains
   ```

5. **Add metrics/observability**:
   ```python
   # Track failover rates per connection
   # Monitor recovery state size
   # Alert on repeated failures
   ```

---

## 7. TESTING SCENARIOS

### Test Case 1: Model Resolution Priority

```python
def test_model_resolution_priority():
    # Given: Request with both body model and path model
    # When: Body model differs from path model
    # Then: Body model takes precedence
    
    request_body = b'{"model": "gpt-4o"}'
    request_path = "/v1/models/gpt-3.5-turbo:generateContent"
    
    model_id = _resolve_model_id(request_body, request_path)
    assert model_id == "gpt-4o"  # Body wins
```

### Test Case 2: Failover Trigger

```python
def test_failover_trigger_status_codes():
    # Given: Various HTTP status codes
    # When: should_failover() is called
    # Then: Only specific codes trigger failover
    
    assert should_failover(403) == True
    assert should_failover(429) == True
    assert should_failover(500) == True
    assert should_failover(502) == True
    assert should_failover(503) == True
    assert should_failover(529) == True
    
    assert should_failover(400) == False
    assert should_failover(401) == False
    assert should_failover(404) == False
    assert should_failover(200) == False
```

### Test Case 3: Recovery State Expiration

```python
def test_recovery_state_expiration():
    # Given: Connection marked failed at time 1000.0 with 60s cooldown
    # When: build_attempt_plan() called at different times
    # Then: Connection transitions from blocked to probe_eligible to healthy
    
    mark_connection_failed(profile_id=1, connection_id=1, cooldown_seconds=60, now_mono=1000.0)
    
    # At 1030.0: Still blocked
    plan = build_attempt_plan(profile_id=1, model_config, now_mono=1030.0)
    assert Connection(id=1) not in plan
    
    # At 1060.0: Probe eligible
    plan = build_attempt_plan(profile_id=1, model_config, now_mono=1060.0)
    assert Connection(id=1) in plan  # But after healthy connections
    
    # After success: Recovered
    mark_connection_recovered(profile_id=1, connection_id=1)
    plan = build_attempt_plan(profile_id=1, model_config, now_mono=1070.0)
    assert Connection(id=1) in plan  # In healthy section
```

