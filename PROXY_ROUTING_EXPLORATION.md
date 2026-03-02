# Prism Backend: Proxy Routing Architecture Exploration

**Date**: 2026-03-03  
**Focus**: Catch-all request paths, model/provider inference, path allowlisting, and failover decision logic

---

## 1. CATCH-ALL ROUTE HANDLERS

### Entry Points

Two catch-all routes accept all HTTP methods and forward to upstream providers:

```python
# backend/app/routers/proxy.py, lines 748-756
@router.api_route("/v1/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE"])
async def proxy_catch_all(
    request: Request,
    path: str,
    db: Annotated[AsyncSession, Depends(get_db, scope="function")],
    profile_id: Annotated[int, Depends(get_active_profile_id)],
):
    raw_body = await request.body() or None
    return await _handle_proxy(request, db, raw_body, f"/v1/{path}", profile_id)

@router.api_route("/v1beta/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE"])
async def proxy_catch_all_v1beta(
    request: Request,
    path: str,
    db: Annotated[AsyncSession, Depends(get_db, scope="function")],
    profile_id: Annotated[int, Depends(get_active_profile_id)],
):
    raw_body = await request.body() or None
    return await _handle_proxy(request, db, raw_body, f"/v1beta/{path}", profile_id)
```

**Key observations:**
- Both routes use `{path:path}` to capture the entire remaining path as a single parameter
- All HTTP methods (GET, POST, PUT, PATCH, DELETE) are accepted
- Request body is read as raw bytes for later parsing
- Profile ID is resolved via `get_active_profile_id` dependency (always uses active profile, never management override)
- Query parameters are preserved and appended to upstream URL (line 240-241)

---

## 2. MODEL ID INFERENCE

### Resolution Strategy (3-tier fallback)

Model ID is resolved in `_resolve_model_id()` (lines 90-97):

```python
def _resolve_model_id(raw_body: bytes | None, request_path: str) -> str | None:
    if not raw_body:
        return _extract_model_from_path(request_path)
    model_id = extract_model_from_body(raw_body)
    if model_id:
        return model_id
    # Gemini-style: model is in the URL path, not the body.
    return _extract_model_from_path(request_path)
```

**Tier 1: Request Body (JSON `model` field)**
- Extracted via `extract_model_from_body()` (proxy_service.py, lines 280-289)
- Parses JSON minimally to read the `model` key
- Returns `None` on JSON decode errors

```python
def extract_model_from_body(raw_body: bytes) -> str | None:
    try:
        parsed = json.loads(raw_body)
        return parsed.get("model")
    except (json.JSONDecodeError, UnicodeDecodeError):
        return None
```

**Tier 2: URL Path (Gemini-style `/models/{model_id}` pattern)**
- Extracted via `_extract_model_from_path()` (lines 65-67)
- Uses regex: `_GEMINI_MODEL_RE = re.compile(r"/models/([^/:]+)")` (line 45)
- Matches `/models/{model_id}` anywhere in the path
- Example: `/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-pro:generateContent` → extracts `gemini-pro`

```python
def _extract_model_from_path(request_path: str) -> str | None:
    match = _GEMINI_MODEL_RE.search(request_path)
    return match.group(1) if match else None
```

**Fallback: No model found**
- Returns HTTP 400 with detail: "Cannot determine model for routing. Include 'model' in the request body or use a Gemini-style model path."

---

## 3. PROVIDER INFERENCE

### Lookup Flow

Once model ID is resolved, provider is determined via database lookup:

```python
# proxy.py, line 130
model_config = await get_model_config_with_connections(db, profile_id, model_id)
if not model_config:
    raise HTTPException(status_code=404, detail=f"Model '{model_id}' not configured or disabled")

# Extract provider info
provider_type = model_config.provider.provider_type  # "openai", "anthropic", or "gemini"
provider_id = model_config.provider.id
audit_enabled = model_config.provider.audit_enabled
audit_capture_bodies = model_config.provider.audit_capture_bodies
```

**Model Resolution Logic** (loadbalancer.py, lines 14-57):

