# Prism Proxy Routing: Deep Dive & Integration Points

**Date**: 2026-03-03  
**Scope**: Detailed code walkthroughs, provider-specific routing, initialization, and integration points

---

## 1. DETAILED CODE WALKTHROUGH: REQUEST ENTRY TO RESPONSE

### Step 1: FastAPI Route Matching

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
```

**What happens**:
- FastAPI matches `/v1/chat/completions` → `path = "chat/completions"`
- `{path:path}` is a special FastAPI syntax that captures the entire remaining path (including slashes)
- `request.body()` reads the entire request body as bytes (can only be called once)
- `get_active_profile_id` dependency resolves the active profile from database
- Control passes to `_handle_proxy()` with reconstructed full path `/v1/chat/completions`

**Key detail**: The `path` parameter does NOT include the leading `/v1/` prefix. It's reconstructed in the handler.

### Step 2: Model ID Resolution

```python
# backend/app/routers/proxy.py, lines 114-129
async def _handle_proxy(
    request: Request,
    db: AsyncSession,
    raw_body: bytes | None,
    request_path: str,
    profile_id: int,
):
    model_id = _resolve_model_id(raw_body, request_path)
    if not model_id:
        raise HTTPException(
            status_code=400,
            detail=(
                "Cannot determine model for routing. "
                "Include 'model' in the request body or use a Gemini-style model path."
            ),
        )
```

**Resolution logic** (lines 90-97):

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

**Execution trace for `POST /v1/chat/completions` with body `{"model": "gpt-4o"}`**:
1. `raw_body` is not None → skip first return
2. `extract_model_from_body(raw_body)` → parses JSON → returns `"gpt-4o"`
3. `model_id` is truthy → return `"gpt-4o"`
4. Path extraction never happens

**Execution trace for `POST /v1beta/projects/.../models/gemini-pro:generateContent` with no body**:
1. `raw_body` is None → return `_extract_model_from_path(request_path)`
2. Regex matches `/models/gemini-pro` → returns `"gemini-pro"`

### Step 3: Model Configuration Lookup

```python
# backend/app/routers/proxy.py, line 130
model_config = await get_model_config_with_connections(db, profile_id, model_id)
if not model_config:
    raise HTTPException(
        status_code=404, detail=f"Model '{model_id}' not configured or disabled"
    )
```

**Database query** (loadbalancer.py, lines 14-57):

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

    # Handle proxy model redirect
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

**What's loaded**:
- `ModelConfig` row with all columns
- `ModelConfig.connections` (list of Connection objects)
- For each Connection: `Connection.endpoint_rel` (the Endpoint object)
- `ModelConfig.provider` (the Provider object)

**Why selectinload**: Avoids N+1 queries. Without it, accessing `config.connections` would trigger another query.

### Step 4: Extract Provider Info

```python
# backend/app/routers/proxy.py, lines 141-144
provider_type = model_config.provider.provider_type  # "openai", "anthropic", or "gemini"
provider_id = model_config.provider.id
audit_enabled = model_config.provider.audit_enabled
audit_capture_bodies = model_config.provider.audit_capture_bodies
```

**Provider types** (hardcoded in database seed):
- `openai` → Auth header: `Authorization: Bearer {api_key}`
- `anthropic` → Auth header: `x-api-key: {api_key}` + extra header `anthropic-version: 2023-06-01`
- `gemini` → Auth header: `Authorization: Bearer {api_key}`

### Step 5: Build Attempt Plan

```python
# backend/app/routers/proxy.py, lines 178-184
now_mono = time.monotonic()
endpoints_to_try = build_attempt_plan(profile_id, model_config, now_mono)
if not endpoints_to_try:
    raise HTTPException(
        status_code=503,
        detail=f"No active connections available for model '{model_id}'. All connections may be in cooldown.",
    )
```

**Connection selection** (loadbalancer.py, lines 75-121):

```python
def build_attempt_plan(
    profile_id: int, model_config: ModelConfig, now_mono: float
) -> list[Connection]:
    active = get_active_connections(model_config)
    if not active:
        return []

    # Strategy 1: single
    if model_config.lb_strategy == "single":
        return [active[0]]

    # Strategy 2: failover without recovery
    if not model_config.failover_recovery_enabled:
        return active

    # Strategy 3: failover with recovery
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

