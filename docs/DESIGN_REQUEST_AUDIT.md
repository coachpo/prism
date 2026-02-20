# Design: Request Audit Logging

## Goal

Record full HTTP request/response data for all proxied requests into the database for auditing purposes. The feature is controlled per provider (e.g., enable audit for OpenAI but not Anthropic, and disable body capture for a specific provider). Sensitive information (API keys, auth headers) is redacted before storage. A dedicated Audit page in the frontend allows browsing and inspecting recorded requests.

This replaces the existing Anthropic-only `print_raw_request_if_anthropic` console logging with a persistent, provider-agnostic, database-backed audit trail.

No backward compatibility — this is a new feature addition.

---

## 1. Scope

### In Scope
- Record raw HTTP request (method, URL, headers, body) and response (status, headers, body) for every upstream attempt (including failover attempts)
- Per-provider controls (`audit_enabled`, `audit_capture_bodies` on `providers` table)
- Redaction of sensitive data (API keys, auth tokens) before storage
- Audit log browsing page in the frontend with filtering and detail view
- REST API for querying audit logs
- Non-blocking recording — audit failures must never affect proxy behavior

### Out of Scope
- Audit log export (can be added later)
- Automatic scheduled cleanup (manual preset-based deletion is provided instead)
- Audit of non-proxy requests (config API, health checks)
- Full-text search within request/response bodies

---

## 2. Data Model

### 2.1 Provider Change

Add audit columns to `providers` table:

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| audit_enabled | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether to record audit logs for this provider's proxy requests |
| audit_capture_bodies | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether request/response bodies are stored for this provider's audited requests |

Default is `FALSE` — audit is opt-in per provider.

### 2.2 New Table: `audit_logs`

Stores the full HTTP request and response for audited proxy requests.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| request_log_id | INTEGER | FK → request_logs.id, NULLABLE, UNIQUE, ON DELETE SET NULL | Link to the corresponding request_log entry for this upstream attempt |
| provider_id | INTEGER | FK → providers.id, NOT NULL | Provider that handled this request |
| model_id | VARCHAR(200) | NOT NULL | Model ID from the request |
| request_method | VARCHAR(10) | NOT NULL | HTTP method (POST, GET, etc.) |
| request_url | VARCHAR(2000) | NOT NULL | Full upstream URL the request was sent to |
| request_headers | TEXT | NOT NULL | JSON object of request headers (redacted) |
| request_body | TEXT | NULLABLE | Request body as text. NULL if body capture is disabled or no body. |
| response_status | INTEGER | NOT NULL | HTTP status code from upstream |
| response_headers | TEXT | NULLABLE | JSON object of response headers |
| response_body | TEXT | NULLABLE | Response body as text. NULL for streaming responses or when body capture is disabled. |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether this was a streaming request |
| duration_ms | INTEGER | NOT NULL | Total request duration in milliseconds |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | When the audit record was created |

### 2.3 Indexes

```sql
CREATE INDEX idx_audit_logs_provider_id ON audit_logs(provider_id);
CREATE INDEX idx_audit_logs_model_id ON audit_logs(model_id);
CREATE INDEX idx_audit_logs_response_status ON audit_logs(response_status);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_request_log_id ON audit_logs(request_log_id);
```

### 2.4 Relationships

- `providers` 1:N `audit_logs` — One provider can have many audit records
- `request_logs` 1:0..1 `audit_logs` — Each upstream-attempt request log may have one corresponding audit log

### 2.5 Storage Considerations

- **Streaming responses**: `response_body` is stored as `NULL` for streaming requests. The accumulated SSE chunks can be very large and are not practical to store. The response headers and status are still captured.
- **Body capture toggle**: If `providers.audit_capture_bodies = FALSE`, both `request_body` and `response_body` are stored as `NULL` for that provider.
- **Body size limit**: When body capture is enabled, request and response bodies are truncated to 64KB before storage. This covers virtually all LLM API requests while preventing unbounded growth.
- **Text encoding**: Bodies are decoded as UTF-8 with replacement characters for invalid bytes, same as existing `error_detail` handling.

---

## 3. Redaction

### 3.1 What Gets Redacted

All sensitive values are replaced with `[REDACTED]` before storage:

