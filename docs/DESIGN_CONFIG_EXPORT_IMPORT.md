# Design: Configuration Export/Import

## Goal

Allow users to back up and restore the entire gateway configuration (providers, models, endpoints) as a single JSON file via API endpoints and a frontend UI.

No backward compatibility — import is a full replacement of all existing configuration.

---

## 1. Export JSON Schema

Single file, version-stamped, containing all three entity types with their relationships resolved by reference (not by database ID).

```json
{
  "version": 1,
  "exported_at": "2026-02-20T00:00:00Z",
  "providers": [
    {
      "name": "OpenAI",
      "provider_type": "openai",
      "description": "OpenAI API (GPT models)"
    }
  ],
  "models": [
    {
      "provider_type": "openai",
      "model_id": "gpt-4o",
      "display_name": "GPT-4o",
      "model_type": "native",
      "redirect_to": null,
      "lb_strategy": "round_robin",
      "is_enabled": true,
      "endpoints": [
        {
          "base_url": "https://api.openai.com",
          "api_key": "sk-abc123...",
          "is_active": true,
          "priority": 0,
          "description": "Primary key"
        }
      ]
    },
    {
      "provider_type": "openai",
      "model_id": "gpt-4o-alias",
      "display_name": "GPT-4o (alias)",
      "model_type": "proxy",
      "redirect_to": "gpt-4o",
      "lb_strategy": "single",
      "is_enabled": true,
      "endpoints": []
    }
  ]
}
```

### Design Decisions

- **No database IDs**: Relationships use natural keys (`provider_type` links models to providers, `redirect_to` uses `model_id` string). This makes the file portable across instances.
- **Endpoints nested under models**: Mirrors the ownership hierarchy. No orphan endpoints.
- **API keys in plaintext**: Consistent with existing project convention (single-user local deployment, no auth).
- **`version: 1`**: For future schema evolution if needed. Current implementation only accepts `version: 1`.
- **Excluded from export**: `request_logs` (telemetry, not config), all `id` fields, all timestamps (`created_at`, `updated_at`), health check state (`health_status`, `health_detail`, `last_health_check`).

---

## 2. Backend API

New router: `backend/app/routers/config.py`, prefix `/api/config`.

### 2.1 Export Configuration

```
GET /api/config/export
```

Response `200` (`application/json`):
```json
{
  "version": 1,
  "exported_at": "2026-02-20T00:00:00Z",
  "providers": [ ... ],
  "models": [ ... ]
}
```

Response header: `Content-Disposition: attachment; filename="gateway-config-{date}.json"`

Implementation:
1. Query all providers
2. Query all model configs with eager-loaded endpoints
3. Serialize to export schema (strip IDs, timestamps, health state)
4. Return JSON with download header

### 2.2 Import Configuration

```
POST /api/config/import
Content-Type: application/json
```

Request body: Same schema as export response.

Response `200`:
```json
{
  "providers_imported": 3,
  "models_imported": 12,
  "endpoints_imported": 8
}
```

Error `400`:
```json
{
  "detail": "Validation error: model 'gpt-4o-alias' references unknown redirect target 'gpt-4o-nonexistent'"
}
```

Error `422`: Standard Pydantic validation error (malformed JSON, missing required fields).

Implementation (single transaction):
1. Validate JSON against schema
2. Validate referential integrity:
   - Every model's `provider_type` must match a provider in the file
   - Every proxy model's `redirect_to` must reference a native model's `model_id` within the same provider
   - No duplicate `model_id` values
   - Proxy models must have empty `endpoints` array
3. Delete all existing endpoints, model configs, providers (in FK order)
4. Insert providers from file
5. Insert native models first, then proxy models (to satisfy `redirect_to` references)
6. Insert endpoints for each model
7. Commit transaction
8. Return counts

If any step fails, the transaction rolls back — existing config is preserved.

### 2.3 Pydantic Schemas

Add to `backend/app/schemas/schemas.py`:

```python
class ConfigEndpointExport(BaseModel):
    base_url: str
    api_key: str
    is_active: bool = True
    priority: int = 0
    description: str | None = None

class ConfigModelExport(BaseModel):
    provider_type: str
    model_id: str
    display_name: str | None = None
    model_type: str = "native"
    redirect_to: str | None = None
    lb_strategy: str = "single"
    is_enabled: bool = True
    endpoints: list[ConfigEndpointExport] = []

class ConfigProviderExport(BaseModel):
    name: str
    provider_type: str
    description: str | None = None

class ConfigExportResponse(BaseModel):
    version: int = 1
    exported_at: datetime
    providers: list[ConfigProviderExport]
    models: list[ConfigModelExport]

class ConfigImportRequest(BaseModel):
    version: int
    exported_at: datetime | None = None
    providers: list[ConfigProviderExport]
    models: list[ConfigModelExport]

class ConfigImportResponse(BaseModel):
    providers_imported: int
    models_imported: int
    endpoints_imported: int
```

### 2.4 Router Registration

In `backend/app/main.py`, add:
```python
from app.routers import config
app.include_router(config.router)
```

---

## 3. Frontend

### 3.1 UI Placement

Add a "Settings" page at `/settings` with export/import controls. This is the first settings page — keep it focused.

Files to create/modify:
- `frontend/src/pages/SettingsPage.tsx` — new page
- `frontend/src/App.tsx` — add route
- `frontend/src/components/layout/AppLayout.tsx` — add nav link
- `frontend/src/lib/api.ts` — add API functions
- `frontend/src/lib/types.ts` — add types

### 3.2 Settings Page Layout