**Active connections filter** (lines 60-72):

```python
def get_active_connections(model_config: ModelConfig) -> list[Connection]:
    active_connections = [
        connection
        for connection in model_config.connections
        if connection.is_active and connection.endpoint_rel is not None
    ]
    return sorted(active_connections, key=lambda connection: connection.priority)
```

**Filters**:
1. `connection.is_active == True` (database flag)
2. `connection.endpoint_rel is not None` (endpoint exists and is not deleted)
3. Sorted by `priority` (ascending)

### Step 6: Load Costing Settings

```python
# backend/app/routers/proxy.py, lines 186-197
costing_settings = await load_costing_settings(
    db,
    profile_id=profile_id,
    model_id=model_id,
    endpoint_ids=sorted(
        {
            endpoint.endpoint_id
            for endpoint in endpoints_to_try
            if endpoint.endpoint_id is not None
        }
    ),
)
```

**Purpose**: Fetch pricing configuration for cost calculation later.

### Step 7: Attempt Each Connection

```python
# backend/app/routers/proxy.py, lines 224-745
for ep in endpoints_to_try:
    # 7a. Check if connection is still active
    if not await _endpoint_is_active_now(db, ep.id):
        mark_connection_recovered(profile_id, ep.id)
        continue

    # 7b. Build upstream URL
    upstream_url = build_upstream_url(
        ep, effective_request_path, endpoint=ep.endpoint_rel
    )
    if request.url.query:
        upstream_url = f"{upstream_url}?{request.url.query}"

    # 7c. Build headers
    headers = build_upstream_headers(
        ep,
        provider_type,
        client_headers,
        blocklist_rules,
        endpoint=ep.endpoint_rel,
    )

    # 7d. Send request
    start_time = time.monotonic()
    try:
        if is_streaming:
            # ... streaming logic
        else:
            response = await proxy_request(
                client,
                method,
                upstream_url,
                headers,
                endpoint_body,
            )
            elapsed_ms = int((time.monotonic() - start_time) * 1000)

            # 7e. Check response status
            if response.status_code >= 400 and should_failover(response.status_code):
                # Failover: log, mark failed, continue
                mark_connection_failed(...)
                continue

            # 7f. Success: log and return
            tokens = extract_token_usage(response.content)
            rl_id = await log_request(...)
            if audit_enabled:
                await record_audit_log(...)
            if recovery_active:
                mark_connection_recovered(profile_id, ep.id)
            return Response(...)

    except httpx.ConnectError as e:
        # Connection error: log, mark failed, continue
        mark_connection_failed(...)
        continue
    except httpx.TimeoutException as e:
        # Timeout: log, mark failed, continue
        mark_connection_failed(...)
        continue

# All connections exhausted
if not attempted_any_endpoint:
    raise HTTPException(status_code=503, detail="No active connections available...")
raise HTTPException(status_code=502, detail="All connections failed. Last error: ...")
```

---

## 2. PROVIDER-SPECIFIC ROUTING DETAILS

### OpenAI Routing

```python
# Provider config (proxy_service.py, lines 12-17)
"openai": {
    "auth_header": "Authorization",
    "auth_prefix": "Bearer ",
    "extra_headers": {},
}

# Example request:
POST https://api.openai.com/v1/chat/completions
Authorization: Bearer sk-proj-...
Content-Type: application/json

{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}]
}

# Supported paths (no restrictions):
- /v1/chat/completions
- /v1/embeddings
- /v1/images/generations
- /v1/audio/speech
- /v1/files
- /v1/fine_tuning/jobs
- ... (any path under /v1/)
```

### Anthropic Routing

```python
# Provider config (proxy_service.py, lines 18-24)
"anthropic": {
    "auth_header": "x-api-key",
    "auth_prefix": "",
    "extra_headers": {
        "anthropic-version": "2023-06-01",
    },
}

# Example request:
POST https://api.anthropic.com/v1/messages
x-api-key: sk-ant-...
anthropic-version: 2023-06-01
Content-Type: application/json

{
  "model": "claude-3-sonnet-20250219",
  "messages": [{"role": "user", "content": "Hello"}]
}

# Supported paths (no restrictions):
- /v1/messages
- /v1/models
- ... (any path under /v1/)
```