1. Query `ModelConfig` by `(profile_id, model_id, is_enabled=True)`
2. Load relationships: `connections` + `endpoint_rel` + `provider`
3. **Proxy model handling**: If `model_type == "proxy"` and `redirect_to` is set:
   - Recursively fetch the target native model
   - Return target model's config (with its connections and provider)
   - Log warning if target not found or disabled

```python
async def get_model_config_with_connections(
    db: AsyncSession, profile_id: int, model_id: str
) -> ModelConfig | None:
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.connections).selectinload(Connection.endpoint_rel),
            selectinload(ModelConfig.provider),
        )
        .where(
            ModelConfig.profile_id == profile_id,
            ModelConfig.model_id == model_id,
            ModelConfig.is_enabled.is_(True),
        )
    )
    config = result.scalar_one_or_none()
    if not config:
        return None

    # Proxy model redirect
    if config.model_type == "proxy" and config.redirect_to:
        target_result = await db.execute(
            select(ModelConfig)
            .options(
                selectinload(ModelConfig.connections).selectinload(Connection.endpoint_rel),
                selectinload(ModelConfig.provider),
            )
            .where(
                ModelConfig.profile_id == profile_id,
                ModelConfig.model_id == config.redirect_to,
                ModelConfig.is_enabled.is_(True),
            )
        )
        target = target_result.scalar_one_or_none()
        if not target:
            logger.warning(...)
            return None
        return target

    return config
```

**Supported Providers** (hardcoded):
- `openai`
- `anthropic`
- `gemini`

---

## 4. PATH ALLOWLISTING & RESTRICTIONS

### No Per-Provider Path Restrictions

**Finding**: There are **NO explicit per-provider path allowlists or restrictions** in the proxy code.

All paths are forwarded as-is to the upstream provider. The proxy:
1. Accepts any path via `{path:path}` catch-all
2. Preserves the path exactly when building upstream URL
3. Does not validate or filter paths based on provider type

### Path Rewriting (Model ID Substitution Only)

The only path transformation is **model ID rewriting** for proxy models:

```python
# proxy.py, lines 155-160
path_model = _extract_model_from_path(request_path)
effective_request_path = request_path
if path_model and upstream_model_id != path_model:
    effective_request_path = _rewrite_model_in_path(
        request_path, path_model, upstream_model_id
    )
```

**Example**: If request path is `/v1/models/claude-sonnet-4-5:generateContent` but the proxy model resolves to `claude-sonnet-4-5-20250929`, the path is rewritten to `/v1/models/claude-sonnet-4-5-20250929:generateContent`.

```python
def _rewrite_model_in_path(
    request_path: str, original_model: str, target_model: str
) -> str:
    if original_model == target_model:
        return request_path
    return request_path.replace(
        f"/models/{original_model}", f"/models/{target_model}", 1
    )
```

### Upstream URL Construction

```python
# proxy_service.py, lines 82-94
def build_upstream_url(
    connection: Connection | Endpoint,
    request_path: str,
    endpoint: Endpoint | None = None,
) -> str:
    """Forward the request path to the endpoint base URL without path normalization."""
    endpoint_obj = endpoint or connection
    parsed = httpx.URL(str(endpoint_obj.base_url or ""))
    base_path = parsed.path.rstrip("/")
    req_path = request_path if request_path.startswith("/") else f"/{request_path}"
    final_path = f"{base_path}{req_path}"
    return str(parsed.copy_with(path=final_path))
```

**Flow**:
1. Parse endpoint base URL (e.g., `https://api.openai.com/v1`)
2. Extract base path component (e.g., `/v1`)
3. Append request path (e.g., `/chat/completions`)
4. Result: `https://api.openai.com/v1/chat/completions`

---

## 5. FAILOVER DECISION LOGIC

### Failover Trigger Status Codes

```python
# proxy_service.py, line 32
FAILOVER_STATUS_CODES = {403, 429, 500, 502, 503, 529}

# proxy_service.py, lines 276-277
def should_failover(status_code: int) -> bool:
    return status_code in FAILOVER_STATUS_CODES
```