**Request Headers:**
- `authorization` → `Bearer [REDACTED]`
- `x-api-key` → `[REDACTED]`
- `x-goog-api-key` → `[REDACTED]`
- Any header whose name contains `key`, `secret`, `token`, `auth` (case-insensitive) → value replaced with `[REDACTED]`

**Request Body:**
- Not header-redacted. May contain sensitive user-provided data (prompts, inline credentials, PII).
- Captured only when `providers.audit_capture_bodies = TRUE`.

**Response Headers:**
- Same rules as request headers (though upstream responses rarely contain auth headers)

**Response Body:**
- Not header-redacted. May contain sensitive generated or echoed data.
- Captured only when `providers.audit_capture_bodies = TRUE` and the request is non-streaming.

### 3.2 Redaction Implementation

A single `redact_headers(headers: dict) -> dict` function that applies all rules. Called before serializing headers to JSON for storage.

The redaction is applied at write time (before INSERT), not at read time. This means:
- Sensitive data never touches the database
- No risk of accidental exposure through direct DB access
- Redaction logic is centralized in one function

---

## 4. Recording Flow

### 4.1 Non-Streaming Requests

```
Client → Proxy Router → resolve model + provider
                      → check provider.audit_enabled
                      → ProxyService forwards attempt #1 to upstream
                      → Upstream responds
                      → Log attempt #1 to request_logs (existing)
                      → If audit_enabled: log attempt #1 to audit_logs (new, non-blocking)
                      → If failover triggered: repeat per additional attempt
                      → Return response to client
```

### 4.2 Streaming Requests

```
Client → Proxy Router → resolve model + provider
                      → check provider.audit_enabled
                      → ProxyService opens streaming connection (attempt #N)
                      → SSE chunks piped to client
                      → On stream complete (finally block):
                          → Log attempt #N to request_logs (existing)
                          → If audit_enabled: log attempt #N to audit_logs (new, non-blocking)
                            (response_body = NULL for streaming)
```

### 4.3 Non-Interference Guarantee

Audit logging MUST NOT affect proxy behavior:

1. **Async best-effort**: Audit log INSERT runs in a try/except path decoupled from client-facing error handling. Failures are logged to console but never propagated.
2. **Separate DB session**: For streaming requests, audit logging uses its own `AsyncSessionLocal()` (same pattern as existing stream logging in `_iter_and_log`).
3. **No request modification**: Audit captures data by reading existing variables (headers, body, response). It does not modify the request or response pipeline.
4. **Minimal latency impact**: Audit work is best-effort and does not gate response success/failure semantics.

### 4.4 Audit Toggle Check

The provider audit flags are checked once per request, after model/provider resolution but before the upstream call. The provider object is already loaded by `get_model_config_with_endpoints()`, so no additional DB query is needed.

```python
# In _handle_proxy(), after model resolution:
audit_enabled = model_config.provider.audit_enabled
audit_capture_bodies = model_config.provider.audit_capture_bodies
```

If `audit_enabled` is `False`, no audit data is captured.

---

## 5. Backend API

### 5.1 Provider Update (Modified)

`GET /api/providers` and `GET /api/providers/{id}` responses include audit fields. A new `PATCH /api/providers/{id}` endpoint updates provider audit settings.

#### Updated Provider Response

```json
{
  "id": 1,
  "name": "OpenAI",
  "provider_type": "openai",
  "description": "OpenAI API (GPT models)",
  "audit_enabled": false,
  "audit_capture_bodies": true,
  "created_at": "2025-01-01T00:00:00Z",
  "updated_at": "2025-01-01T00:00:00Z"
}
```

#### Toggle Audit

```
PATCH /api/providers/{id}
Content-Type: application/json
```

Request:
```json
{
  "audit_enabled": true,
  "audit_capture_bodies": false
}
```

Response `200`: Updated provider object.

Both fields are optional in PATCH; omitted fields are unchanged.

### 5.2 List Audit Logs

```
GET /api/audit/logs
```

Query parameters:
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| provider_id | integer | — | Filter by provider |
| model_id | string | — | Filter by model ID |
| status_code | integer | — | Filter by response status code |
| from_time | datetime | — | Start of time range (ISO 8601) |
| to_time | datetime | — | End of time range (ISO 8601) |
| limit | integer | 50 | Max results (1-200) |
| offset | integer | 0 | Pagination offset |