### Gemini Routing

```python
# Provider config (proxy_service.py, lines 25-29)
"gemini": {
    "auth_header": "Authorization",
    "auth_prefix": "Bearer ",
    "extra_headers": {},
}

# Example request:
POST https://generativelanguage.googleapis.com/v1beta/projects/my-project/locations/us-central1/publishers/google/models/gemini-pro:generateContent
Authorization: Bearer {goog_api_key}
Content-Type: application/json

{
  "contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
}

# Model extraction from path:
# Path: /v1beta/projects/my-project/locations/us-central1/publishers/google/models/gemini-pro:generateContent
# Regex: /models/([^/:]+)
# Extracted: gemini-pro

# Supported paths (no restrictions):
- /v1beta/projects/{project}/locations/{location}/publishers/google/models/{model}:generateContent
- /v1beta/projects/{project}/locations/{location}/publishers/google/models/{model}:streamGenerateContent
- ... (any path under /v1beta/)
```

---

## 3. INITIALIZATION & STARTUP

### Backend Startup Sequence

```python
# backend/app/main.py (lifespan)
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup
    logger.info("Starting Prism backend...")
    
    # 1. Validate DATABASE_URL
    db_url = os.getenv("DATABASE_URL", "postgresql+asyncpg://prism:prism@localhost:5432/prism")
    if not db_url:
        raise ValueError("DATABASE_URL not set")
    
    # 2. Run migrations
    await run_migrations()  # Alembic upgrade head
    
    # 3. Seed providers (openai, anthropic, gemini)
    async with AsyncSessionLocal() as session:
        await seed_providers(session)
        await seed_user_settings(session)
        await seed_header_blocklist_rules(session)
        await session.commit()
    
    # 4. Create shared httpx client
    app.state.http_client = httpx.AsyncClient(
        timeout=httpx.Timeout(30.0),
        limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
    )
    
    yield
    
    # Shutdown
    await app.state.http_client.aclose()
```

### Seed Providers

```python
# Ensures these providers exist in database
async def seed_providers(session: AsyncSession):
    providers = [
        Provider(name="OpenAI", provider_type="openai", audit_enabled=False),
        Provider(name="Anthropic", provider_type="anthropic", audit_enabled=False),
        Provider(name="Google Gemini", provider_type="gemini", audit_enabled=False),
    ]
    for provider in providers:
        existing = await session.execute(
            select(Provider).where(Provider.provider_type == provider.provider_type)
        )
        if not existing.scalar_one_or_none():
            session.add(provider)
```

---

## 4. INTEGRATION POINTS WITH OTHER SERVICES

### Integration with Stats Service

```python
# backend/app/routers/proxy.py, lines 276-290 (on failover)
rl_id = await log_request(
    model_id=model_id,
    profile_id=profile_id,
    provider_type=provider_type,
    endpoint_id=ep.endpoint_id,
    connection_id=ep.id,
    endpoint_base_url=ep.endpoint_rel.base_url,
    endpoint_description=ep_desc,
    status_code=upstream_resp.status_code,
    response_time_ms=elapsed_ms,
    is_stream=True,
    request_path=request_path,
    error_detail=body.decode("utf-8", errors="replace")[:500],
    **build_cost_fields(ep, upstream_resp.status_code),
)
```

**What gets logged**:
- Request metadata (model, profile, provider, endpoint, connection)
- Response status and timing
- Token usage (if extractable from response)
- Cost fields (computed from pricing settings)
- Error details (first 500 chars)

**Used by**:
- `/api/stats/requests` - List request logs with filters
- `/api/stats/summary` - Aggregated statistics
- `/api/stats/spending` - Cost breakdown by model/endpoint/provider

### Integration with Audit Service