**Triggered on**:
- `403` - Forbidden (auth/quota issue)
- `429` - Too Many Requests (rate limit)
- `500` - Internal Server Error
- `502` - Bad Gateway
- `503` - Service Unavailable
- `529` - Site Overloaded (Google-specific)

### Failover Flow in Proxy Handler

The proxy handler iterates through connections in priority order (lines 224-745):

```python
endpoints_to_try = build_attempt_plan(profile_id, model_config, now_mono)
if not endpoints_to_try:
    raise HTTPException(status_code=503, detail="No active connections available...")

for ep in endpoints_to_try:
    # Skip if connection is disabled
    if not await _endpoint_is_active_now(db, ep.id):
        mark_connection_recovered(profile_id, ep.id)
        continue

    # Build upstream request
    upstream_url = build_upstream_url(ep, effective_request_path, endpoint=ep.endpoint_rel)
    headers = build_upstream_headers(ep, provider_type, client_headers, blocklist_rules, endpoint=ep.endpoint_rel)

    # Send request (streaming or non-streaming)
    try:
        if is_streaming:
            # ... streaming logic
            if upstream_resp.status_code >= 400:
                if should_failover(upstream_resp.status_code):
                    # Log failure, mark connection failed, continue to next
                    mark_connection_failed(profile_id, ep.id, cooldown_seconds, now_mono)
                    continue
        else:
            response = await proxy_request(client, method, upstream_url, headers, endpoint_body)
            if response.status_code >= 400 and should_failover(response.status_code):
                # Log failure, mark connection failed, continue to next
                mark_connection_failed(profile_id, ep.id, cooldown_seconds, now_mono)
                continue

        # Success: return response
        if recovery_active:
            mark_connection_recovered(profile_id, ep.id)
        return Response(...)

    except httpx.ConnectError as e:
        # Connection error: log, mark failed, continue
        mark_connection_failed(profile_id, ep.id, cooldown_seconds, now_mono)
        continue
    except httpx.TimeoutException as e:
        # Timeout: log, mark failed, continue
        mark_connection_failed(profile_id, ep.id, cooldown_seconds, now_mono)
        continue

# All connections exhausted
if not attempted_any_endpoint:
    raise HTTPException(status_code=503, detail="No active connections available...")
raise HTTPException(status_code=502, detail="All connections failed. Last error: ...")
```

### Connection Selection Strategy

```python
# loadbalancer.py, lines 75-121
def build_attempt_plan(
    profile_id: int, model_config: ModelConfig, now_mono: float
) -> list[Connection]:
    active = get_active_connections(model_config)
    if not active:
        return []

    # Strategy 1: Single (always use first active connection)
    if model_config.lb_strategy == "single":
        return [active[0]]

    # Strategy 2: Failover without recovery (try all in priority order)
    if not model_config.failover_recovery_enabled:
        return active

    # Strategy 3: Failover with recovery (separate healthy from probe-eligible)
    healthy: list[Connection] = []
    probe_eligible: list[Connection] = []

    for connection in active:
        state = _recovery_state.get((profile_id, connection.id))
        if state is None:
            healthy.append(connection)
        else:
            blocked_until, _ = state
            if now_mono >= blocked_until:
                probe_eligible.append(connection)

    return healthy + probe_eligible
```

**Strategies**:

1. **`single`**: Always use first active connection (priority 0). No failover.
2. **`failover` (without recovery)**: Try all active connections in priority order until one succeeds.
3. **`failover` (with recovery)**: 
   - Maintain in-memory recovery state: `_recovery_state: dict[tuple[int, int], tuple[float, float]]`
   - Key: `(profile_id, connection_id)`
   - Value: `(blocked_until_mono, cooldown_seconds)`
   - On failure: mark connection blocked for `failover_recovery_cooldown_seconds` (default 60s)
   - On success: remove from recovery state
   - Attempt order: healthy connections first, then probe-eligible (cooldown expired)

### Recovery State Management