Each row represents one upstream attempt. If failover occurs, each attempt appears as a separate audit row.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "request_log_id": 42,
      "provider_id": 1,
      "model_id": "gpt-4o",
      "request_method": "POST",
      "request_url": "https://api.openai.com/v1/chat/completions",
      "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"Bearer [REDACTED]\"}",
      "request_body_preview": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"con...",
      "response_status": 200,
      "is_stream": false,
      "duration_ms": 1234,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

The list endpoint returns a preview of the request body (first 200 chars) to keep response sizes manageable. Full bodies are available in the detail endpoint.
If provider body capture is disabled, `request_body_preview` is `null`.
Rows are ordered by `created_at DESC`.

### 5.3 Get Audit Log Detail

```
GET /api/audit/logs/{id}
```

Response `200`:
```json
{
  "id": 1,
  "request_log_id": 42,
  "provider_id": 1,
  "model_id": "gpt-4o",
  "request_method": "POST",
  "request_url": "https://api.openai.com/v1/chat/completions",
  "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"Bearer [REDACTED]\"}",
  "request_body": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello!\"}],\"temperature\":0.7}",
  "response_status": 200,
  "response_headers": "{\"content-type\": \"application/json\", \"x-request-id\": \"req_abc123\"}",
  "response_body": "{\"id\":\"chatcmpl-abc\",\"choices\":[...],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}",
  "is_stream": false,
  "duration_ms": 1234,
  "created_at": "2025-01-15T10:30:00Z"
}
```

For streaming requests, `response_body` is `null`.
If provider body capture is disabled, both `request_body` and `response_body` are `null`.

### 5.4 Delete Audit Logs (Batch)

```
DELETE /api/audit/logs
```

Query parameters:
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| before | datetime | — | Delete logs created before this time (ISO 8601). |
| older_than_days | integer | — | Delete logs older than N days. Must be ≥ 1. |
| delete_all | boolean | false | Delete all audit logs. |

Exactly one of `before`, `older_than_days`, or `delete_all=true` must be provided.

When using `older_than_days`, the cutoff is computed server-side from UTC app time as `current_utc - N days`.

Response `200`:
```json
{
  "deleted_count": 1234
}
```

Response `400`: Missing or conflicting parameters.

This provides both preset-based cleanup (for the Settings page UI) and custom datetime cleanup (for API consumers).

### 5.5 Pydantic Schemas

Add to `backend/app/schemas/schemas.py`:

```python
# --- Provider Update (add audit fields) ---

class ProviderUpdate(BaseModel):
    audit_enabled: bool | None = None
    audit_capture_bodies: bool | None = None

# --- Audit Log Schemas ---

class AuditLogListItem(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    request_log_id: int | None
    provider_id: int
    model_id: str
    request_method: str
    request_url: str
    request_headers: str  # JSON string
    request_body_preview: str | None  # First 200 chars
    response_status: int
    is_stream: bool
    duration_ms: int
    created_at: datetime

class AuditLogDetail(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    request_log_id: int | None
    provider_id: int
    model_id: str
    request_method: str
    request_url: str
    request_headers: str  # JSON string
    request_body: str | None
    response_status: int
    response_headers: str | None  # JSON string
    response_body: str | None
    is_stream: bool
    duration_ms: int
    created_at: datetime

class AuditLogListResponse(BaseModel):
    items: list[AuditLogListItem]
    total: int
    limit: int
    offset: int

class AuditLogDeleteResponse(BaseModel):
    deleted_count: int
```

---

## 6. Frontend

### 6.1 Provider Audit Toggle