```python
# backend/app/routers/proxy.py, lines 291-311 (on failover)
if audit_enabled:
    await record_audit_log(
        request_log_id=rl_id,
        profile_id=profile_id,
        provider_id=provider_id,
        endpoint_id=ep.endpoint_id,
        connection_id=ep.id,
        endpoint_base_url=ep.endpoint_rel.base_url,
        endpoint_description=ep_desc,
        model_id=model_id,
        request_method=method,
        request_url=upstream_url,
        request_headers=headers,
        request_body=endpoint_body,
        response_status=upstream_resp.status_code,
        response_headers=dict(upstream_resp.headers),
        response_body=body,
        is_stream=True,
        duration_ms=elapsed_ms,
        capture_bodies=audit_capture_bodies,
    )
```

**What gets captured** (if enabled):
- Full request/response headers
- Request/response bodies (if `capture_bodies=true`)
- Timing information
- Connection metadata

**Redaction** (audit_service.py):
- Headers matching `authorization|x-api-key|x-goog-api-key|key|secret|token|auth` are redacted
- Sensitive headers never logged

**Used by**:
- `/api/audit/logs` - Query audit logs
- Compliance and debugging

### Integration with Costing Service

```python
# backend/app/routers/proxy.py, lines 199-216
def build_cost_fields(
    connection,
    status_code: int,
    tokens: dict[str, int | None] | None = None,
) -> CostFieldPayload:
    token_values = tokens or {}
    return compute_cost_fields(
        connection=connection,
        endpoint=connection.endpoint_rel,
        model_id=model_id,
        status_code=status_code,
        input_tokens=token_values.get("input_tokens"),
        output_tokens=token_values.get("output_tokens"),
        cache_read_input_tokens=token_values.get("cache_read_input_tokens"),
        cache_creation_input_tokens=token_values.get("cache_creation_input_tokens"),
        reasoning_tokens=token_values.get("reasoning_tokens"),
        settings=costing_settings,
    )
```

**Cost calculation**:
- Looks up pricing from `costing_settings` (loaded earlier)
- Multiplies tokens by per-token rates
- Stores as integer micros (e.g., $0.01 = 10000 micros)
- Handles cache tokens, reasoning tokens separately

**Used by**:
- `/api/stats/spending` - Cost breakdown
- `/api/settings/costing` - Pricing configuration

### Integration with Load Balancer

```python
# backend/app/routers/proxy.py, lines 218-221
recovery_active = (
    model_config.lb_strategy == "failover"
    and model_config.failover_recovery_enabled
)
```

**Recovery state management**:
- `mark_connection_failed()` - Add to recovery state with cooldown
- `mark_connection_recovered()` - Remove from recovery state
- `build_attempt_plan()` - Respect recovery state when ordering connections

---

## 5. CONFIGURATION & CUSTOMIZATION

### Model Configuration Options

```python
# ModelConfig table columns
id: int
profile_id: int
provider_id: int
model_id: str                              # e.g., "gpt-4o"
display_name: str | None                   # e.g., "GPT-4 Omni"
model_type: str                            # "native" or "proxy"
redirect_to: str | None                    # Target model_id for proxy models
lb_strategy: str                           # "single" or "failover"
failover_recovery_enabled: bool            # Enable recovery cooldown
failover_recovery_cooldown_seconds: int    # Default 60
is_enabled: bool                           # Enable/disable model
```

### Connection Configuration Options

```python
# Connection table columns
id: int
profile_id: int
model_config_id: int
endpoint_id: int
priority: int                              # Lower = higher priority
is_active: bool                            # Enable/disable connection
name: str                                  # Description
custom_headers: str | None                 # JSON-encoded custom headers
```

### Endpoint Configuration Options

```python
# Endpoint table columns
id: int
profile_id: int
base_url: str                              # e.g., "https://api.openai.com/v1"
api_key: str                               # Provider API key
description: str | None                    # Description
```

---

## 6. MONITORING & OBSERVABILITY HOOKS

### Logging Points