```python
# loadbalancer.py, lines 124-149
def mark_connection_failed(
    profile_id: int,
    connection_id: int,
    cooldown_seconds: float,
    now_mono: float,
) -> None:
    blocked_until = now_mono + cooldown_seconds
    _recovery_state[(profile_id, connection_id)] = (blocked_until, cooldown_seconds)
    logger.info("Connection marked failed, cooldown %.0fs, blocked until mono=%.1f", ...)

def mark_connection_recovered(profile_id: int, connection_id: int) -> None:
    key = (profile_id, connection_id)
    if key in _recovery_state:
        del _recovery_state[key]
        logger.info("Connection recovered, removed from recovery state", ...)
```

**Important**: Recovery state is **in-memory and process-scoped**. It resets on process restart.

---

## 6. REQUEST BODY REWRITING

### Model ID Substitution in Body

When a proxy model resolves to a different native model, the request body is rewritten:

```python
# proxy.py, lines 149-153
upstream_model_id = model_config.model_id
body_model_id = extract_model_from_body(raw_body) if raw_body else None
rewritten_body = raw_body
if raw_body and body_model_id and upstream_model_id != body_model_id:
    rewritten_body = _rewrite_model_in_body(raw_body, upstream_model_id)
```

```python
# proxy.py, lines 100-111
def _rewrite_model_in_body(raw_body: bytes, target_model_id: str) -> bytes:
    try:
        payload = json.loads(raw_body)
    except (json.JSONDecodeError, UnicodeDecodeError, TypeError):
        return raw_body
    if not isinstance(payload, dict):
        return raw_body
    payload["model"] = target_model_id
    try:
        return json.dumps(payload).encode("utf-8")
    except (TypeError, ValueError):
        return raw_body
```

**Example**: Request with `{"model": "claude-sonnet-4-5", ...}` is rewritten to `{"model": "claude-sonnet-4-5-20250929", ...}` if the proxy model resolves to the versioned model.

---

## 7. HEADER HANDLING

### Header Blocklist Application

Before forwarding, headers are sanitized against blocklist rules:

```python
# proxy.py, lines 162-176
blocklist_rules: list[HeaderBlocklistRule] = list(
    (
        (
            await db.execute(
                select(HeaderBlocklistRule).where(
                    HeaderBlocklistRule.enabled == True,
                    (HeaderBlocklistRule.is_system == True)
                    | (HeaderBlocklistRule.profile_id == profile_id),
                )
            )
        )
        .scalars()
        .all()
    )
)
```

Blocklist rules are applied in `build_upstream_headers()` (proxy_service.py, lines 119-199):

1. **Client headers** (minus hop-by-hop, minus client auth, minus proxy-controlled)
2. **Provider auth headers** (e.g., `Authorization: Bearer {api_key}`)
3. **Provider extra headers** (e.g., `anthropic-version: 2023-06-01`)
4. **Endpoint custom headers** (JSON-encoded, applied last)
5. **Blocklist sanitization** (applied twice: on client headers and on final merged result)

```python
def build_upstream_headers(
    connection: Connection | Endpoint,
    provider_type: str,
    client_headers: dict[str, str] | None = None,
    blocklist_rules: list[HeaderBlocklistRule] | None = None,
    endpoint: Endpoint | None = None,
) -> dict[str, str]:
    # 1. Client headers (filtered)
    headers: dict[str, str] = {}
    if client_headers:
        for key, value in client_headers.items():
            k_lower = key.lower()
            if (
                k_lower not in HOP_BY_HOP_HEADERS
                and k_lower not in CLIENT_AUTH_HEADERS
                and k_lower != "content-length"
                and k_lower != "accept-encoding"
                and k_lower not in proxy_controlled_headers
            ):
                headers[key] = value

    # 2. Apply blocklist to client headers
    if blocklist_rules:
        headers = sanitize_headers(headers, blocklist_rules)

    # 3. Provider auth
    headers[config["auth_header"]] = f"{config['auth_prefix']}{normalized_api_key}"
    headers.update(config["extra_headers"])

    # 4. Endpoint custom headers
    if custom_headers:
        custom = json.loads(custom_headers)
        headers.update(custom)

    # 5. Apply blocklist to final result (protecting auth headers)
    if blocklist_rules:
        sanitized = {}
        for key, value in headers.items():
            if key.lower() in protected:
                sanitized[key] = value
            elif not header_is_blocked(key, blocklist_rules):
                sanitized[key] = value
        headers = sanitized

    return _normalize_header_values(headers)
```