Add provider-level audit controls to the provider display. Since providers are not directly editable in the current UI (they're seed data), the controls are shown:

- **Dashboard page**: In the provider section or as a global setting
- **Settings page**: Under a new "Audit Configuration" section

The controls call `PATCH /api/providers/{id}` with audit fields.

#### Settings Page Addition

Add a new section to the existing Settings page (`/settings`):

```
Audit Configuration
─────────────────────────────────────────

Enable request/response recording per provider.
When enabled, upstream request attempts are recorded.
Body capture can be disabled per provider for privacy.
Sensitive headers (API keys) are automatically redacted.

┌─────────────────────────────────────────────────────┐
│  OpenAI           Audit [ON]   Bodies [OFF]         │
│  Anthropic        Audit [OFF]  Bodies [ON]          │
│  Google Gemini    Audit [OFF]  Bodies [ON]          │
└─────────────────────────────────────────────────────┘
```

### 6.2 Audit Page

New page at `/audit` accessible from sidebar navigation.

#### Nav Link
- Icon: `FileSearch` from lucide-react
- Label: "Audit"
- Route: `/audit`
- Position: Between "Statistics" and "Settings" in sidebar

#### Page Layout

```
Audit Logs
─────────────────────────────────────────

┌─ Filters ──────────────────────────────────────────┐
│ Provider: [All ▾]  Model: [All ▾]  Status: [All ▾] │
│ Time Range: [Last 24h ▾]  [From] [To]              │
│                                        [ Clear All ] │
└────────────────────────────────────────────────────┘

┌─ Results (showing 1-50 of 234) ────────────────────┐
│ Time       │ Model   │ Provider │ Method │ URL      │
│            │         │          │        │ (trunc)  │
│ Status │ Duration │ Stream │ Actions              │
├────────────────────────────────────────────────────┤
│ 10:30:00  │ gpt-4o  │ OpenAI  │ POST   │ /v1/ch.. │
│ 200     │ 1234ms  │ No     │ [View]              │
│ 10:29:45  │ claude  │ Anthro  │ POST   │ /v1/me.. │
│ 200     │ 890ms   │ Yes    │ [View]              │
└────────────────────────────────────────────────────┘

                    [← Prev] Page 1 of 5 [Next →]
```

#### Detail View (Modal or Slide-over)

Clicking "View" on a row opens a detail panel showing:

```
Audit Log #42
─────────────────────────────────────────

Request
  Method: POST
  URL: https://api.openai.com/v1/chat/completions
  Headers:
    content-type: application/json
    authorization: Bearer [REDACTED]
    user-agent: ...
  Body:
  ┌──────────────────────────────────────┐
  │ {                                     │
  │   "model": "gpt-4o",                 │
  │   "messages": [                       │
  │     {"role": "user",                  │
  │      "content": "Hello!"}             │
  │   ],                                  │
  │   "temperature": 0.7                  │
  │ }                                     │
  └──────────────────────────────────────┘

Response
  Status: 200
  Headers:
    content-type: application/json
    x-request-id: req_abc123
  Body:
  ┌──────────────────────────────────────┐
  │ {                                     │
  │   "id": "chatcmpl-abc",              │
  │   "choices": [...],                   │
  │   "usage": {                          │
  │     "prompt_tokens": 10,              │
  │     "completion_tokens": 20           │
  │   }                                   │
  │ }                                     │
  └──────────────────────────────────────┘

  Duration: 1234ms
  Stream: No
  Recorded: 2025-01-15 10:30:00
```

Headers and bodies are displayed in a monospace code block with JSON pretty-printing where applicable.

For streaming requests, the response body section shows: "Response body not recorded for streaming requests."

### 6.3 API Client Additions

In `frontend/src/lib/api.ts`:

```typescript
audit: {
  list: (params?: AuditLogParams) => {
    const qs = new URLSearchParams();
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        if (v !== undefined && v !== null && v !== "") qs.set(k, String(v));
      });
    }
    const query = qs.toString();
    return request<AuditLogListResponse>(
      `/api/audit/logs${query ? `?${query}` : ""}`
    );
  },
  get: (id: number) => request<AuditLogDetail>(`/api/audit/logs/${id}`),
  delete: (params: { before?: string; older_than_days?: number }) => {
    const qs = new URLSearchParams();
    if (params.before) qs.set("before", params.before);
    if (params.older_than_days) qs.set("older_than_days", String(params.older_than_days));
    return request<AuditLogDeleteResponse>(
      `/api/audit/logs?${qs.toString()}`,
      { method: "DELETE" }
    );
  },
},

providers: {
  // ... existing methods ...
  update: (id: number, data: ProviderUpdate) =>
    request<Provider>(`/api/providers/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),
},
```

### 6.4 Type Additions

In `frontend/src/lib/types.ts`:

```typescript
// --- Provider (updated) ---
export interface Provider {
  id: number;
  name: string;
  provider_type: string;
  description: string | null;
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProviderUpdate {
  audit_enabled?: boolean;
  audit_capture_bodies?: boolean;
}

// --- Audit Log ---
export interface AuditLogListItem {
  id: number;
  request_log_id: number | null;
  provider_id: number;
  model_id: string;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body_preview: string | null;
  response_status: number;
  is_stream: boolean;
  duration_ms: number;
  created_at: string;
}

export interface AuditLogDetail {
  id: number;
  request_log_id: number | null;
  provider_id: number;
  model_id: string;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body: string | null;
  response_status: number;
  response_headers: string | null;
  response_body: string | null;
  is_stream: boolean;
  duration_ms: number;
  created_at: string;
}

export interface AuditLogListResponse {
  items: AuditLogListItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface AuditLogParams {
  provider_id?: number;
  model_id?: string;
  status_code?: number;
  from_time?: string;
  to_time?: string;
  limit?: number;
  offset?: number;
}

export interface AuditLogDeleteResponse {
  deleted_count: number;
}
```

---

## 7. File Inventory

### New Files
| File | Description |
|------|-------------|
| `backend/app/routers/audit.py` | Audit log API endpoints (list, detail, delete) |
| `backend/app/services/audit_service.py` | Audit recording logic, redaction, DB writes |
| `frontend/src/pages/AuditPage.tsx` | Audit log browsing page with filters and detail view |

### Modified Files
| File | Change |
|------|--------|
| `backend/app/models/models.py` | Add `AuditLog` ORM model; add provider audit fields (`audit_enabled`, `audit_capture_bodies`) |
| `backend/app/schemas/schemas.py` | Add audit schemas; add provider audit fields to provider schemas; add `ProviderUpdate` schema |
| `backend/app/main.py` | Register audit router; add `audit_logs` table migration; add provider audit columns migration |
| `backend/app/routers/proxy.py` | Call audit service after request completion (non-blocking) |
| `backend/app/routers/providers.py` | Add PATCH endpoint for provider update |
| `frontend/src/App.tsx` | Add `/audit` route |
| `frontend/src/components/layout/AppLayout.tsx` | Add Audit nav link |
| `frontend/src/lib/api.ts` | Add `audit` namespace and `providers.update` |
| `frontend/src/lib/types.ts` | Add audit types; update `Provider` interface |
| `frontend/src/pages/SettingsPage.tsx` | Add Audit Configuration section with per-provider toggles |
| `docs/API_SPEC.md` | Document audit endpoints and provider update |
| `docs/DATA_MODEL.md` | Document `audit_logs` table and provider audit fields |
| `docs/ARCHITECTURE.md` | Document audit recording flow |
| `docs/PRD.md` | Add Request Audit feature section |

---

## 8. Edge Cases

- **Audit disabled mid-request**: If `audit_enabled` is toggled off while a request is in-flight, the request still gets audited (the flag was checked at request start). This is acceptable.
- **Very large request bodies**: Truncated to 64KB. A truncation marker `[TRUNCATED]` is appended.
- **Binary/non-UTF-8 bodies**: Decoded with `errors="replace"`. Replacement characters indicate binary content.
- **Streaming response bodies**: Always `NULL`. The response headers and status are still recorded.
- **Audit INSERT failure**: Caught and logged to console. Never affects the proxy response. Never retried.
- **Concurrent requests**: Each audit INSERT is independent. No locking needed.
- **Failover requests**: Each upstream attempt (including failed failover attempts) gets its own audit row.
- **Request log deletion**: `audit_logs.request_log_id` is set to `NULL` (`ON DELETE SET NULL`), audit rows remain intact.
- **Provider deleted**: `audit_logs` reference `provider_id` with FK. If provider deletion is ever supported, cascade or SET NULL would apply. Currently providers are seed data and not deletable.
- **Config import**: Provider audit settings are preserved via export/import (`audit_enabled`, `audit_capture_bodies`).

---

## 9. Migration

Since the project uses manual migrations (`_add_missing_columns()` in `main.py`):

1. Add `audit_enabled BOOLEAN NOT NULL DEFAULT 0` to `providers` table
2. Add `audit_capture_bodies BOOLEAN NOT NULL DEFAULT 1` to `providers` table
3. Create `audit_logs` table with all columns and indexes (`request_log_id` FK uses `ON DELETE SET NULL`)

All operations are idempotent (check if column/table exists before creating).