```python
# 1. Model resolution
logger.warning("Proxy lookup failed: model_id=%r not found or disabled", model_id)

# 2. Connection selection
logger.debug("get_active_connections for model %s: %d/%d active", model_id, active_count, total_count)
logger.debug("build_attempt_plan: single strategy using connection %d", connection_id)
logger.debug("build_attempt_plan: failover with recovery healthy=%s probe_eligible=%s", healthy, probe_eligible)

# 3. Failover events
logger.warning("Endpoint %d failed with %d, trying next", endpoint_id, status_code)
logger.info("Connection marked failed, cooldown %.0fs", cooldown_seconds)
logger.info("Connection recovered, removed from recovery state")

# 4. Streaming
logger.debug("Streaming response cancelled by client")
logger.warning("Stream iteration failed: %s", exception)
logger.exception("Failed to log streaming request")

# 5. Header processing
logger.warning("Dropping header '%s' due to invalid value", header_name)
logger.debug("Blocked header: %s", header_name)
```

### Metrics to Track

```python
# Recommended metrics for observability
- request_count (by model, provider, endpoint, status_code)
- request_latency_ms (by model, provider, endpoint)
- failover_count (by model, connection)
- recovery_state_size (in-memory dict size)
- streaming_payload_size (bytes accumulated)
- token_usage (input, output, cache, reasoning)
- cost_total (by model, provider, endpoint)
```

### Health Check Endpoints

```python
# Current: Manual health check via /api/connections/{id}/health
POST /api/connections/{id}/health

# Could add:
GET /health - Overall proxy health
GET /health/connections - Per-connection status
GET /health/recovery-state - Recovery state summary
```

---

## 7. QUERY PARAMETER HANDLING

### Query String Preservation

```python
# backend/app/routers/proxy.py, lines 240-241
if request.url.query:
    upstream_url = f"{upstream_url}?{request.url.query}"
```

**Example**:
```
Client request: GET /v1/models?limit=10&offset=20
Upstream URL: https://api.openai.com/v1/models?limit=10&offset=20
```

**Important**: Query parameters are preserved exactly as sent by client.

---

## 8. RESPONSE HEADER FILTERING

### Hop-by-Hop Headers Removed

```python
# backend/app/services/proxy_service.py, lines 226-232
HOP_BY_HOP_HEADERS = frozenset({
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
    "host",
})

def filter_response_headers(response_headers: httpx.Headers) -> dict[str, str]:
    filtered: dict[str, str] = {}
    for key, value in response_headers.items():
        if key.lower() not in HOP_BY_HOP_HEADERS and key.lower() != "content-length":
            filtered[key] = value
    return filtered
```

**Why**: These headers are managed by HTTP layer, not forwarded by proxies.

---

## 9. STREAMING RESPONSE FINALIZATION

### Detached Task Tracking

```python
# backend/app/routers/proxy.py, lines 49-58
def _track_detached_task(task: asyncio.Task[None], *, name: str) -> None:
    def _on_done(done_task: asyncio.Task[None]) -> None:
        try:
            done_task.result()
        except asyncio.CancelledError:
            logger.debug("%s cancelled before completion", name)
        except Exception:
            logger.exception("%s failed", name)

    task.add_done_callback(_on_done)
```

**Purpose**: Track background tasks that log streaming responses after stream completes.

**Why needed**: Request-scoped DB session is closed before streaming finishes, so logging must happen in a separate task with its own session.

---

## 10. SUMMARY: REQUEST LIFECYCLE

```
1. Client sends request to /v1/{path}
   ↓
2. FastAPI route handler extracts path and reads body
   ↓
3. Resolve model_id (body JSON → path regex → error)
   ↓
4. Lookup ModelConfig (with proxy redirect if needed)
   ↓
5. Extract provider info (openai/anthropic/gemini)
   ↓
6. Load blocklist rules and costing settings
   ↓
7. Build attempt plan (connection selection with recovery state)
   ↓
8. For each connection in attempt plan:
   a. Check if still active
   b. Build upstream URL (base_url + request_path)
   c. Build headers (client + auth + custom + blocklist)
   d. Rewrite body (model ID substitution if proxy)
   e. Send request (streaming or non-streaming)
   f. Check response status
   g. If failover trigger: mark failed, continue
   h. If success: log, mark recovered, return response
   i. If error: log, mark failed, continue
   ↓
9. If all connections exhausted: return 502 or 503
   ↓
10. Log request (stats service)
    ↓
11. Record audit log (if enabled)
    ↓
12. Return response to client
```