---

## 8. STREAMING VS NON-STREAMING

### Stream Flag Detection

```python
# proxy_service.py, lines 292-297
def extract_stream_flag(raw_body: bytes) -> bool:
    try:
        parsed = json.loads(raw_body)
        return bool(parsed.get("stream", False))
    except (json.JSONDecodeError, UnicodeDecodeError):
        return False
```

### Streaming Response Handling

For streaming responses, the proxy:
1. Builds the upstream request with `stream=True`
2. Returns a `StreamingResponse` with an async generator
3. Accumulates chunks in memory
4. On stream completion, logs the full accumulated payload in a detached task

```python
# proxy.py, lines 256-517
if is_streaming:
    send_req = client.build_request(method, upstream_url, **kwargs)
    upstream_resp = await client.send(send_req, stream=True)

    # ... error handling ...

    async def _iter_and_log(resp: httpx.Response) -> AsyncGenerator[bytes, None]:
        accumulated = bytearray()
        try:
            async for chunk in resp.aiter_bytes():
                if chunk:
                    accumulated.extend(chunk)
                    yield chunk
        finally:
            payload = bytes(accumulated)
            async def _finalize_stream() -> None:
                # Log request with full payload
                rl_id = await log_request(...)
                if audit_enabled:
                    await record_audit_log(...)

            finalize_task = asyncio.create_task(_finalize_stream(), name="proxy-stream-finalize")
            # Track detached task to handle cancellation gracefully

    return StreamingResponse(
        _iter_and_log(upstream_resp),
        status_code=upstream_resp.status_code,
        media_type=upstream_resp.headers.get("content-type", "text/event-stream"),
        headers={**resp_headers_filtered, "Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )
```

---

## 9. ERROR HANDLING & LOGGING

### Exception Handling

Three exception types are caught and trigger failover:

1. **`httpx.ConnectError`** (lines 639-686): Connection refused, DNS failure, etc.
2. **`httpx.TimeoutException`** (lines 687-734): Request timeout
3. **HTTP status codes** (lines 267-581): Failover-triggering status codes

All three log the request and mark the connection as failed.

### Final Error Responses

If all connections are exhausted:

```python
# proxy.py, lines 736-745
if not attempted_any_endpoint:
    raise HTTPException(
        status_code=503,
        detail=f"No active connections available for model '{model_id}'.",
    )

raise HTTPException(
    status_code=502,
    detail=f"All connections failed for model '{model_id}'. Last error: {last_error}",
)
```

---

## 10. SUMMARY TABLE

| Aspect | Behavior |
|--------|----------|
| **Catch-all routes** | `/v1/{path:path}` and `/v1beta/{path:path}` accept all HTTP methods |
| **Model inference** | Body JSON `model` field → Gemini URL path `/models/{id}` → 400 error |
| **Provider inference** | Via `ModelConfig.provider_id` lookup after model resolution |
| **Path restrictions** | None. All paths forwarded as-is to upstream |
| **Path rewriting** | Only model ID substitution for proxy models |
| **Failover triggers** | Status codes: 403, 429, 500, 502, 503, 529; ConnectError; TimeoutException |
| **Failover strategy** | `single` (no failover) or `failover` (with optional recovery cooldown) |
| **Recovery state** | In-memory, process-scoped, keyed by `(profile_id, connection_id)` |
| **Streaming** | Full payload accumulated and logged after stream completes |
| **Header handling** | Client headers → provider auth → custom headers → blocklist sanitization |
| **Body rewriting** | Model ID substitution only (for proxy models) |