```
Settings
─────────────────────────────────────────

Configuration Backup
Manage your gateway configuration. Export all providers,
models, and endpoints as a single JSON file, or import
a previously exported backup.

┌─────────────────────────────────────────────────────┐
│  Export                                              │
│                                                      │
│  Download a JSON file containing all providers,      │
│  models, and endpoint configurations.                │
│                                                      │
│  ⚠ API keys are included in plaintext.               │
│                                                      │
│                              [ Export Configuration ] │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Import                                              │
│                                                      │
│  Upload a JSON backup file to replace all current    │
│  configuration. This will DELETE all existing         │
│  providers, models, and endpoints.                   │
│                                                      │
│  ⚠ This action is destructive and cannot be undone.  │
│                                                      │
│  [ Choose File ]  gateway-config-2026-02-20.json     │
│                                                      │
│                    [ Import Configuration ]           │
└─────────────────────────────────────────────────────┘
```

### 3.3 UX Flow

**Export:**
1. User clicks "Export Configuration"
2. Frontend calls `GET /api/config/export`
3. Response JSON is saved as a file download (`gateway-config-YYYY-MM-DD.json`)
4. Toast: "Configuration exported successfully"

**Import:**
1. User selects a JSON file via file input
2. File is read client-side, parsed as JSON
3. If JSON parse fails → toast error, stop
4. User clicks "Import Configuration"
5. Confirmation dialog: "This will replace ALL existing configuration (N providers, N models). This cannot be undone. Continue?"
   - Show counts parsed from the uploaded file
6. On confirm → `POST /api/config/import` with parsed JSON body
7. On success → toast: "Imported N providers, N models, N endpoints"
8. On error → toast with error detail from backend
9. No page navigation — user stays on Settings

### 3.4 Nav Link

Add to sidebar in `AppLayout.tsx`, below Statistics:
- Icon: `Settings` from lucide-react
- Label: "Settings"
- Route: `/settings`

### 3.5 API Client Additions

In `frontend/src/lib/api.ts`:

```typescript
config: {
  export: () => request<ConfigExportResponse>("/api/config/export"),
  import: (data: ConfigImportRequest) =>
    request<ConfigImportResponse>("/api/config/import", {
      method: "POST",
      body: JSON.stringify(data),
    }),
},
```

### 3.6 Type Additions

In `frontend/src/lib/types.ts`:

```typescript
export interface ConfigEndpointExport {
  base_url: string;
  api_key: string;
  is_active: boolean;
  priority: number;
  description: string | null;
}

export interface ConfigModelExport {
  provider_type: string;
  model_id: string;
  display_name: string | null;
  model_type: string;
  redirect_to: string | null;
  lb_strategy: string;
  is_enabled: boolean;
  endpoints: ConfigEndpointExport[];
}

export interface ConfigProviderExport {
  name: string;
  provider_type: string;
  description: string | null;
}

export interface ConfigExportResponse {
  version: number;
  exported_at: string;
  providers: ConfigProviderExport[];
  models: ConfigModelExport[];
}

export interface ConfigImportRequest {
  version: number;
  exported_at?: string;
  providers: ConfigProviderExport[];
  models: ConfigModelExport[];
}

export interface ConfigImportResponse {
  providers_imported: number;
  models_imported: number;
  endpoints_imported: number;
}
```

---

## 4. Validation Rules (Import)

| Rule | Error |
|------|-------|
| `version` must be `1` | `"Unsupported config version: {N}. Expected: 1"` |
| `providers` array must not be empty | `"At least one provider is required"` |
| Each provider `provider_type` must be one of: `openai`, `anthropic`, `gemini` | `"Unknown provider type: '{type}'"` |
| No duplicate `provider_type` in providers array | `"Duplicate provider type: '{type}'"` |
| No duplicate `model_id` across all models | `"Duplicate model_id: '{id}'"` |
| Each model's `provider_type` must match a provider in the file | `"Model '{id}' references unknown provider type '{type}'"` |
| Proxy model `redirect_to` must reference a native model's `model_id` | `"Model '{id}' references unknown redirect target '{target}'"` |
| Proxy model `redirect_to` target must have the same `provider_type` | `"Model '{id}' cannot redirect cross-provider to '{target}'"` |
| Proxy models must have empty `endpoints` array | `"Proxy model '{id}' must not have endpoints"` |
| Native model `redirect_to` must be null | `"Native model '{id}' must not have redirect_to"` |

All validation happens before any database writes. First error encountered returns immediately (no partial import).

---

## 5. File Inventory

### New Files
| File | Description |
|------|-------------|
| `backend/app/routers/config.py` | Export/import API endpoints |
| `frontend/src/pages/SettingsPage.tsx` | Settings page with export/import UI |

### Modified Files
| File | Change |
|------|--------|
| `backend/app/schemas/schemas.py` | Add 6 config export/import schemas |
| `backend/app/main.py` | Register config router |
| `frontend/src/App.tsx` | Add `/settings` route |
| `frontend/src/components/layout/AppLayout.tsx` | Add Settings nav link |
| `frontend/src/lib/api.ts` | Add `config.export()` and `config.import()` |
| `frontend/src/lib/types.ts` | Add 6 config export/import interfaces |
| `docs/API_SPEC.md` | Document new endpoints in Section 1.4 |

---

## 6. Edge Cases

- **Empty database**: Export returns `{"version": 1, "providers": [], "models": []}`. Import of empty config clears everything.
- **Import over existing data**: All existing config is deleted first within the same transaction. If import fails validation, nothing is deleted.
- **Large files**: No explicit size limit. FastAPI default body limit applies. Practical limit: thousands of models would still be < 1MB JSON.
- **Concurrent imports**: No locking. Last write wins. Acceptable for single-user local deployment.
- **`auth_type` field on endpoints**: The `Endpoint` ORM model has an `auth_type` field not exposed in the current frontend types. Include it in export/import schema for completeness — add `auth_type: str | None` to `ConfigEndpointExport`.
