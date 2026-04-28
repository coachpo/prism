# API Specification: Prism

Service default base URL: `http://localhost:8000`

Local `./start.sh` base URL: `http://localhost:18000`

## 0. Profile Context Semantics
- Prism has three route classes:
  - Global management routes, which omit `X-Profile-Id`.
  - Profile-scoped management routes, which require `X-Profile-Id` and resolve against the selected profile.
  - Runtime proxy routes, which always use the active profile and ignore management scope overrides.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always use the active profile and ignore management scope overrides.
- Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/auth/*`, `/api/realtime/*`, `/api/settings/auth*`, `/api/config/vendors/*`, and `POST /api/config/profile/import/preview`.
- Profile-scoped management routes include `/api/config/profile/import`, `/api/settings/costing`, `/api/settings/timezone`, `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, `/api/models/*`, `/api/endpoints/*`, `/api/connections/*`, and the other non-global `/api/config/profile/*` routes.
- Detail endpoints return `404` when a resource exists in another profile but not in the effective profile context.
- Scope-control failures return structured JSON with `code` and `detail`, where `code` is stable for machine handling and `detail` is safe to show to operators.


## 1. Management API (`/api/*`)

### 1.0 Profiles
#### List Profiles
```
GET /api/profiles
```
Response `200`: Array of profile objects.
```json
[
  {
    "id": 1,
    "name": "Default",
    "description": "System default profile",
    "is_active": true,
    "is_default": true,
    "is_editable": true,
    "version": 5,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Bootstrap Profiles for the Shell
```
GET /api/profiles/bootstrap
```
Response `200`:
```json
{
  "profiles": [
    {
      "id": 1,
      "name": "Default",
      "description": "System default profile",
      "is_active": true,
      "is_default": true,
      "is_editable": true,
      "version": 5,
      "created_at": "2025-01-01T00:00:00Z",
      "updated_at": "2025-01-01T00:00:00Z"
    }
  ],
  "active_profile": {
    "id": 1,
    "name": "Default",
    "description": "System default profile",
    "is_active": true,
    "is_default": true,
    "is_editable": true,
    "version": 5,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  },
  "profile_limits": {
    "max_profiles": 10
  }
}
```

`active_profile` may be `null` when no runtime profile is currently active. The frontend shell uses this bootstrap endpoint as its single source for profile list, active-profile runtime metadata, and the max-profile creation cap.

#### Get Active Profile
```
GET /api/profiles/active
```
Response `200`: Active profile object (same schema as list item).

#### Create Profile
```
POST /api/profiles
```
Request:
```json
{
  "name": "Profile A",
  "description": "OpenAI workspace"
}
```
Response `201`: Created profile object.
Returns `409` if 10 non-deleted profiles already exist.
New profiles are always created with `is_default=false` and `is_editable=true`.

#### Update Profile
```
PATCH /api/profiles/{id}
```
Request fields: `name` (optional), `description` (optional).
Response `200`: Updated profile object.
Returns `400` if attempting to update a non-editable profile.

#### Activate Profile (CAS)
```
POST /api/profiles/{id}/activate
```
Request:
```json
{
  "expected_active_profile_id": 1
}
```
Response `200`: Updated active profile object.
Returns `409` when the expected active profile ID no longer matches the current active profile.

#### Delete Profile
```
DELETE /api/profiles/{id}
```
Response `200`: `{ "deleted": true }` for deletable profiles.
Returns `400` if the target profile is currently active or is the default profile.

---

### 1.1 Vendors

#### List Vendors
```
GET /api/vendors
```
Response `200`:
```json
[
  {
    "id": 1,
    "name": "OpenAI",
    "key": "openai",
    "description": "OpenAI API (GPT models)",
    "icon_key": "openai",
    "is_readonly": true,
    "audit_enabled": false,
    "audit_capture_bodies": true,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Create Vendor
```
POST /api/vendors
```
Request:
```json
{
  "key": "openrouter",
  "name": "OpenRouter",
  "description": "Shared vendor metadata for OpenRouter-backed models",
  "icon_key": "openrouter"
}
```
Response `201`: Created vendor object.

Returns `403` when `key` matches a canonical readonly system vendor such as `openai`, `anthropic`, or `gemini`.

#### Get Vendor
```
GET /api/vendors/{id}
```
Response `200`: Single vendor object (same schema as list item).

#### Get Vendor Usage
```
GET /api/vendors/{id}/models
```
Response `200`:
```json
[
  {
    "model_config_id": 12,
    "profile_id": 3,
    "profile_name": "Default",
    "model_id": "gpt-4o",
    "display_name": "GPT-4o",
    "model_type": "native",
    "api_family": "openai",
    "is_enabled": true
  }
]
```

Returns the profile-scoped model rows currently referencing the vendor. The Settings → Global delete flow uses this endpoint as informational context before operators confirm removal; deleting the vendor clears those models' `vendor_id`/`vendor` metadata instead of blocking the delete.

#### Update Vendor
```
PATCH /api/vendors/{id}
```
Request:
```json
{
  "audit_enabled": true,
  "audit_capture_bodies": false
}
```
Response `200`: Updated vendor object.

Mutable vendor fields:
- `key` (stable vendor key; normalized to lowercase)
- `name` (display name)
- `description` (optional shared description)
- `icon_key` (optional presentation-only vendor icon key; normalized to lowercase, nullable)
- `audit_enabled` (enable or disable audit for this vendor)
- `audit_capture_bodies` (when false, request/response bodies are stored as `null` for this vendor)

Readonly vendor behavior:
- API responses include derived `is_readonly` for canonical system vendors.
- Readonly vendors may update audit toggles, but identity fields (`key`, `name`, `description`, `icon_key`) are rejected with `403`.

#### Delete Vendor
```
DELETE /api/vendors/{id}
```
Response `204`: Vendor deleted.
Readonly system vendors return `403` and cannot be deleted from `/api/vendors/*`.
If an editable vendor is still referenced by live model rows, the delete still returns `204`. The backend nulls those models' `vendor_id` and returns `vendor: null` on later model reads. Runtime compatibility continues to come from each model's required `api_family`.

Vendor name, key, and description are part of the global vendor catalog.

Vendor records are global/shared and are not profile-scoped. The frontend manages them from Settings → Global, while profile-scoped audit toggles continue to consume the shared catalog from the Profile tab.
`icon_key` is optional, persisted, and presentation-only. It never affects runtime routing or `api_family` behavior. Vendor icon presets are locally vendored from the pinned `cc-switch` source, and the frontend falls back to a monogram or generic placeholder when the stored `icon_key` is unknown or missing. Source-backed asset IDs are persisted directly, so Z.ai uses `icon_key="zhipu"` and Microsoft/Azure uses `icon_key="azure"`.
The seeded OpenAI, Anthropic, and Gemini catalog rows currently surface as readonly system vendors in live API responses.

---

### 1.2 Model Configurations

#### List Models
```
GET /api/models
```
Response `200`: Array of model objects.

#### Get Model
```
GET /api/models/{id}
```
Response `200`: Full model object with nullable vendor metadata, required `api_family`, and ordered connections in the effective profile scope. Model rows do not carry `icon_key`; that metadata stays on `vendors[]`.

#### Get Models by Endpoints (Batch)
```
POST /api/models/by-endpoints
```
Request:
```json
{
  "endpoint_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains an `endpoint_id` and the models currently attached to it.

#### Get Models by Endpoint
```
GET /api/models/by-endpoint/{endpoint_id}
```
Response `200`: Array of models currently attached to the endpoint within the effective profile scope.

#### Create Model
```
POST /api/models
```
Request (native model):
```json
{
  "vendor_id": null,
  "api_family": "openai",
  "model_id": "gpt-4o",
  "display_name": "GPT-4o",
  "model_type": "native",
  "loadbalance_strategy_id": 7,
  "is_enabled": true
}
```
Request (proxy model):
```json
{
  "vendor_id": 2,
  "api_family": "anthropic",
  "model_id": "claude-sonnet-4-5",
  "display_name": "Claude Sonnet 4.5",
  "model_type": "proxy",
  "proxy_targets": [
    {
      "target_model_id": "claude-sonnet-4-5-20250929",
      "position": 0
    },
    {
      "target_model_id": "claude-sonnet-4-5-20250701",
      "position": 1
    }
  ],
  "is_enabled": true
}
```
Response `201`: Created model object.

Validation rules:
- `model_id` must be unique within the effective profile scope
- `api_family` is required on every model contract and remains the authoritative runtime compatibility field.
- `vendor_id` is optional metadata and may be `null`.
- If `model_type = "proxy"`: `proxy_targets` is required and must be a non-empty ordered list. Every entry must reference a unique native target model from the same profile and same `api_family`, and `position` values must stay contiguous starting at `0`. `loadbalance_strategy_id` must be null/omitted.
- If `model_type = "native"`: `proxy_targets` must be empty/omitted and `loadbalance_strategy_id` is required.
- Proxy target self-reference is rejected.
- Deleting a native model referenced by any proxy target returns `400` until the proxy targets are removed or updated.

#### Update Model
```
PUT /api/models/{id}
```
Request (all fields optional):
```json
{
  "vendor_id": null,
  "api_family": "anthropic",
  "model_id": "gpt-4o-updated",
  "display_name": "GPT-4o (Updated)",
  "model_type": "native",
  "loadbalance_strategy_id": 9,
  "is_enabled": true
}
```
Proxy-model updates use the same strict `proxy_targets` authoring contract as create: a non-empty ordered list of unique same-profile native targets from the same `api_family`, contiguous `position` values starting at `0`, no self-targets, and no `loadbalance_strategy_id`. Native-model updates continue to omit or empty `proxy_targets` and provide `loadbalance_strategy_id`. Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile. Returns `400` if proxy-target validation fails.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `200`: `{ "deleted": true }`. Deleting a model removes its scoped connections. Returns `400` if other proxy models still target a native model through `proxy_targets`.

---

### 1.3 Endpoints (Profile-Scoped Credentials)

#### List Endpoints
```
GET /api/endpoints
```
Response `200`: Array of endpoint objects in the effective profile scope, ordered by `position ASC, id ASC`.

#### List All Connections (Dropdown)
```
GET /api/endpoints/connections
```
Response `200`: `{ "items": [...] }` containing connection summary rows for dropdown consumers.

#### Create Endpoint

```
POST /api/endpoints
```
Request:
```json
{
  "name": "Primary OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-abc123..."
}
```
Response `201`: Created endpoint object.

#### Duplicate Endpoint
```
POST /api/endpoints/{id}/duplicate
```
Response `201`: Created endpoint copy with a generated duplicate-safe name.

#### Update Endpoint
```
PUT /api/endpoints/{id}
```
Request:
```json
{
  "name": "Updated OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-new-key..."
}
```
Response `200`: Updated endpoint object.

#### Move Endpoint Position
```
PATCH /api/endpoints/{id}/position
```
Request:
```json
{
  "to_index": 0
}
```
Response `200`: Ordered array of endpoint objects after the move.

Behavior:
- `to_index` must be in the range `0..(endpoint_count - 1)` or the API returns `422`.
- A no-op move returns the current ordered list unchanged.
- The backend rewrites endpoint positions to contiguous `0..N-1` values after every successful move.

#### Delete Endpoint
```
DELETE /api/endpoints/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` if any connections still reference this endpoint.
After a successful delete, later endpoints in the same profile are compacted so `position` remains contiguous.

### 1.4 Connections (Model-Scoped Routing)

#### List Connections for Model
```
GET /api/models/{model_config_id}/connections
```
Response `200`: Array of connection objects ordered by `priority ASC, id ASC`.

#### List Connections for Models (Batch)
```
POST /api/models/connections/batch
```
Request:
```json
{
  "model_config_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains a `model_config_id` and its ordered connections.

#### Create Connection
```
POST /api/models/{model_config_id}/connections
```
Request (using existing endpoint):
```json
{
  "endpoint_id": 1,
  "is_active": true,
  "name": "Primary production key",
  "custom_headers": {
    "X-Custom-Org": "org-123"
  },
    "openai_probe_endpoint_variant": "responses_minimal",
  "pricing_template_id": 2,
  "qps_limit": 3,
  "max_in_flight_non_stream": 6,
  "max_in_flight_stream": 2
}
```
Request (inline endpoint creation):
```json
{
  "endpoint_create": {
    "name": "New Endpoint",
    "base_url": "https://api.openai.com",
    "api_key": "sk-abc123..."
  },
  "is_active": true,
  "name": "Regional fallback",
    "openai_probe_endpoint_variant": "responses_minimal",
  "pricing_template_id": null,
  "qps_limit": null,
  "max_in_flight_non_stream": null,
  "max_in_flight_stream": null
}
```
Response `201`: Created connection object.
New connections always append at the end of the ordered list (`priority == current_connection_count`).
Create payloads that include `priority` are rejected with `422`.
Limiter fields are optional. `null` means unlimited. Positive integers apply per-connection request admission limits.
`openai_probe_endpoint_variant` selects the lightweight OpenAI probe target plus payload variant for OpenAI-family connections. Supported values are `responses_minimal` (default), `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`. For non-OpenAI families, providing this field is rejected and omitted values persist as `null`.

#### Update Connection
```
PUT /api/connections/{id}
```
Request: Mutable connection metadata only: `endpoint_id`, `endpoint_create`, `is_active`, `name`, `auth_type`, `custom_headers`, `openai_probe_endpoint_variant`, `pricing_template_id`, `qps_limit`, `max_in_flight_non_stream`, `max_in_flight_stream`.
`endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`.
`priority` is not accepted on update and any payload that includes it is rejected with `422`.
Response `200`: Updated connection object.

#### Move Connection Priority
```
PATCH /api/models/{model_config_id}/connections/{connection_id}/priority
```
Request:
```json
{
  "to_index": 0
}
```
Response `200`: Ordered array of connection objects after the move.

Behavior:
- `to_index` must be in the range `0..(connection_count - 1)` or the API returns `422`.
- A no-op move returns the current ordered list unchanged.
- The backend rewrites connection priorities to contiguous `0..N-1` values after every successful move.

#### Update Connection Pricing Template
```
PUT /api/connections/{connection_id}/pricing-template
```
Request:
```json
{
  "pricing_template_id": 2
}
```
Set to `null` to detach the template from the connection.

Response `200`: Updated connection object.

#### Delete Connection
```
DELETE /api/connections/{connection_id}
```
Response `200`: `{ "deleted": true }`.
After a successful delete, later connections for the same `(profile_id, model_config_id)` are compacted so `priority` remains contiguous.

#### Get Connection Owner
```
GET /api/connections/{connection_id}/owner
```
Response `200`:
```json
{
  "connection_id": 1,
  "model_config_id": 2,
  "model_id": "gpt-4o",
  "connection_name": "Primary production key",
  "endpoint_id": 12,
  "endpoint_name": "Primary OpenAI",
  "endpoint_base_url": "https://api.openai.com"
}
```

#### Health Check Connection
```
POST /api/connections/{connection_id}/health-check
```
Sends an api-family-specific lightweight request using the configured model ID to validate URL routing, authentication, and model availability end to end. This manual check uses the same probe builder and runner as the model-detail health-check preview flow.

Response `200`:
```json
{
  "connection_id": 1,
  "health_status": "healthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection successful",
  "response_time_ms": 523
}
```
API-family-specific health-check probes:
- OpenAI: `POST {base_url}/v1/responses` or `POST {base_url}/v1/chat/completions` based on the persisted `openai_probe_endpoint_variant`; the specific variant also controls whether the probe uses the minimal payload shape or the `reasoning: none` payload shape.
- Anthropic: `POST {base_url}/v1/messages` with a one-token user prompt.
- Gemini: `POST {base_url}/v1beta/models/{model}:generateContent` with minimal content payload.

#### Health Check Preview
```
POST /api/models/{model_config_id}/connections/health-check-preview
```
Request: Same inline connection payload accepted by `POST /api/models/{model_config_id}/connections`.

Response `200`:
```json
{
  "health_status": "healthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection successful",
  "response_time_ms": 523
}
```

The preview route runs the same lightweight probe as the persisted connection health check without creating or updating a connection row.

#### Base URL Validation

On endpoint create (`POST`) and update (`PUT`), the `base_url` is:
1. **Normalized**: Trailing slashes are stripped (e.g., `https://api.example.com/` → `https://api.example.com`)
2. **Validated**: Rejected with HTTP 422 if scheme/host is missing.
3. **Version path is not allowed**: Rejected with HTTP 422 if `base_url` includes upstream API version segments such as `/v1` or `/v1beta`.

Use host-root base URLs only:
- ✅ `https://api.openai.com`
- ✅ `https://generativelanguage.googleapis.com`
- ❌ `https://api.openai.com/v1`
- ❌ `https://generativelanguage.googleapis.com/v1beta`

### 1.5 Pricing Templates

#### List Pricing Templates
```
GET /api/pricing-templates
```
Response `200`: Array of pricing template list items in the effective profile scope.

#### Create Pricing Template
```
POST /api/pricing-templates
```
Request:
```json
{
  "name": "GPT-4o Standard",
  "description": "Default OpenAI pricing",
  "pricing_currency_code": "USD",
  "input_price": "5.00",
  "output_price": "15.00",
  "cached_input_price": "2.50",
  "cache_creation_price": null,
  "reasoning_price": "15.00"
}
```
Response `201`: Created pricing template object.

#### Update Pricing Template
```
PUT /api/pricing-templates/{id}
```
Request: Any mutable pricing template fields.
Response `200`: Updated pricing template object.

#### Delete Pricing Template
```
DELETE /api/pricing-templates/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the template is still referenced by connections; response `detail` includes a `connections` array with dependency details.

#### List Connections Using Template
```
GET /api/pricing-templates/{id}/connections
```
Response `200`: Usage payload with `template_id` and `items[]` (`connection_id`, `connection_name`, `model_config_id`, `model_id`, `endpoint_id`, `endpoint_name`).
---

### 1.6 Config Export/Import

Prism now uses a breaking `version: 1` profile config bundle contract with two explicit ownership domains:

- **Profile bundle**: profile-scoped config only
- **Vendor catalog bundle**: global vendor metadata only

#### Export Profile Configuration
```
GET /api/config/profile/export
```
Response `200`:
```json
{
  "version": 1,
  "bundle_kind": "profile_config",
  "exported_at": "2026-04-04T15:00:00Z",
  "vendor_refs": [
    {
      "key": "openai",
      "name_hint": "OpenAI",
      "description_hint": "OpenAI API (GPT models)",
      "icon_key_hint": "openai"
    }
  ],
  "endpoints": [
    {
      "name": "Primary OpenAI",
      "base_url": "https://api.openai.com",
      "api_key_secret_ref": "endpoint:Primary OpenAI:api_key",
      "position": 0
    }
  ],
  "pricing_templates": [],
  "loadbalance_strategies": [],
  "models": [],
  "profile_settings": {
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "timezone_preference": "Europe/Helsinki",
    "endpoint_fx_mappings": []
  },
  "header_blocklist_rules": [],
  "secret_payload": {
    "kind": "encrypted",
    "cipher": "fernet-v1",
    "key_id": "sha256:...",
    "entries": [
      {
        "ref": "endpoint:Primary OpenAI:api_key",
        "ciphertext": "enc:..."
      }
    ]
  }
}
```
The response includes a `Content-Disposition` header to trigger a file download: `attachment; filename="prism-profile-config-v1-YYYY-MM-DD.json"`.

Profile export semantics:
- `bundle_kind` is always `profile_config`.
- `vendor_refs` are non-authoritative hints keyed by actual referenced `vendor_key` values only.
- Vendorless models export `vendor_key: null` and do not add entries to `vendor_refs`.
- Export never includes plaintext `endpoints[].api_key`.
- Endpoints without an API key export `api_key_secret_ref: null` and do not contribute an entry to `secret_payload.entries[]`.
- Endpoint secrets are exported only through `secret_payload.entries[]`.
- Export fails if a stored endpoint secret cannot be decrypted before bundle encryption.
- Profile bundles preserve ordered proxy targets, same-family model routing, and attached loadbalance references as part of the canonical contract.

#### Preview Profile Import
```
POST /api/config/profile/import/preview
```
Request: Full profile bundle using `version: 1` and `bundle_kind: "profile_config"`.

This preview route is global and does not require `X-Profile-Id`.

Response `200`:
```json
{
  "ready": true,
  "version": 1,
  "bundle_kind": "profile_config",
  "endpoints_imported": 2,
  "pricing_templates_imported": 4,
  "strategies_imported": 2,
  "models_imported": 5,
  "connections_imported": 10,
  "vendor_resolutions": [
    {
      "vendor_key": "openai",
      "resolution": "reuse",
      "warning": null
    }
  ],
  "secret_key_id": "sha256:...",
  "decryptable_secret_refs": [
    "endpoint:Primary OpenAI:api_key"
  ],
  "blocking_errors": [],
  "warnings": []
}
```

Preview semantics:
- Preview is the authoritative backend readiness check for profile import.
- The backend validates bundle kind/version, internal references, vendor resolution, and secret decryption before returning `ready: true`.
- Preview rejects plaintext or otherwise non-encrypted `secret_payload.entries[].ciphertext` values.
- When bundle key validation or secret decryption fails, preview returns `ready: false` with `blocking_errors[]` and does not mutate profile state.

#### Import Profile Configuration
```
POST /api/config/profile/import
```
Request: Full profile bundle using `version: 1` and `bundle_kind: "profile_config"`.

This import route is profile-scoped and requires `X-Profile-Id`.

Response `200`:
```json
{
  "endpoints_imported": 2,
  "pricing_templates_imported": 4,
  "strategies_imported": 2,
  "models_imported": 5,
  "connections_imported": 10
}
```

Profile import semantics:
- Import is profile-targeted and replaces configuration in the effective profile only.
- Other profiles are not deleted or mutated.
- `models[].vendor_key` is optional; when omitted or `null`, the imported model persists with `vendor_id = null` and `vendor = null`.
- When `models[].vendor_key` is present, the backend resolves or creates the matching shared vendor row by that key only.
- Global vendor rows are resolved by `vendor_key` only.
- If a referenced `vendor_key` already exists globally, import reuses that vendor row.
- If vendor hints differ from existing global metadata, import does not fail and does not mutate the existing global vendor row.
- If a profile bundle would create a new vendor whose proposed name collides with an existing global vendor name, preview/import fail before profile replacement starts.
- When endpoint `position` is present, import uses it as the ordering hint; when omitted, import falls back to endpoint file order. Persisted endpoint positions are normalized to contiguous `0..N-1` values.
- Exported model connections are ordered by `(priority, id)`. During import, each model's connection priorities are normalized to contiguous `0..N-1` values while preserving relative order by imported `priority` and payload order.
- Import decrypts bundle secrets before any destructive mutation begins, then re-encrypts them into Prism's normal at-rest secret storage.
- Endpoints with `api_key_secret_ref: null` import as no-auth endpoints with an empty stored endpoint secret.
- Wrong bundle key or unreadable secret payloads fail before profile replacement starts.
- Internal IDs (`endpoint_id`, `connection_id`, `pricing_template_id`) remain omitted from the profile bundle. The contract uses name-based references.
- Exported/imported proxy models carry ordered `proxy_targets` on the proxy model payload.
- Exported/imported connections include `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited.
- Exported/imported loadbalance strategies use a top-level `strategy_type` discriminator. Legacy strategies carry `legacy_strategy_type` plus `auto_recovery`; adaptive strategies carry `routing_policy`.
- Other config version numbers are unsupported.

#### Export Vendor Catalog
```
GET /api/config/vendors/export
```
Vendor catalog routes are global and do not require `X-Profile-Id`.

Response `200`:
```json
{
  "version": 1,
  "bundle_kind": "vendor_catalog",
  "exported_at": "2026-04-04T15:00:00Z",
  "vendors": [
    {
      "key": "openai",
      "name": "OpenAI",
      "description": "OpenAI API (GPT models)",
      "icon_key": "openai",
      "audit_enabled": false,
      "audit_capture_bodies": true
    }
  ]
}
```

#### Preview Vendor Catalog Import
```
POST /api/config/vendors/import/preview
```
Response `200`:
```json
{
  "ready": true,
  "version": 1,
  "bundle_kind": "vendor_catalog",
  "create_count": 1,
  "update_count": 1,
  "blocking_errors": [],
  "warnings": []
}
```

#### Import Vendor Catalog
```
POST /api/config/vendors/import
```
Response `200`:
```json
{
  "created_count": 1,
  "updated_count": 1
}
```

Vendor catalog semantics:
- Vendor catalog bundles are authoritative for global vendor metadata only.
- Vendor catalog import upserts vendor metadata by `key`.
- Vendor catalog preview/import reject duplicate bundle keys, duplicate bundle names, and global name collisions before mutation.
- Vendor catalog import is independent from the profile backup/import flow.
- Vendor catalog exports and imports stay on the same split-bundle contract version as profile bundles.

---

### 1.7 Settings API

#### Get Auth Settings
```
GET /api/settings/auth
```
Response `200`: Global operator-auth settings (`auth_enabled`, `username`, `email`, `pending_email`, `email_verification_required`, `has_password`, `proxy_key_limit`).

#### Update Auth Settings
```
PUT /api/settings/auth
```
Request fields:
- `auth_enabled`
- `username`
- `password`

Lifecycle contract:
- Disabling auth clears the current browser cookies in the response and invalidates stale management sessions immediately.
- Changing the operator username or password invalidates stale management sessions immediately, even when auth remains enabled.
- After invalidation, `GET /api/auth/session` returns `401`, while `GET /api/auth/status` continues to report the live global auth mode.

#### Request Email Verification
```
POST /api/settings/auth/email-verification/request
```

#### Confirm Email Verification
```
POST /api/settings/auth/email-verification/confirm
```

#### Proxy API Keys
- `GET /api/settings/auth/proxy-keys`
- `POST /api/settings/auth/proxy-keys`
- `PATCH /api/settings/auth/proxy-keys/{id}`
- `POST /api/settings/auth/proxy-keys/{id}/rotate`
- `DELETE /api/settings/auth/proxy-keys/{id}`

Proxy-key lifecycle contract:
- Create and update payloads accept optional `expires_at` in RFC3339 form; `null` clears expiry.
- List responses keep historical rows with `is_active`, `expires_at`, and `rotated_from_id` so the UI can render retired, expired, and rotated lineage without rewriting history.
- Rotate is lineage-creating, not in-place mutation: the historical row becomes inactive, a successor row is created with `rotated_from_id` pointing at the predecessor, and only the successor response includes the new one-time secret.

#### Get Costing Settings
```
GET /api/settings/costing
```
Response `200`:
```json
{
  "report_currency_code": "USD",
  "report_currency_symbol": "$",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "1.0"
    }
  ]
}
```

`/api/settings/auth*` routes are global management endpoints. `/api/settings/costing` and `/api/settings/timezone` are profile-scoped and require `X-Profile-Id`.

#### Update Costing Settings
```
PUT /api/settings/costing
```
Request:
```json
{
  "report_currency_code": "EUR",
  "report_currency_symbol": "€",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "0.92"
    }
  ]
}
```
Response `200`: Updated settings object.

#### Get Timezone Preference
```
GET /api/settings/timezone
```
Response `200`:
```json
{
  "profile_id": 2,
  "timezone_preference": "Europe/Helsinki"
}
```

#### Update Timezone Preference
```
PUT /api/settings/timezone
```
Request:
```json
{
  "timezone_preference": "America/New_York"
}
```
Response `200`: Updated timezone object.

There is no standalone `/api/settings/monitoring` route or `/api/monitoring/*` family in the current live OpenAPI contract. Current operator-facing observability and routing-health surfaces are provided through `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, and the manual connection health endpoints.

---

### 1.8 User-Agent Client Rules (System Global + User Profile-Scoped)

#### List User-Agent Client Rules
```
GET /api/config/user-agent-client-rules
```
Query parameters:
- `include_disabled` (boolean, default `true`): Whether to include disabled rules in the list.

Response `200`:
```json
[
  {
    "id": 1,
    "name": "Codex",
    "pattern": "codex",
    "enabled": true,
    "is_system": true,
    "profile_id": null,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

The list returns system rules plus any user rules in the effective profile.

#### Get User-Agent Client Rule
```
GET /api/config/user-agent-client-rules/{rule_id}
```
Response `200`: Single rule object.

#### Create User-Agent Client Rule
```
POST /api/config/user-agent-client-rules
```
Request:
```json
{
  "name": "My SDK",
  "pattern": "my-sdk",
  "enabled": true
}
```
Response `201`: Created rule object. `pattern` must be a valid regular expression.

#### Update User-Agent Client Rule
```
PATCH /api/config/user-agent-client-rules/{rule_id}
```
Request (all fields optional):
```json
{
  "enabled": false
}
```
Response `200`: Updated rule object.
Note: For system rules (`is_system: true`), only `enabled` is mutable. Attempting to change `name` or `pattern` returns `400`.

#### Delete User-Agent Client Rule
```
DELETE /api/config/user-agent-client-rules/{rule_id}
```
Response `200`: `{ "deleted": true }`.
Note: Delete is only available for effective-profile user rules. Attempting to delete a system rule through this route returns `404` because system rows are not in the profile-owned delete scope.

---

### 1.9 Header Blocklist Rules (System Global + User Profile-Scoped)

#### List Header Blocklist Rules
```
GET /api/config/header-blocklist-rules
```
Query parameters:
- `include_disabled` (boolean, default `true`): Whether to include disabled rules in the list.

Response `200`:
```json
[
  {
    "id": 1,
    "name": "Cloudflare Ray",
    "match_type": "exact",
    "pattern": "cf-ray",
    "enabled": true,
    "is_system": true,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Get Header Blocklist Rule
```
GET /api/config/header-blocklist-rules/{id}
```
Response `200`: Single rule object.

#### Create Header Blocklist Rule
```
POST /api/config/header-blocklist-rules
```
Request:
```json
{
  "name": "My Custom Header",
  "match_type": "prefix",
  "pattern": "x-custom-",
  "enabled": true
}
```
Response `201`: Created rule object. Returns `409` if a user rule with the same `match_type` and `pattern` already exists in the effective profile. Prefix patterns must end with `-`.

#### Update Header Blocklist Rule
```
PATCH /api/config/header-blocklist-rules/{id}
```
Request (all fields optional):
```json
{
  "name": "Updated Name",
  "enabled": false
}
```
Response `200`: Updated rule object.
Note: For system rules (`is_system: true`), only the `enabled` field can be modified. Attempting to change other fields returns `400`.

#### Delete Header Blocklist Rule
```
DELETE /api/config/header-blocklist-rules/{id}
```
Response `200`: `{ "deleted": true }`.
Note: Delete is only available for effective-profile user rules. Attempting to delete a system rule through this route returns `404` because system rows are not in the profile-owned delete scope.

---

## 2. Proxy API

Prism accepts api-family-native runtime paths only:

- OpenAI clients use base URL `<prism-host>/v1`
- Anthropic clients use base URL `<prism-host>`
- Gemini clients use base URL `<prism-host>`

### 2.1 OpenAI-Compatible Chat Completions
```
POST /v1/chat/completions
```
Request (standard OpenAI format):
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "stream": false
}
```
Response: Proxied directly from the upstream API family. Format matches that family's native response.

### 2.2 Anthropic-Compatible Messages
```
POST /v1/messages
```
Request (standard Anthropic format):
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```
Response: Proxied directly from upstream Anthropic API.

### 2.3 Gemini Native Generate Content
```
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent
```
Request (standard Gemini native format):
```json
{
  "contents": [
    {
      "role": "user",
      "parts": [{"text": "Hello!"}]
    }
  ]
}
```
Response: Proxied directly from the upstream Gemini API.

### 2.4 Streaming

Streaming stays api-family-native: OpenAI and Anthropic typically use body flags on `/v1/*` routes, while Gemini streaming uses `/v1beta/models/{model}:streamGenerateContent`. Streaming responses are proxied directly from upstream.
For Gemini, the path is authoritative: `/v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`.

### 2.5 Token Usage Extraction

The gateway extracts token usage from upstream responses and logs it to `request_logs`. Extraction is api-family-aware:

**Non-streaming responses:**
| API Family | Response Format | Extraction Path |
|---|---|---|
| OpenAI | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` | `usage.prompt_tokens`, `usage.completion_tokens`, `usage.total_tokens` |
| Anthropic Messages | `{"usage": {"input_tokens": N, "output_tokens": N}}` | `usage.input_tokens`, `usage.output_tokens`; `total_tokens` = sum |
| Anthropic count_tokens | `{"input_tokens": N}` | Top-level `input_tokens`; `output_tokens` and `total_tokens` = null |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from the final events:
| API Family | Usage Events | Extraction |
|---|---|---|
| OpenAI | Final chunk/event containing a `usage` object (when provided by upstream) | Same as non-streaming `usage` object |
| Anthropic | `message_start` event → `message.usage.input_tokens`; `message_delta` event → `usage.output_tokens` | Accumulated from both events; `total_tokens` = sum |

If token data cannot be extracted, all token fields are logged as `null`.

---

## 3. Health Check

```
GET /health
```
Response `200` returns liveness, readiness, and startup state together with the current backend release version:
```json
{
  "status": "ok",
  "version": "<current release version>",
  "liveness": "ok",
  "readiness": "ready",
  "startup": "complete"
}
```

The health contract is not version only. It is the operator-facing target for backend readiness, startup state, and live in-app health views.

---

## 4. Statistics API

Stats APIs are profile-scoped and require `X-Profile-Id`.

### 4.1 Usage Snapshot
```
GET /api/stats/usage-snapshot
```
This is the live dashboard analytics contract. It returns the unified usage snapshot used by `/dashboard?tab=analytics` after the request-events surface was removed, leaving aggregate summary, endpoint, model, and proxy-key statistics only.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Snapshot range preset. Supported values: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |

The snapshot is backed by `backend/internal/httpapi/management/stats/service.go` together with the aggregation types and query helpers in `backend/internal/domain/stats/snapshot.go` and `backend/internal/domain/stats/types.go`.

The snapshot is still aggregated from persisted usage-event rows, and dashboard analytics stays focused on aggregate views. Exact request investigation remains on `/request-logs`, while dashboard and other pages continue to use the shared stats routes below.

`GET /api/stats/requests/operations` is not part of the current management API.

### 4.1A Endpoint Model Statistics
```
GET /api/stats/endpoints/{endpoint_id}/models
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Time preset: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |
| `from_time` | datetime | — | Optional explicit start time |
| `to_time` | datetime | — | Optional explicit end time |

Response `200`: Array of per-model endpoint statistics. Each item includes `model_id`, `model_label`, request counts, success rates, TTFT percentiles, token totals, total cost, and average output rate for the selected endpoint scope.

### 4.2 List Request Logs
```
GET /api/stats/requests
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `ingress_request_id` | string | — | Exact incoming-request grouping ID shared by per-attempt rows |
| `model_id` | string | — | Filter by model ID |
| `status_family` | string | — | Filter by status family (`4xx` or `5xx`) |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `limit` | integer | 50 | Max results (1-500) |
| `offset` | integer | 0 | Pagination offset |

Response `200`:
```json
{
  "filter_options": {
    "endpoints": [
      {
        "endpoint_id": 12,
        "endpoint_label": "Primary OpenAI"
      }
    ],
    "models": [
      {
        "model_id": "gpt-4o",
        "model_label": "GPT-4o"
      }
    ]
  },
  "items": [
    {
      "id": 1,
      "model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "resolved_target_model_id": null,
      "resolved_target_model_label": null,
      "is_proxy_origin": false,
      "api_family": "openai",
      "vendor_id": 1,
      "vendor_key": "openai",
      "vendor_name": "OpenAI",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "connection_id": 1,
      "status_code": 200,
      "response_time_ms": 1234,
      "ttft_ms": 320,
      "completion_duration_ms": 914,
      "is_stream": false,
      "total_tokens": 57,
      "total_cost_user_currency_micros": 1250,
      "priced_flag": true,
      "unpriced_reason": null,
      "report_currency_symbol": "$",
      "caller_client_display": "Codex",
      "upstream_client_display": "OpenAI SDK",
      "user_agent_overridden": false,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

The list route is the slim browse contract used by `/request-logs` and other row-summary consumers. It keeps one row per upstream attempt, returns `filter_options.endpoints` for the endpoint dropdown and `filter_options.models` for the model dropdown, includes `model_label`, `resolved_target_model_label`, and `is_proxy_origin` for display, and does not treat vendor as a server filter. The current request-log page uses page sizes `100`, `300`, and `500`, with `100` as the frontend default. This is the operator drill-in surface for investigation, not a dashboard aggregate.

`filter_options` always includes both `endpoints` and `models`. `filter_options.models` is request-log native and contains `{ model_id, model_label }` entries; when no current model options exist, the backend still returns `models: []` instead of omitting the field. `ingress_request_id` groups multiple attempt rows that belong to one incoming runtime request. For proxy traffic, `model_id` stays the requested proxy model and `resolved_target_model_id` captures the selected native target model for that attempt, while `resolved_target_model_label` surfaces the matching display label.

Exact single-request investigation now lives on `GET /api/stats/requests/{request_id}` instead of the paginated list-query surface.

### 4.3 Get Request Log Detail
```
GET /api/stats/requests/{request_id}
```

Response `200`:
```json
{
  "summary": {
    "id": 1,
    "created_at": "2025-01-15T10:30:00Z",
    "model_id": "gpt-4o",
    "model_label": "GPT-4o",
    "resolved_target_model_id": null,
    "resolved_target_model_label": null,
    "is_proxy_origin": false,
    "api_family": "openai",
    "vendor_id": 1,
    "vendor_key": "openai",
    "vendor_name": "OpenAI",
    "status_code": 200,
    "response_time_ms": 1234,
    "ttft_ms": 320,
    "completion_duration_ms": 914,
    "is_stream": false
  },
  "request": {
    "request_path": "/v1/chat/completions",
    "ingress_request_id": "ingress_req_42",
    "attempt_number": 2,
    "provider_correlation_id": "req_upstream_abc123",
    "proxy_api_key_id": null,
    "proxy_api_key_name_snapshot": null,
    "caller_user_agent": "codex/1.0",
    "upstream_user_agent": "OpenAI/Python 1.0",
    "caller_client_display": "Codex",
    "upstream_client_display": "OpenAI SDK",
    "user_agent_overridden": false,
    "error_detail": null
  },
  "routing": {
    "profile_id": 2,
    "endpoint_label": "Primary OpenAI",
    "endpoint_id": 12,
    "connection_id": 1,
    "endpoint_base_url": "https://api.openai.com",
    "endpoint_description": "Primary production key",
    "audit_enabled_at_request": false
  },
  "usage": {
    "input_tokens": 15,
    "output_tokens": 42,
    "total_tokens": 57,
    "success_flag": true,
    "billable_flag": true,
    "priced_flag": true,
    "unpriced_reason": null,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0,
    "reasoning_tokens": 0
  },
  "costing": {
    "input_cost_micros": 500,
    "output_cost_micros": 750,
    "cache_read_input_cost_micros": 0,
    "cache_creation_input_cost_micros": 0,
    "reasoning_cost_micros": 0,
    "total_cost_original_micros": 1250,
    "total_cost_user_currency_micros": 1250,
    "currency_code_original": "USD",
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "fx_rate_used": "1",
    "fx_rate_source": "DEFAULT_1_TO_1"
  },
  "pricing": {
    "pricing_snapshot_unit": "PER_1M",
    "pricing_snapshot_input": "33.333333",
    "pricing_snapshot_output": "17.857143",
    "pricing_snapshot_cache_read_input": null,
    "pricing_snapshot_cache_creation_input": null,
    "pricing_snapshot_reasoning": null,
    "pricing_config_version_used": 1
  }
}
```

Response `404`: returned when the request ID is missing or out of scope for the effective profile.

The request-log sheet consumes this grouped detail contract. The frontend keeps audit loading separate and lazy: opening the `overview` tab uses only this response, while the `audit` tab resolves linked audit payloads on demand.

### 4.4 Get Aggregated Statistics
```
GET /api/stats/summary
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |
| `group_by` | string | — | Group results by: `model`, `api_family`, `endpoint` |
| `model_id` | string | — | Filter by model ID |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |

Response `200`:
```json
{
  "total_requests": 1500,
  "success_count": 1450,
  "error_count": 50,
  "success_rate": 96.67,
  "avg_response_time_ms": 850,
  "p95_response_time_ms": 2100,
  "total_input_tokens": 50000,
  "total_output_tokens": 120000,
  "total_tokens": 170000,
  "groups": [
    {
      "key": "gpt-4o",
      "total_requests": 800,
      "success_count": 790,
      "error_count": 10,
      "avg_response_time_ms": 750,
      "total_tokens": 90000
    }
  ]
}
```

### 4.4 Model Metrics (Batch)
```
POST /api/stats/models/metrics
```
Request:
```json
{
  "model_ids": ["gpt-4o", "claude-3-5-sonnet"],
  "summary_window_hours": 24,
  "spending_preset": "last_30_days"
}
```
Response `200`: `items[]`, where each item contains `model_id`, `success_rate`, `request_count_24h`, `p95_latency_ms`, and `spend_30d_micros`.

### 4.6 Get Connection Success Rates
```
GET /api/stats/connection-success-rates
```
Returns success rate data for all connections, computed from `request_logs`.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |

Response `200`:
```json
[
  {
    "connection_id": 1,
    "total_requests": 150,
    "success_count": 148,
    "error_count": 2,
    "success_rate": 98.67
  },
  {
    "connection_id": 2,
    "total_requests": 0,
    "success_count": 0,
    "error_count": 0,
    "success_rate": null
  }
]
```

Fields:
- `connection_id` (int): The connection ID
- `total_requests` (int): Total requests routed through this connection
- `success_count` (int): Requests with 2xx status codes
- `error_count` (int): Requests with non-2xx status codes
- `success_rate` (float | null): Success percentage (0-100), `null` if no requests

### 4.7 Get Throughput Report
```
GET /api/stats/throughput
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range |
| `to_time` | datetime | now | End of time range |
| `model_id` | string | — | Filter by model ID |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |

Response `200`: Throughput summary plus time buckets (`average_rpm`, `peak_rpm`, `current_rpm`, `total_requests`, `time_window_seconds`, `buckets[]`).

### 4.8 Delete Request Logs (Batch)
```
DELETE /api/stats/requests
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `older_than_days` | integer | — | Delete logs older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all request logs. |

Exactly one of `older_than_days` or `delete_all=true` must be provided. If both are provided, returns `400`. If neither is provided, returns `400`.

When using `older_than_days`, the cutoff timestamp is computed server-side from UTC app time as `current_utc - older_than_days`. Cleanup is scheduled immediately after the response and runs in the background with a fresh async DB session.

Response `200`:
```json
{
  "accepted": true
}
```

The response acknowledges that cleanup was scheduled; it does not include a final row count.

### 4.8A Delete Aggregated Statistics Data
```
DELETE /api/stats/statistics
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `older_than_days` | integer | — | Delete aggregated statistics data older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all aggregated statistics data. |

Exactly one of `older_than_days` or `delete_all=true` must be provided.

Response `200`:
```json
{
  "accepted": true
}
```

The response acknowledges that cleanup was scheduled; it does not include a final row count.

Response `400`:
```json
{
  "detail": "Provide either 'older_than_days' (integer >= 1) or 'delete_all=true'"
}
```

Deleting request logs does NOT cascade to `audit_logs`. Linked audit rows remain, and `audit_logs.request_log_id` is set to `null` (`ON DELETE SET NULL`).

### 4.9 Get Spending Reports
```
GET /api/stats/spending
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | — | Time preset: `today`, `24h`, `last_7_days`, `7d`, `last_30_days`, `30d`, `custom`, `all` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `model_id` | string | — | Filter by model ID |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |
| `group_by` | string | `none` | Group by: `none`, `day`, `week`, `month`, `api_family`, `model`, `endpoint`, `model_endpoint` |
| `limit` | integer | 50 | Max results (1-500) |
| `offset` | integer | 0 | Pagination offset |
| `top_n` | integer | 5 | Number of top spenders to return (1-50) |

Response `200`:
```json
{
  "summary": {
    "total_cost_micros": 1250000,
    "successful_request_count": 1500,
    "priced_request_count": 1450,
    "unpriced_request_count": 50,
    "total_input_tokens": 50000,
    "total_output_tokens": 120000,
    "total_cache_read_input_tokens": 10000,
    "total_cache_creation_input_tokens": 1500,
    "total_reasoning_tokens": 2000,
    "total_tokens": 182000,
    "avg_cost_per_successful_request_micros": 833
  },
  "groups": [
    {
      "key": "gpt-4o",
      "total_cost_micros": 850000,
      "total_requests": 800,
      "priced_requests": 790,
      "unpriced_requests": 10,
      "total_tokens": 90000
    }
  ],
  "groups_total": 12,
  "top_spending_models": [
    {
      "model_id": "gpt-4o",
      "total_cost_micros": 850000
    }
  ],
  "top_spending_endpoints": [
    {
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "total_cost_micros": 740000
    }
  ],
  "unpriced_breakdown": {
    "PRICING_DISABLED": 30,
    "UNKNOWN": 20
  },
  "report_currency_code": "USD",
  "report_currency_symbol": "$"
}
```

---

## 5. Audit API

Audit APIs are profile-scoped and require `X-Profile-Id`.

### 5.1 List Audit Logs
```
GET /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `request_log_id` | integer | — | Filter audit rows linked to one request log |
| `vendor_id` | integer | — | Filter by vendor ID |
| `model_id` | string | — | Filter by model ID |
| `status_code` | integer | — | Filter by response status code |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `limit` | integer | 50 | Max results (1-200) |
| `offset` | integer | 0 | Pagination offset |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 2,
      "request_log_id": 42,
       "vendor_id": 1,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "connection_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
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

The list API returns `request_body_preview` (first 200 characters of the request body) instead of the full body. Use the detail API for full content.
If vendor body capture is disabled, `request_body_preview` is `null`.
Rows are ordered by `created_at DESC`.

### 5.2 Get Audit Log Detail
```
GET /api/audit/logs/{id}
```
Response `200`:
```json
{
  "id": 1,
  "profile_id": 2,
  "request_log_id": 42,
   "vendor_id": 1,
  "model_id": "gpt-4o",
  "endpoint_id": 12,
  "connection_id": 1,
  "endpoint_base_url": "https://api.openai.com",
  "endpoint_description": "Primary production key",
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

For streaming requests, `response_body` is `null` (streaming response bodies are not recorded).
If vendor body capture is disabled, both `request_body` and `response_body` are `null`.

Response `404`: Audit log not found.

### 5.3 Delete Audit Logs (Batch)
```
DELETE /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `before` | datetime | — | Delete logs created before this time (ISO 8601). |
| `older_than_days` | integer | — | Delete logs older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all audit logs. |

Exactly one of `before`, `older_than_days`, or `delete_all=true` must be provided. If multiple are provided or none are provided, returns `400`.

When using `older_than_days`, the cutoff timestamp is computed server-side from UTC app time as `current_utc - older_than_days`. Cleanup is scheduled immediately after the response and runs in the background with a fresh async DB session.

Response `200`:
```json
{
  "accepted": true
}
```

The response acknowledges that cleanup was scheduled; it does not include a final row count.

Response `400`: Missing or conflicting parameters.

### 5.4 Redaction Rules

All audit log entries have sensitive header values redacted before storage:
- `authorization` → `Bearer [REDACTED]`
- `x-api-key` → `[REDACTED]`
- `x-goog-api-key` → `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, or `auth` (case-insensitive) → value replaced with `[REDACTED]`

Request and response bodies are not header-redacted and may contain user-provided secrets or PII.
Body capture is configurable per vendor via `audit_capture_bodies`; when disabled, both `request_body` and `response_body` are `null`.

### 5.5 Body Size Limits

When body capture is enabled for the vendor, request and response bodies are truncated to 64KB before storage. A `[TRUNCATED]` marker is appended when truncation occurs.

---

## 6. Loadbalance API

Loadbalance APIs are profile-scoped and require `X-Profile-Id`.

### 6.1 List Loadbalance Strategies
```
GET /api/loadbalance/strategies
```
Response `200`: Array of strategy objects in the effective profile scope.

### 6.2 Create Loadbalance Strategy Defaults
```
POST /api/loadbalance/strategies/defaults
```
No request body.

This endpoint is selected-profile scoped through `X-Profile-Id` and creates the canonical defaults for that profile only. Those defaults are the canonical operator entrypoint for the adaptive routing contract.

Response `200`:
```json
{
  "items": [
    { "id": 12, "profile_id": 3, "name": "Default legacy routing" }
  ],
  "created_count": 1,
  "created_names": ["Default legacy routing"],
  "existing_names": ["Default adaptive routing"]
}
```

The response includes the full current strategy list in `items` plus creation metadata so the caller can tell which canonical rows were created versus already present.

Returns `409` when one or more canonical default names are already occupied by non-canonical strategies in the selected profile. In that case, the error payload includes `code` plus `detail.conflicting_names` with the conflicting names.

### 6.3 Create Loadbalance Strategy
```
POST /api/loadbalance/strategies
```
Request:
```json
{
  "name": "legacy-primary",
  "strategy_type": "legacy",
  "legacy_strategy_type": "round-robin",
  "auto_recovery": {
    "mode": "enabled",
    "status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
    "cooldown": {
      "base_seconds": 45,
      "failure_threshold": 4,
      "backoff_multiplier": 3.5,
      "max_cooldown_seconds": 720
    },
    "ban": {
      "mode": "temporary",
      "max_cooldown_strikes_before_ban": 3,
      "ban_duration_seconds": 1800
    }
  }
}
```
Response `201`: Created strategy object.

Validation rules:
- `name` must be unique within the effective profile scope.
- `strategy_type` must be `legacy` or `adaptive`.
- `legacy` requires `legacy_strategy_type` and `auto_recovery`, and must not include `routing_policy`.
- `adaptive` requires `routing_policy`, must not include legacy-only fields, and `routing_policy.kind` is fixed to `adaptive`.
- Upstream request timing is controlled by the shared backend timeout settings rather than per-strategy fields.
- `hedge.delay_ms` must be within `0..300000`, and `hedge.max_additional_attempts` within `1..10`.
- `circuit_breaker.failure_status_codes` must be a unique, sorted list of valid HTTP status integers (`100..599`).
- `circuit_breaker.base_open_seconds`, `failure_threshold`, `max_open_seconds`, and `backoff_multiplier` use the same bounds as the backend defaults.
- Old clients and imported bundles that still contain `jitter_ratio` are rejected in this release.
- `circuit_breaker.ban_mode` is `off`, `temporary`, or `manual`.
- `circuit_breaker.ban_mode = "off"` requires zero strike and duration values.
- `circuit_breaker.ban_mode = "temporary"` requires both `max_open_strikes_before_ban >= 1` and `ban_duration_seconds >= 1`.
- `circuit_breaker.ban_mode = "manual"` requires `max_open_strikes_before_ban >= 1` and a zero duration value.

### 6.4 Update Loadbalance Strategy
```
PUT /api/loadbalance/strategies/{strategy_id}
```
Request: Full replacement of mutable strategy fields using the same shape as create.
Response `200`: Updated strategy object.

Strategy responses include the persisted/effective family-specific strategy document:

```json
{
  "id": 12,
  "profile_id": 3,
  "name": "adaptive-primary",
  "strategy_type": "adaptive",
  "legacy_strategy_type": null,
  "auto_recovery": null,
  "routing_policy": {
    "kind": "adaptive",
    "routing_objective": "minimize_latency",
    "hedge": {
      "enabled": true,
      "delay_ms": 1500,
      "max_additional_attempts": 1
    },
    "circuit_breaker": {
      "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
      "base_open_seconds": 45,
      "failure_threshold": 4,
      "backoff_multiplier": 3.5,
      "max_open_seconds": 720,
      "ban_mode": "temporary",
      "max_open_strikes_before_ban": 3,
      "ban_duration_seconds": 1800
    },
    "admission": {
      "respect_qps_limit": true,
      "respect_in_flight_limits": true
    }
  },
  "attached_model_count": 2,
  "created_at": "2026-03-25T08:00:00Z",
  "updated_at": "2026-03-25T08:05:00Z"
}
```

Legacy strategy responses use the same envelope but replace `routing_policy` with `legacy_strategy_type` plus `auto_recovery`.

### 6.5 Delete Loadbalance Strategy
```
DELETE /api/loadbalance/strategies/{strategy_id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the strategy is still attached to one or more native models; the response detail includes `attached_model_count`.

### 6.6 List Current Loadbalance State for a Model
```
GET /api/loadbalance/current-state
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `model_config_id` | integer | — | Model config ID in the effective profile (required, `>=1`) |

Response `200`:
```json
{
  "items": [
    {
      "connection_id": 12,
      "circuit_state": "open",
      "probe_available_at": "2026-03-30T08:02:00Z",
      "window_started_at": "2026-03-30T08:00:00Z",
      "window_request_count": 4,
      "in_flight_non_stream": 1,
      "in_flight_stream": 0,
      "consecutive_failures": 2,
      "last_failure_kind": "transient_http",
      "last_cooldown_seconds": 60.0,
      "max_cooldown_strikes": 1,
      "ban_mode": "off",
      "banned_until_at": null,
      "blocked_until_at": "2025-01-15T10:31:00Z",
      "probe_eligible_logged": false,
      "live_p95_latency_ms": 540,
      "last_probe_status": "degraded",
      "last_probe_at": "2026-03-30T08:00:30Z",
      "endpoint_ping_ewma_ms": 190.0,
      "conversation_delay_ewma_ms": 420.0,
      "state": "blocked",
      "created_at": "2025-01-15T10:30:00Z",
      "updated_at": "2025-01-15T10:30:00Z"
    }
  ]
}
```

Returns `404` when the model config does not exist in the effective profile.

`state` is one of `counting`, `blocked`, `probe_eligible`, or `banned`. `banned` is derived from `ban_mode` plus `banned_until_at`, while the additional fields expose the current circuit state, admission counters, and recent probe-derived health signals.

### 6.7 Reset Current Loadbalance State for a Connection
```
POST /api/loadbalance/current-state/{connection_id}/reset
```
Response `200`:
```json
{
  "connection_id": 12,
  "cleared": true
}
```

`cleared=false` is returned when no persisted current-state row existed for that `(profile_id, connection_id)` pair.
Reset clears cooldown, strike, and ban state together.

### 6.8 List Loadbalance Events
```
GET /api/loadbalance/events
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `model_id` | string | — | Filter by model ID (required) |
| `limit` | integer | 50 | Max results (1-200) |
| `offset` | integer | 0 | Pagination offset |

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 2,
      "connection_id": 1,
      "event_type": "banned",
      "failure_kind": "transient_http",
      "consecutive_failures": 2,
      "cooldown_seconds": 60.0,
      "blocked_until_mono": 123456.789,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
   "vendor_id": 1,
      "max_cooldown_strikes": 3,
      "ban_mode": "temporary",
      "banned_until_at": "2025-01-15T11:00:00Z",
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0
}
```

### 6.8 Get Loadbalance Event Detail
```
GET /api/loadbalance/events/{id}
```
Response `200`: Single event object with full metadata, including `failure_threshold`, `backoff_multiplier`, `max_cooldown_seconds`, and ban-related fields (`max_cooldown_strikes`, `ban_mode`, `banned_until_at`) when present.

### 6.9 Delete Loadbalance Events (Batch)
```
DELETE /api/loadbalance/events
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `before` | datetime | — | Delete events created before this time (ISO 8601). |
| `older_than_days` | integer | — | Delete events older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all loadbalance events. |

Exactly one of `before`, `older_than_days`, or `delete_all=true` must be provided.

Response `200`:
```json
{
  "accepted": true
}
```

---

## 7. Auth API

Auth APIs are global and do not require `X-Profile-Id`.

### 7.1 Auth Status
```
GET /api/auth/status
```
Response `200`:
```json
{
  "auth_enabled": true
}
```

### 7.2 Public Bootstrap
```
GET /api/auth/public-bootstrap
```
Initializes session and returns basic auth state for the login page.

### 7.3 Login
```
POST /api/auth/login
```
Request:
```json
{
  "username": "admin",
  "password": "password123",
  "session_duration": "7_days"
}
```
Response `200`: Session object. Sets the configured access-token and refresh-token cookies.

### 7.4 Logout
```
POST /api/auth/logout
```
Clears session cookies and revokes the current refresh token.

### 7.5 Refresh Session
```
POST /api/auth/refresh
```
Uses the `refresh_token` cookie to issue a new session. Implements token family rotation.

### 7.6 Get Session
```
GET /api/auth/session
```
Returns the current authenticated session state.

### 7.7 Password Reset
- `POST /api/auth/password-reset/request`: Request a reset email.
- `POST /api/auth/password-reset/confirm`: Confirm reset with OTP and new password.


---

## 8. Realtime API

Realtime routes are global management routes and do not use `X-Profile-Id`. WebSocket subscriptions carry `profile_id` explicitly in the message payload.

### 8.1 Dashboard WebSocket
```
WS /api/realtime/ws
```

Authentication:
- When operator auth is disabled, the socket can connect without cookies.
- When operator auth is enabled, the backend validates the configured access-token cookie before allowing subscriptions.

Supported channel:
- `dashboard`

Client -> server messages:
```text
{ "type": "subscribe", "profile_id": 2, "channel": "dashboard" }
{ "type": "unsubscribe" }
{ "type": "unsubscribe_channel", "channel": "dashboard" }
{ "type": "ping" }
{ "type": "pong" }
```

Server -> client messages include:
- `authenticated`
- `heartbeat`
- `subscribed`
- `unsubscribed`
- `error`
- `dashboard.update`
- `pong`

Example `dashboard.update` payload:
```json
{
  "type": "dashboard.update",
  "request_log": { "id": 101, "model_id": "gpt-4o" },
  "stats_summary_24h": { "total_requests": 42 },
  "api_family_summary_24h": { "group_by": "api_family", "items": [] },
  "spending_summary_30d": { "summary": { "total_cost_micros": 1250000 } },
  "throughput_24h": { "total_requests": 42, "buckets": [] },
  "routing_route_24h": {
    "model_id": "gpt-4o",
    "endpoint_id": 12,
    "request_count_24h": 42,
    "success_rate_24h": 97.62
  }
}
```

## 9. Error Responses

Scope-control errors follow this format:
```json
{
  "code": "profile_scope_header_missing",
  "detail": "X-Profile-Id header is required"
}
```

| Status Code | Meaning |
|---|---|
| 400 | Bad request (invalid input) |
| 404 | Resource not found |
| 409 | Conflict (stale activation CAS version, profile capacity reached, or duplicate scoped identifier) |
| 502 | Upstream service error |
| 503 | No active connections available |

---

## 10. OpenAPI Spec

The checked-in OpenAPI artifact is the management-and-health contract served at `/openapi.json`. It stays aligned with the narrative docs, but the narrative docs remain the source of truth for the Phase 1 upgrade contract.
