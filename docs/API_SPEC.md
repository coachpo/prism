# API Specification: Prism

Local `./start.sh` backend base URL follows the selected bootstrap file's `server.port`; fresh repo-local bootstrap seeds use `http://localhost:8000`.

Container and custom deployments use the listener configured in the plaintext bootstrap file. The root single-image Docker bundle publishes Nginx on `http://localhost:8080` by default and proxies to the private backend upstream on port `8000`.

Prism does not expose a backend-local `/metrics` operations endpoint or start telemetry exporters. The startup `telemetry` block is parsed for existing `config.json` compatibility only. The retained `/api/stats/*` routes remain product-facing request-history and aggregate APIs.

## 0. Profile Context Semantics
- Prism has three route classes:
  - Global management routes, which omit `X-Profile-Id`.
  - Profile-scoped management routes, which accept `X-Profile-Id` but ignore its value and resolve against Default profile id `1`.
  - Runtime proxy routes, which resolve against frozen Default profile id `1` and ignore management scope overrides.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always resolve against frozen Default profile id `1` and ignore management scope overrides.
- Global management routes include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Management job routes `/api/management/jobs*` are low-priority management routes: list resolves Default-profile jobs, while read and cancel resolve Default-profile jobs and can fall back to global log-retention jobs by ID. The frontend does not treat them as `X-Profile-Id`-scoped routes.
- Profile-scoped management routes include `/api/config/header-blocklist-rules*`, `/api/config/user-agent-client-rules*`, `/api/settings/costing`, `/api/settings/timezone`, `/api/settings/audit`, `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, `/api/models/*`, `/api/endpoints/*`, `/api/pricing-templates*`, and `/api/connections/*`.
- Detail endpoints return `404` when a resource exists outside Default profile id `1`.
- Scope-control failures return structured JSON with `code` and `detail`, where `code` is stable for machine handling and `detail` is safe to show to operators.


## 1. Management API (`/api/*`)

### 1.0 Startup Config File

Prism loads steady-state startup settings from the plaintext `config.json` selected by `PRISM_CONFIG_PATH`. The live v1 file requires `meta`, `server`, `database.url`, `database.pools`, `database.managementAdmission`, `runtime.secretEncryptionKey`, `runtime.transport`, `runtime.sideEffects`, `http.corsAllowedOrigins`, and `auth`. Optional top-level sections are `alerting`, `mail`, `telemetry`, and `stateTransfer`; `mail`, `telemetry`, and `stateTransfer` are parsed for compatibility only and do not re-enable retired behavior. R2 removed the management API for editing that file; external edits require a Prism restart before they affect the running process.

### 1.1 Profiles

The profile management API is frozen. Prism preserves the `profiles` table and all `profile_id` storage columns, but no longer exposes `/api/profiles*` management routes. Profile-scoped management APIs are pinned to Default profile id `1`.

---

### 1.2 Catalog Management

Prism no longer exposes a catalog management product surface. Model compatibility is carried by each model's required `api_family`; catalog metadata does not participate in runtime routing.

---

### 1.3 Model Configurations

#### List Models
```
GET /api/models
```
Response `200`: Array of model objects.

#### Get Model
```
GET /api/models/{id}
```
Response `200`: Full model object with required `api_family`, optional `loadbalance_strategy_id`, ordered `access_targets`, and attached Terminal Target summaries in the effective profile scope. Model create/update does not author access targets; use `/api/models/{id}/targets` for ordered same-family model targets and model-owned Terminal Target ownership edges.

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
Response `200`: `items[]`, where each item contains an `endpoint_id` and the models that can reach that endpoint through Terminal Targets. Endpoints are reusable and may be referenced by Terminal Targets owned by different models.

#### Get Models by Endpoint
```
GET /api/models/by-endpoint/{endpoint_id}
```
Response `200`: Array of models that can reach the endpoint through Terminal Targets within the effective profile scope.

#### Create Model
```
POST /api/models
```
Request:
```json
{
  "api_family": "openai",
  "model_id": "gpt-4o-public",
  "display_name": "GPT-4o Public",
  "loadbalance_strategy_id": 7,
  "openai_accepted_format": "dual_native",
  "is_enabled": false
}
```
Response `201`: Created model object.

Validation rules:
- `model_id` must be unique within the effective profile scope.
- `api_family` is required on every model contract and remains the authoritative runtime compatibility field.
- `is_enabled` defaults to `false` when omitted. Enabling a model still requires at least one enabled access target in the stored graph.
- Create and update payloads reject `access_targets`, exact-facade fields, and retired model-owned context capability fields.
- Ordered same-profile, same-`api_family` model targets are managed through `/api/models/{id}/targets`.
- Model target self-reference and target cycles are rejected by the target management routes.
- Deleting a model referenced by another model target returns `409` until the target rows are removed or updated. Deleting an owner model deletes its Terminal Targets with the owning target rows.

#### Update Model
```
PUT /api/models/{id}
```
Request (all fields optional):
```json
{
  "api_family": "openai",
  "model_id": "gpt-4o-public-updated",
  "display_name": "GPT-4o Public (Updated)",
  "loadbalance_strategy_id": 9,
  "is_enabled": true
}
```
Update payloads use the same field contract as create and do not mutate access targets. Existing access targets and private Terminal Targets are preserved and remain managed by model-scoped target and connection routes. Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `200`: `{ "deleted": true }`. Returns `409` if other models still reference this model through model targets. When deletion succeeds, the owner model's private connection rows and their internal owning access-target rows are removed in the same operation.

---

### 1.4 Endpoints (Profile-Scoped Credentials)

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

### 1.5 Terminal Targets and Model Access Targets

Terminal Targets are Prism's product term for model-private endpoint bindings within one profile. Terminal Targets are represented as `connections` / `connection_id` in the compatibility API and database schema. A compatibility connection carries its owner model's `api_family`, endpoint reference or inline endpoint create payload, pricing template, and optional admission limits. Endpoints remain reusable, so many Terminal Targets may point at the same endpoint. `model_access_targets.target_type="connection"` is an internal ownership and runtime routing edge, not a public assignment surface for connection IDs.

#### List Terminal Targets Through `/api/connections`
```
GET /api/connections
```
Response `200`: Array of compatibility connection objects in the effective profile. This is a read surface for Terminal Targets. Public `/api/connections` mutation routes reject writes and direct operators to model detail.

#### Get Terminal Target Through `/api/connections/{connection_id}`
```
GET /api/connections/{connection_id}
```
Response `200`: Single compatibility connection object in the effective profile. Returns `404` when the connection does not exist in that profile.

#### List Terminal Targets Attached to Models
```
POST /api/models/connections/batch
```
Request:
```json
{
  "model_config_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains a `model_config_id` and the Terminal Targets owned by that model's enabled or disabled internal connection targets, ordered by target position.

#### List Terminal Targets For One Model
```
GET /api/models/{model_config_id}/connections
```
Response `200`: Ordered array of Terminal Targets owned by the given model in the effective profile.

#### Create Terminal Target
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
  "openai_text_capability": "responses_only",
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
  "openai_text_capability": "dual_native",
  "pricing_template_id": null,
  "qps_limit": null,
  "max_in_flight_non_stream": null,
  "max_in_flight_stream": null
}
```
Response `201`: Created Terminal Target object, represented as a compatibility connection, plus its owner routing edge for the model.

Create semantics:
- Exactly one of `endpoint_id` or `endpoint_create` is required.
- The connection `api_family` is derived from the owner model. A conflicting request value is rejected.
- `priority` is rejected with `422`; Terminal Target ordering for a model is owned by `/api/models/{model_config_id}/targets` positions.
- Limiter fields are optional. `null` means unlimited. Positive integers apply per-connection request admission limits.
- `openai_text_capability` is the OpenAI text runtime capability source of truth for OpenAI-family Terminal Targets. It accepts `responses_only`, `chat_completions_only`, or `dual_native`, and is required for OpenAI rows. Non-OpenAI rows must omit it or persist `null`.

#### Update Terminal Target
```
PATCH /api/models/{model_config_id}/connections/{connection_id}
```
Request: Mutable compatibility connection metadata: `endpoint_id`, `endpoint_create`, `is_active`, `name`, `auth_type`, `custom_headers`, `openai_text_capability`, `pricing_template_id`, `qps_limit`, `max_in_flight_non_stream`, `max_in_flight_stream`.

`endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`. `priority` is rejected with `422`. The owner model and connection `api_family` are immutable.

Response `200`: Updated Terminal Target object, represented as a compatibility connection. Public `PUT` or `PATCH /api/connections/{connection_id}` rejects mutation requests.

#### List Terminal Target References
```
GET /api/connections/{connection_id}/references
```
Response `200`: Owner references for the Terminal Target, wrapped with the requested compatibility `connection_id`. A valid connection has one owner:
```json
{
  "connection_id": 15,
  "items": [
    {
      "target_id": 42,
      "model_config_id": 7,
      "model_id": "gpt-4o",
      "api_family": "openai",
      "position": 0,
      "is_enabled": true
    }
  ]
}
```

#### Update Terminal Target Pricing Template

Pricing templates are assigned through the Terminal Target update route by setting `pricing_template_id`. Public connection-level pricing-template mutation routes reject writes.

#### Delete Terminal Target
```
DELETE /api/models/{model_config_id}/connections/{connection_id}
```
Response `200`: `{ "deleted": true }`.

Deletes the Terminal Target and its internal owner access-target row together, subject to enabled-model target validation. Public `DELETE /api/connections/{connection_id}` rejects mutation requests.

Rejected legacy mutation routes return `400` with guidance to use model-scoped Terminal Target routes instead: `POST /api/connections`, `PUT/PATCH/DELETE /api/connections/{connection_id}`, `PUT /api/connections/{connection_id}/pricing-template`, `PUT /api/models/{model_config_id}/connections/{connection_id}`, `PUT /api/models/{model_config_id}/connections/{connection_id}/pricing-template`, and `PATCH /api/models/{model_config_id}/connections/{connection_id}/priority`.

#### Model Target Routes
```
GET /api/models/{model_config_id}/targets
POST /api/models/{model_config_id}/targets
PUT /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}/position
DELETE /api/models/{model_config_id}/targets/{target_id}
```

Model target rows define a model's ordered access graph. Public authoring creates same-family model targets only:
```json
{
  "target_type": "model",
  "target_model_id": "gpt-4o-backup",
  "position": 0,
  "is_enabled": true
}
```

Target semantics:
- Public `POST /api/models/{model_config_id}/targets` accepts `target_type="model"` with exact `target_model_id`, `position`, and `is_enabled`. Obsolete `weight` and `target_priority` keys reject on create, update, and patch payloads.
- Runtime routing consumes exact target-model IDs only. Target payloads do not accept regex matcher fields, capability-metadata expansion, weighted policy names, or hidden priority fields.
- Public target authoring rejects submitted `target_type="connection"`, `connection_id`, or `target_connection_id` values. Private connections are created and managed through `/api/models/{model_config_id}/connections`.
- `PUT` and `PATCH /api/models/{model_config_id}/targets/{target_id}` update target metadata within the owning model scope. For internal connection targets, `PATCH` accepts only `position` and `is_enabled`; pointer fields are immutable and obsolete weight fields must stay omitted.
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position` is the dedicated move route and accepts `to_index`.
- Existing internal `target_type="connection"` rows identify the source model that owns a private connection and provide the runtime terminal routing edge.
- Target positions are contiguous starting at `0` and determine routing order for that source model. Position is an ordering key only, not a priority, tier, or weight replacement.
- Target validation is Default-profile scoped, same-family, enabled-target aware, and cycle-safe.

#### Base URL Validation

On endpoint create (`POST`) and update (`PUT`), the `base_url` is:
1. **Normalized**: Trailing slashes are stripped (e.g., `https://api.example.com/` → `https://api.example.com`)
2. **Validated**: Rejected with HTTP 422 if scheme/host is missing or the URL includes a query string or fragment.
3. **Joined at runtime**: Path prefixes are allowed. Runtime appends the allowlisted operation path to the normalized endpoint path without version-segment de-duplication.

Valid examples:
- ✅ `https://api.openai.com`
- ✅ `https://api.openai.com/v1`
- ✅ `https://generativelanguage.googleapis.com`
- ✅ `https://generativelanguage.googleapis.com/v1beta`
- ❌ `https://api.openai.com/v1?timeout=30`
- ❌ `https://api.openai.com/v1#runtime`

### 1.6 Pricing Templates

#### List Pricing Templates
```
GET /api/pricing-templates
```
Response `200`: Array of pricing template list items in the effective profile scope.

#### Get Pricing Template
```
GET /api/pricing-templates/{id}
```
Response `200`: Pricing template object in the effective profile scope.
Returns `404` when the template does not exist in the effective profile.

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
  "cache_creation_price": "0",
  "reasoning_price": "15.00"
}
```
Response `201`: Created pricing template object.

Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Create and update ingress normalizes missing, `null`, empty, and whitespace-only values for any of those five fields to `"0"` before decimal validation. Explicit `"0"` is configured free pricing. It is not missing price data. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or for required FX data that cannot be applied.

#### Import Pricing Templates
```
POST /api/pricing-templates/import
```
Request:
```json
{
  "mode": "upsert_by_name",
  "templates": [
    {
      "name": "gpt-4o",
      "pricing_unit": "PER_1M",
      "pricing_currency_code": "USD",
      "input_price": "2.5",
      "output_price": "10",
      "cached_input_price": "1.25",
      "cache_creation_price": "0",
      "reasoning_price": "0",
      "description": "OpenAI GPT-4o"
    }
  ]
}
```
Response `200`: `{ "created": 1, "updated": 0, "skipped": [], "errors": [] }`.

`mode` is either `upsert_by_name` or `create_only`. Imports are Default-profile scoped and use one transaction: validation errors return `400` with row-level `errors[]`, and no templates are created or updated.

#### Update Pricing Template
```
PUT /api/pricing-templates/{id}
```
Request: Full replacement for mutable pricing template fields. Missing, `null`, empty, and whitespace-only price component fields normalize to `"0"` before validation. Optional `expected_updated_at` is an RFC3339 optimistic-concurrency guard; when supplied, the backend returns `409` if it does not match the current row `updated_at`.
Response `200`: Updated pricing template object.

#### Delete Pricing Template
```
DELETE /api/pricing-templates/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the template is still referenced by Terminal Targets; response `detail` includes a compatibility `connections` array with dependency details.

#### List Terminal Targets Using Template
```
GET /api/pricing-templates/{id}/connections
```
Response `200`: Usage payload with `template_id` and `items[]` (`connection_id`, `connection_name`, `model_config_id`, `model_id`, `endpoint_id`, `endpoint_name`).
---

### 1.7 Settings API

#### Get Auth Settings
```
GET /api/settings/auth
```
Response `200`: Global operator-auth settings (`auth_enabled`, `username`, `has_password`, `proxy_key_limit`).

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

#### Proxy API Keys
```
GET /api/settings/auth/proxy-keys
POST /api/settings/auth/proxy-keys
PATCH /api/settings/auth/proxy-keys/{id}
POST /api/settings/auth/proxy-keys/{id}/rotate
DELETE /api/settings/auth/proxy-keys/{id}
```

Proxy-key lifecycle contract:
- List responses are arrays of proxy-key items with `id`, `name`, `key_prefix`, `key_preview`, `is_active`, `expires_at`, `last_used_at`, `last_used_ip`, `notes`, `rotated_from_id`, `created_at`, and `updated_at`.
- Create accepts `name`, optional `notes`, and optional RFC3339 `expires_at`. Response `201` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Update requires a non-empty `name` and accepts optional `notes`, `is_active`, and RFC3339 `expires_at`. Response `200` is the updated item. Omitted or JSON `null` `expires_at` preserves the current expiry; update does not expose a clear-expiry operation.
- Rotate is lineage-creating, not in-place mutation: the historical row becomes inactive, a successor row is created with `rotated_from_id` pointing at the predecessor, and response `200` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Delete returns `{ "deleted": true }`.

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

`/api/settings/auth*` routes are global management endpoints. `/api/settings/costing`, `/api/settings/timezone`, and `/api/settings/audit` are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

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
  "profile_id": 1,
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

There is no standalone `/api/settings/monitoring` route, `/api/monitoring/*` family, or Terminal Target probe route in the current live API contract. Current operator-facing observability and routing-health surfaces are provided through `/api/stats/*`, `/api/audit/*`, and `/api/loadbalance/*`.

---

### 1.9 User-Agent Client Rules (System Global + User Profile-Scoped)

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

### 1.10 Header Blocklist Rules (System Global + User Profile-Scoped)

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
    "profile_id": null,
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

### 1.11 Removed Management Surface

The former CLIProxyAPI management control plane and runtime context-overflow promotion surfaces are not part of the current API.

---

## 2. Runtime Proxy API

Prism's runtime proxy is an explicit allowlist, not a full vendor API clone. It supports only the operations listed in this section through frozen Default profile id `1`. Other vendor routes, including stored-object, retrieve, delete, cancel, embedding, file, batch, and admin APIs, are outside Prism's runtime contract unless they appear in this allowlist.

Runtime proxy routes ignore management `X-Profile-Id` overrides and always resolve against frozen Default profile id `1`. Profile-scoped management reads and writes are pinned to Default profile id `1`; they do not switch proxy traffic.

When operator auth is enabled, runtime proxy routes require a valid active, unexpired proxy API key. Prism accepts the key as `Authorization: Bearer <key>`, `X-API-Key: <key>`, or `X-Goog-Api-Key: <key>`. Missing keys return `401` with `Proxy API key required`; invalid, inactive, expired, or unknown keys return `401` with `Invalid proxy API key`. When auth is disabled, supported runtime routes continue without proxy-key authentication.

### 2.1 Supported Runtime Operations

| Operation | Canonical operation name | Supported request |
|---|---|---|
| OpenAI models list | `openai.models` | `GET /v1/models` |
| OpenAI chat completions | `openai.chat_completions` | `POST /v1/chat/completions` |
| OpenAI Responses | `openai.responses` | `POST /v1/responses` |
| OpenAI Responses input tokens | `openai.responses.input_tokens` | `POST /v1/responses/input_tokens` |
| OpenAI Responses compact | `openai.responses.compact` | `POST /v1/responses/compact` |
| Anthropic Messages | `anthropic.messages` | `POST /v1/messages` |
| Anthropic token count | `anthropic.count_tokens` | `POST /v1/messages/count_tokens` |
| Gemini generate content | `gemini.generate_content` | `POST /v1beta/models/{model}:generateContent` |
| Gemini stream generate content | `gemini.stream_generate_content` | `POST /v1beta/models/{model}:streamGenerateContent` |
| Gemini token count | `gemini.count_tokens` | `POST /v1beta/models/{model}:countTokens` |

Each allowlisted row maps to one canonical operation name. Provider-forwarded runtime operations persist that name as `operation_name` in runtime telemetry. Operation names are part of the runtime contract, not aliases for broader vendor route groups. The Gemini `{model}` path binding is one non-empty path segment and cannot contain `/` or `:`. Nested Gemini model paths are not part of this runtime contract.

### 2.2 Unsupported Routes and Methods

Unsupported runtime routes return a Prism JSON `404` response before Prism reads the request body, resolves a model, contacts a provider, creates runtime admission state, submits runtime side effects, or writes runtime persistence rows. The current error detail is `Runtime operation not found`.

Wrong methods on supported runtime paths return a Prism JSON `405` response before the same downstream seams run. The response includes the supported method in `Allow`, and the current error detail is `Method not allowed for runtime operation`.

Supported runtime operation request bodies are capped at `20 MiB`. Oversized requests return JSON `413` with `error: "request_body_too_large"` and `limit_bytes` before runtime planning or provider transport.

### 2.2A Routing Failures

Runtime planning applies the requested model's ordered access graph and attached Ban Policy strategy before provider transport. If no eligible Terminal Target is available inside the current retry window, Prism returns a routing-availability error before opening an upstream request. If all otherwise eligible attempts are blocked by admission counters, Prism returns `503` with `error: "admission_exhausted"` plus route-reason metadata before upstream transport. Unsupported translated OpenAI sibling-operation shapes reject with `openai_request_translation_unsupported` when adapter capability checks fail.

Request-log detail keeps flat final-target attribution fields such as `resolved_target_model_id`, `terminal_target_id`, `selected_terminal_target_id`, `endpoint_id`, and `operation_translation_mode`. Deleted model-owned routing metadata is not exposed on public detail responses.

### 2.2B OpenAI sibling-operation translation

OpenAI Chat Completions and Responses targets can be siblings for runtime planning. Translation eligibility is explicit and terminal-target based through `openai_text_capability`: `responses_only`, `chat_completions_only`, or `dual_native`. Native-compatible terminal attempts keep `operation_translation_mode = "none"`, but planning still follows authored access-target and terminal-target order and chooses the first compatible native or translated mode. Compatible sibling targets may use `openai_responses_to_chat_completions` or `openai_chat_completions_to_responses` only when the selected connection's capability is not native for the ingress operation and the adapter approves the request shape.

Sibling OpenAI text translation is always on for adapter-approved Chat Completions and Responses shapes. There is no startup toggle. Unsupported shapes are not universally routable: adapter-rejected conversions remain blocked by capability checks and reject before provider transport with `openai_request_translation_unsupported` when translation compatibility is the blocker. Supported tool definitions, tool/function calls, media content parts, and some stream conversion shapes can translate; unsupported metadata can be recorded as explicit translation loss instead of forcing whole-request rejection. Public Responses requests with stateful shapes such as `previous_response_id` can pass through when missing context estimation is the only blocker, but they still reject if translation compatibility fails. Responses adjunct operations, `openai.responses.input_tokens` and `openai.responses.compact`, require responses-capable targets and never translate to Chat Completions.

Translated non-stream and stream responses are rewritten back to the ingress operation shape for the client. Runtime usage remains canonical from the raw upstream payload or terminal stream event, translated responses strip unsafe entity headers before writing to the client, and audit body capture stays upstream-native rather than translated.

For Chat Completions ingress promoted to a Responses-only target, the client contract remains Chat Completions. Non-stream responses return `object: "chat.completion"`, `choices`, the requested public model ID, and Chat-shaped usage fields. Prism does not expose the raw Responses envelope to the client. Streaming responses return Chat Completions SSE chunks and terminal `data: [DONE]` while accepting text lifecycle events from the Responses upstream: `response.output_item.added`, `response.content_part.added`, `response.output_text.delta`, `response.output_text.done`, `response.content_part.done`, `response.output_item.done`, and terminal `response.completed`. Adapter-supported tool/function-call and content event shapes can translate; unsupported semantic stream shapes still reject deterministically with `openai_stream_translation_unsupported`.

Ingress observability remains stable: `operation_name` is always the client-visible operation. Additive upstream fields use `upstream_operation_name`, `operation_translation_mode`, and `upstream_request_path` for request logs, usage events, and request-log detail. `upstream_request_path` is the sanitized operation path Prism sent upstream, not an unbounded raw URL. For Chat ingress translated to Responses upstream, public request logs preserve `operation_name = "openai.chat_completions"`, may record `upstream_operation_name = "openai.responses"`, and preserve `operation_translation_mode = "openai_chat_completions_to_responses"`.

#### 2.2B.1 Application capability matrix

The following application-spec example assumes these OpenAI text capabilities and matching access-target order:
- `gpt-5.5`: `dual_native`
- `gpt-5.4`: `dual_native`
- `deepseek-v4-flash`: `chat_completions_only`

Native request behavior:

| Requested model | Ingress path | Target capability | Upstream path | `operation_translation_mode` | Client-visible shape |
|---|---|---|---|---|---|
| `gpt-5.5` | `/v1/responses` | `dual_native` | `/v1/responses` | `none` | Responses |
| `gpt-5.5` | `/v1/chat/completions` | `dual_native` | `/v1/chat/completions` | `none` | Chat Completions |
| `gpt-5.4` | `/v1/responses` | `dual_native` | `/v1/responses` | `none` | Responses |
| `gpt-5.4` | `/v1/chat/completions` | `dual_native` | `/v1/chat/completions` | `none` | Chat Completions |
| `deepseek-v4-flash` | `/v1/responses` | `chat_completions_only` | `/v1/chat/completions` | `openai_responses_to_chat_completions` | Responses |
| `deepseek-v4-flash` | `/v1/chat/completions` | `chat_completions_only` | `/v1/chat/completions` | `none` | Chat Completions |

Translated rows above remain subject to adapter approval for the specific request shape. If a request shape is not safely translatable, Prism rejects that translated candidate instead of forcing conversion.

### 2.2C Retired Exact OpenAI Facade Routing

Exact OpenAI facade routing and its model fields are retired. Runtime planning uses the requested model's ordinary access-target graph, operation translation checks, Terminal Target runtime capability, and the attached Ban Policy strategy. Prism no longer performs context-window preflight filtering or returns context-window-exceeded planning errors.

### 2.2D Retired Overflow Replay

Model-scoped overflow replay and its authoring fields are retired. Runtime planning now uses the ordinary operation registry, access-target graph, sibling-operation translation checks, and the attached Ban Policy strategy. Public request-log and usage surfaces keep flat requested model, final target, Terminal Target, endpoint, and operation fields without nested retired routing metadata.

### 2.3 OpenAI Operations

#### Models
```
GET /v1/models
```
Response: Local OpenAI-shaped list of enabled `api_family="openai"` models for frozen Default profile id `1`. Prism does not contact upstream providers for this operation.

#### Chat Completions
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
Response: Native attempts are proxied from the upstream OpenAI-compatible endpoint. Translated sibling attempts are rewritten back to Chat Completions shape before the client sees them. Canonical operation name: `openai.chat_completions`.

#### Responses
```
POST /v1/responses
```
Request (OpenAI Responses generation format):
```json
{
  "model": "gpt-4o",
  "input": "Hello!",
  "stream": false
}
```
Response: Native attempts are proxied from the upstream OpenAI-compatible endpoint. Translated sibling attempts are rewritten back to Responses shape before the client sees them. Canonical operation name: `openai.responses`.

#### Responses Input Tokens
```
POST /v1/responses/input_tokens
```
Request uses the OpenAI Responses input-token counting format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.input_tokens`.

#### Responses Compact
```
POST /v1/responses/compact
```
Request uses the OpenAI Responses compaction format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.compact`.

### 2.4 Anthropic Operations

#### Messages
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
Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.messages`.

#### Token Count
```
POST /v1/messages/count_tokens
```
Request uses the upstream Anthropic-compatible token-count body, including body-bound `model`.
Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.count_tokens`.

### 2.5 Gemini Operations

#### Generate Content
```
POST /v1beta/models/{model}:generateContent
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
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.generate_content`.

#### Stream Generate Content
```
POST /v1beta/models/{model}:streamGenerateContent
```
Request uses the upstream Gemini native generate-content body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.stream_generate_content`.
The `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

#### Count Tokens
```
POST /v1beta/models/{model}:countTokens
```
Request uses the upstream Gemini native token-count body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.count_tokens`.
For all Gemini runtime paths in this section, the `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

### 2.6 Streaming

Streaming stays operation-native for native attempts: `openai.chat_completions`, `openai.responses`, and `anthropic.messages` use their upstream-compatible request body flags, while `gemini.stream_generate_content` uses `POST /v1beta/models/{model}:streamGenerateContent`. Native streaming responses are proxied from upstream; translated OpenAI sibling streams are rewritten back to the ingress operation's SSE shape.
For Gemini, the `gemini.stream_generate_content` path is authoritative: `POST /v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`. `gemini.generate_content` remains the non-stream generate-content operation.

### 2.7 Token Usage Extraction

The gateway extracts token usage from upstream responses and logs canonical disjoint token components to `request_logs`. Extraction is selected by the resolved canonical operation name and its hook collection. `input_tokens` is base input only, `output_tokens` is base output only, cache-read input, cache-creation input, and reasoning output are separate fields, and `total_tokens` uses the provider total when one is supplied.

**Non-streaming responses:**
| Canonical operation name | Response format | Extraction path |
|---|---|---|
| `openai.chat_completions` | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses`, `openai.responses.compact` | `{"usage": {"input_tokens": N, "output_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses.input_tokens` | `{"input_tokens": N, "total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `anthropic.messages` | `{"usage": {"input_tokens": N, "cache_read_input_tokens": N, "cache_creation_input_tokens": N, "output_tokens": N}}` | Base input, cache-read input, cache-creation input, and base output stay separate; total is derived when upstream omits it |
| `anthropic.count_tokens` | `{"input_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `gemini.generate_content`, `gemini.stream_generate_content` when handled as non-stream JSON | `{"usageMetadata": {"promptTokenCount": N, "cachedContentTokenCount": N, "candidatesTokenCount": N, "thoughtsTokenCount": N, "totalTokenCount": N}}` | Base input subtracts cache-read input; base output subtracts reasoning output; provider `totalTokenCount` stays authoritative |
| `gemini.count_tokens` | `{"totalTokens": N}` or `{"total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from operation-specific terminal events:
| Canonical operation name | Usage events | Extraction |
|---|---|---|
| `openai.chat_completions` | Final usage chunk before `[DONE]` | Same canonical disjoint fields as non-streaming usage |
| `openai.responses` | `response.completed` event with a `usage` object when provided by upstream | Same canonical disjoint fields as non-streaming usage |
| `anthropic.messages` | `message_start` usage plus cumulative `message_delta.usage.output_tokens` | Base input, cache-read input, cache-creation input, and final base output stay separate |
| `gemini.stream_generate_content` | Stream terminal or final chunk carrying `usageMetadata` | Same canonical disjoint fields as Gemini non-stream `usageMetadata` |

If token data cannot be extracted from the provider response, runtime usage token fields are logged as `null`. Completed streams that lack required usage keep `MISSING_TOKEN_USAGE`; interrupted or no-terminal streams with missing required tokens use `STREAM_USAGE_UNAVAILABLE` when their classified stream outcome made terminal usage unavailable. Aggregate `cached_tokens` is derived-only from cache-read plus cache-creation input tokens and is not a persisted runtime component.

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

Stats APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

### 4.0 Dashboard Stats
```
GET /api/stats/dashboard
```
This is the canonical overview dashboard read path. It returns one backend-computed, stats-only aggregate snapshot for the effective profile, including overview metrics, API-family rows, top-spending models, and the Routing Health Map. It does not include recent request rows, request-log IDs, or request-log cursor data. Recent activity is served by `GET /api/stats/dashboard/recent-activity`.

Query parameters: none. Legacy `window` query values are ignored. The endpoint always returns the canonical aggregate snapshot and does not expose the old top-level `window`, `covers`, `freshness`, or `metrics` shape. Snapshot freshness is ordered by lexicographic `snapshot_revision`; `source_watermark` is diagnostic only.

Response `200`:
```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "snapshot_revision": "01JZ8Y3K2N4P6R8T0V1W2X3Y4Z",
  "source_watermark": {
    "latest_usage_event_created_at": "2026-04-19T11:59:58Z",
    "latest_usage_event_id": 345
  },
  "coverage_24h": {
    "from": "2026-04-18T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "coverage_30d": {
    "from": "2026-03-20T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "health": {
    "lag_seconds": 0,
    "stale": false,
    "stale_after_seconds": 120
  },
  "metric_snapshot": {
    "active_models": 12,
    "average_rpm": 0.7,
    "average_rpm_request_total": 42,
    "avg_latency": 523,
    "error_rate": 2.38,
    "p95_latency": 900,
    "priced_request_count": 40,
    "stream_share": 18.5,
    "success_rate": 97.62,
    "total_cost": 1250000,
    "total_models": 14,
    "total_requests": 42,
    "unpriced_request_count": 2
  },
  "api_family_rows": [
    { "key": "openai", "total_requests": 42, "success_rate": 97.62 }
  ],
  "top_spending_models": [],
  "routing_health_map": {
    "nodes": [],
    "links": [],
    "endpointCount": 0,
    "modelCount": 0,
    "activeConnectionTotal": 0,
    "activeTerminalTargetTotal": 0,
    "trafficRequestTotal24h": 0
  }
}
```

`routing_health_map` is assembled by the backend from Default-profile model, access-target, endpoint, connection, and final-attributed usage-event data.

### 4.0A Dashboard Recent Activity
```
GET /api/stats/dashboard/recent-activity?limit=N
```
This endpoint is the separate request-history-backed activity feed for dashboard bootstrap and repair. It is not embedded in the dashboard snapshot. The default limit is `12`; the maximum limit is `50`. Rows are ordered by `(created_at DESC, request_log_id DESC)`.

Response `200`:
```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "activity_watermark": {
    "latest_request_log_created_at": "2026-04-19T11:59:59Z",
    "latest_request_log_id": 101
  },
  "items": [
    {
      "request_log_id": 101,
      "created_at": "2026-04-19T11:59:59Z",
      "model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "resolved_target_model_id": "gpt-4o-mini",
      "resolved_target_model_label": "GPT-4o mini",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "status_code": 200,
      "response_time_ms": 523,
      "ttft_ms": 120,
      "completion_duration_ms": 403,
      "is_stream": true,
      "stream_outcome": "completed",
      "total_tokens": 1234,
      "total_cost_user_currency_micros": 1250000,
      "priced_flag": true,
      "unpriced_reason": null,
      "report_currency_symbol": "$"
    }
  ]
}
```

Recent activity links into request-log investigation by `request_log_id`. It does not define snapshot freshness, and activity publication does not force a dashboard snapshot rebuild.

### 4.1 Usage Snapshot
```
GET /api/stats/usage-snapshot
```
This is the REST analytics snapshot contract for API callers, debugging, and the `/observe?tab=analytics` UI polling path.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Snapshot range preset. Supported values: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |

The snapshot is backed by `backend/internal/httpapi/management/stats/service.go` together with the aggregation types and query helpers in `backend/internal/domain/stats/snapshot.go` and `backend/internal/domain/stats/types.go`.

The snapshot is aggregated from persisted usage-event rows. Endpoint aggregates read the stored `usage_request_events.endpoint_label_snapshot` value and expose it as public `endpoint_label`, so historical labels survive later endpoint renames or deletion. `/api/stats/dashboard` is the canonical overview aggregate that also includes the backend-computed Routing Health Map. Exact request investigation remains on `/observe/requests`, while dashboard and other pages continue to use the shared stats routes below.

Response `200` includes `latency_trends` alongside `request_trends`, `token_usage_trends`, `token_type_breakdown`, and `cost_overview`. `latency_trends.hourly[]` and `latency_trends.daily[]` use the same series key/label shape as request trends; each point exposes `bucket_start`, `p50_ms`, and `p95_ms`. Empty latency buckets keep the bucket and return `null` percentile values.

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
| `status_family` | string | — | Filter by status family (`2xx`, `4xx`, or `5xx`) |
| `status_code` | integer | — | Exact response status-code filter |
| `error_text` | string | — | Case-insensitive substring match against `error_detail` |
| `priced` | boolean | — | Filter by retained request-log `priced_flag` (`true` or `false`) |
| `unpriced_reason` | string | — | Exact unpriced reason filter: `PRICING_DISABLED`, `MISSING_TOKEN_USAGE`, `STREAM_USAGE_UNAVAILABLE`, or `MISSING_PRICE_DATA` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `client_rule_id` | integer | none | Filter by caller client, matched against `caller_user_agent` only through enabled User-Agent Client Rules |
| `resolved_target_model_id` | string | none | Filter by final target model selected for the attempt |
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset |

Frontend request-log route contract:
- `/observe/requests` defaults to the last 24 hours by deriving `from_time` from `time_range=24h`; generated URLs omit `time_range=24h` because it is the page default.
- The page's `status=success` URL alias maps to backend `status_family=2xx`; direct `status_family=2xx`, `4xx`, and `5xx` are also supported backend filters.
- The page CSV export is client-only and exports only the rows already loaded on the current table page. There is no full-range server-side CSV export endpoint.

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
    ],
    "clients": [
      {
        "client_rule_id": 7,
        "client_label": "Codex"
      }
    ],
    "resolved_target_models": [
      {
        "resolved_target_model_id": "gpt-4o",
        "model_label": "GPT-4o"
      }
    ]
  },
  "items": [
    {
      "id": 1,
      "model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "resolved_target_model_id": "gpt-4o",
      "resolved_target_model_label": "GPT-4o",
      "api_family": "openai",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "connection_id": 1,
      "terminal_target_id": 1,
      "status_code": 200,
      "response_time_ms": 1234,
      "ttft_ms": 320,
      "completion_duration_ms": 914,
      "is_stream": false,
      "stream_outcome": "not_streaming",
      "stream_error_kind": null,
      "output_tokens": 42,
      "total_tokens": 57,
      "total_cost_user_currency_micros": 1250,
      "priced_flag": true,
      "unpriced_reason": null,
      "reasoning_effort": "low",
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

The list route is the slim browse contract used by `/observe/requests` and other row-summary consumers. It keeps one row per upstream attempt, returns `filter_options.endpoints` for the endpoint dropdown, `filter_options.models` for the requested-model dropdown, `filter_options.clients` for caller client filtering, and `filter_options.resolved_target_models` for final-target filtering. It includes requested-model labels, final-target labels, `stream_outcome`, and `stream_error_kind` for display. The current request-log page uses page sizes `100`, `300`, and `500`, with `100` as the frontend default. This retained-history route is the operator drill-in surface for investigation, not a dashboard aggregate or metrics endpoint.

`filter_options` always includes `endpoints`, `models`, `clients`, and `resolved_target_models`. `filter_options.models` is request-log scoped and contains `{ model_id, model_label }` entries. `filter_options.clients` contains `{ client_rule_id, client_label }` entries built from enabled User-Agent Client Rules. `client_rule_id` filtering is caller-only and matches `caller_user_agent`; it never matches `upstream_user_agent`. `filter_options.resolved_target_models` contains `{ resolved_target_model_id, model_label }` entries for final-target filtering. Empty option sets are returned as empty arrays instead of omitted fields. `ingress_request_id` groups multiple attempt rows that belong to one incoming runtime request. `model_id` stays the requested model and `resolved_target_model_id` captures the final target model for that attempt, while request-log row and detail payloads use `resolved_target_model_label` for the matching display label.

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
    "resolved_target_model_id": "gpt-4o",
    "resolved_target_model_label": "GPT-4o",
    "api_family": "openai",
    "status_code": 200,
    "response_time_ms": 1234,
    "ttft_ms": 320,
    "completion_duration_ms": 914,
    "is_stream": false,
    "stream_outcome": "not_streaming",
    "stream_error_kind": null,
    "stream_error_detail": null
  },
  "request": {
    "operation_name": "openai.chat_completions",
    "upstream_operation_name": "openai.chat_completions",
    "operation_translation_mode": "none",
    "request_path": "/v1/chat/completions",
    "upstream_request_path": "/v1/chat/completions",
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
    "error_detail": null,
    "request_generation_params": {"temperature": 0.7},
    "request_generation_params_status": "captured"
  },
  "routing": {
    "profile_id": 1,
    "endpoint_label": "Primary OpenAI",
    "endpoint_id": 12,
    "terminal_target_id": 1,
    "selected_terminal_target_id": 1,
    "endpoint_base_url": "https://api.openai.com",
    "endpoint_description": "Primary OpenAI",
    "audit_enabled_at_request": false,
    "audit_capture_bodies_at_request": false
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
    "pricing_snapshot_cache_read_input": "0",
    "pricing_snapshot_cache_creation_input": "0",
    "pricing_snapshot_reasoning": "0",
    "pricing_config_version_used": 1
  }
}
```

Request-log detail uses the same canonical disjoint token components as runtime persistence: base input, base output, cache-read input, cache-creation input, reasoning output, and provider or derived total. Pricing snapshots store the five concrete pricing strings used for the attempt. Explicit `"0"` prices are configured free pricing and produce zero component cost without marking the row unpriced. Public request-log detail routing exposes `terminal_target_id` and `selected_terminal_target_id`; it does not expose `routing.connection_id` on the detail surface.

Request-log detail keeps ingress and upstream attribution separate. `request.operation_name` and `request.request_path` are ingress-led. `request.upstream_operation_name`, `request.operation_translation_mode`, and `request.upstream_request_path` describe the provider-facing operation selected for the attempt. Native attempts use `operation_translation_mode = "none"` and usually keep ingress and upstream fields equal; translated attempts keep canonical usage from the upstream payload while presenting the client-visible operation in `operation_name`.

Ordinary target-graph attempts preserve the top-level requested and resolved model fields without rewriting client-visible response-body identity. Request-log detail exposes resolved model and selected Terminal Target attribution.

Response `404`: returned when the request ID is missing or out of scope for the effective profile.

Stream telemetry values are stable strings. `stream_outcome` is one of `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown`. `stream_error_kind` is nullable and, when present, is one of `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event`. `stream_error_detail` appears only on exact request-log detail responses; it is sanitized diagnostic text, not provider content, headers, or secrets.

The request-log sheet consumes this grouped detail contract as overview-only data. Linked audit payload resolution is isolated to the dedicated `/observe/requests/{request_id}/audit` page, which uses `request_log_id` plus a UTC window derived from `summary.created_at`. The derived frontend window is `created_at` minus 12 hours through `created_at` plus 12 hours, serialized explicitly as canonical audit `from` and `to` query parameters.

The request-history, spending, throughput, usage-snapshot, model-metrics, connection-success-rate, and dashboard aggregate APIs in this section remain product-facing PostgreSQL-backed surfaces. Prism no longer starts metrics or tracing exporters and does not surface a backend-local `/metrics` compatibility route.

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

### 4.5 Model Metrics (Batch)
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

### 4.8 Global Log Retention Settings
```
GET /api/settings/log-retention
PUT /api/settings/log-retention
```

These routes are global and do not use `X-Profile-Id`. They store the instance-wide normal retention policy. Request-log, audit-log, statistics, and load-balance list/detail APIs are pinned to Default profile id `1`, but retention settings do not.

Request and response fields:
| Field | Type | Description |
|---|---|---|
| `request_logs_retention_days` | integer or null | Global request-log retention days. `null` disables the stored policy. |
| `audit_logs_retention_days` | integer or null | Global audit-log retention days. `null` disables the stored policy. |
| `statistics_retention_days` | integer or null | Global `usage_request_events` retention days. `null` disables the stored policy. |
| `loadbalance_events_retention_days` | integer or null | Global load-balance event retention days. `null` disables the stored policy. |

Every non-null retention-day value must be `>= 1`; the database enforces this lower bound for all four fields.

Response `200`:
```json
{
  "request_logs_retention_days": 30,
  "audit_logs_retention_days": 90,
  "statistics_retention_days": 90,
  "loadbalance_events_retention_days": 30
}
```

### 4.8A Create Global Log Retention Job
```
POST /api/maintenance/log-retention/jobs
```
Headers:
| Header | Required | Description |
|---|---|---|
| `Idempotency-Key` | no | Optional stable client key for this global retention job |

Request:
```json
{
  "table": "request_logs",
  "cutoff": "2026-04-01T00:00:00Z",
  "delete_all": false,
  "reason": "operator retention cleanup"
}
```

Allowed `table` values are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`. Provide either `cutoff` or `delete_all=true`. If both are omitted, Prism computes `cutoff` from the stored global policy for that table; if no policy exists, it returns `400`.

Response `202`:
```json
{
  "job_id": "job_0123456789abcdef01234567",
  "state": "queued",
  "status_url": "/api/management/jobs/job_0123456789abcdef01234567",
  "scope": {
    "table": "request_logs",
    "cutoff": "2026-04-01T00:00:00Z",
    "delete_all": false
  }
}
```

The response sets `Location` to the same job status URL. The job type is `log_retention`, uses `profile_id = 0`, and applies across the instance.

Retention drops whole daily child partitions whose upper bound is `<= cutoff`. Only the single child partition that overlaps the cutoff receives a bounded row delete, followed by `VACUUM (ANALYZE, PROCESS_TOAST TRUE)` on that boundary child.

Audit rows keep weak request metadata. They retain `request_log_id`, `request_log_created_at`, and `ingress_request_id` when known, but request detail links can be missing after request-log retention expires first.

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
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset |
| `top_n` | integer | 5 | Number of top spenders to return; must be positive |

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
      "model_label": "GPT 4o",
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
    "STREAM_USAGE_UNAVAILABLE": 12,
    "MISSING_TOKEN_USAGE": 8
  },
  "report_currency_code": "USD",
  "report_currency_symbol": "$"
}
```

`top_spending_models` rows carry both the stable `model_id` and the resolved `model_label` used for operator-facing displays. `model_label` reflects the current canonical model configuration label when one exists and otherwise falls back to `model_id`.

Spending summaries aggregate canonical disjoint token components independently. `total_input_tokens` is base input only, `total_output_tokens` is base output only, `total_cache_read_input_tokens`, `total_cache_creation_input_tokens`, and `total_reasoning_tokens` are separate split totals, and `total_tokens` uses provider totals when available.

Unpriced reasons distinguish pricing configuration gaps from observed usage gaps. `MISSING_PRICE_DATA` means the pricing template or pricing snapshot is absent, unusable, or invalid, or required FX data was missing or invalid. Explicit `"0"` prices mean configured free pricing and do not trigger `MISSING_PRICE_DATA`. `MISSING_TOKEN_USAGE` means a completed stream or non-stream response lacked required upstream token usage. `STREAM_USAGE_UNAVAILABLE` means a classified stream outcome made terminal usage unavailable and required tokens were absent. Prism doesn't estimate tokens or cost for usage gaps.

---

## 5. Audit API

Audit APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

### 5.0 API-Family Audit Settings
```
GET /api/settings/audit
PUT /api/settings/audit
```

`/api/settings/audit` is pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored. `GET` returns exactly three rows in stable order: `openai`, `anthropic`, `gemini`. Missing persisted rows default to `audit_enabled=false` and `audit_capture_bodies=false`.

Response `200`:
```json
{
  "profile_id": 1,
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

`PUT` is a full replacement. The request must contain exactly one row for each supported family. Unknown families, duplicates, missing families, and `audit_enabled=false` with `audit_capture_bodies=true` reject before persistence.

Request:
```json
{
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": true },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

Body capture is valid only when audit is enabled for that API family. Runtime loads the selected policy by profile and model `api_family` into request planning snapshots, then persists the request-time booleans in existing `audit_enabled_at_request` and `audit_capture_bodies_at_request` fields.

### 5.1 List Audit Logs
```
GET /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `request_log_id` | integer | none | Filter audit rows linked to one request log |
| `model_id` | string | none | Filter by model ID |
| `status_code` | integer | none | Filter by response status code |
| `endpoint_id` | integer | none | Filter by endpoint ID |
| `connection_id` | integer | none | Filter by connection ID |
| `from` | datetime | required | Inclusive start of bounded time range (RFC 3339) |
| `to` | datetime | required | Exclusive end of bounded time range (RFC 3339) |
| `limit` | integer | 50 | Max positive result count |
| `cursor` | string | none | Opaque keyset cursor returned as `next_cursor` |
| `sort` | string | `desc` | Only `desc` is supported |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row. The `from` and `to` window is required and may not exceed 7 days, including when `request_log_id` is supplied. The backend has no fallback, default audit window, or legacy time-window aliases for request-log lookups. Unsupported query keys return `400` with `audit_filter_unsupported`.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 1,
      "request_log_id": 42,
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
  "next_cursor": "eyJ2IjoxLCJsYXN0X2NyZWF0ZWRfYXQiOiIyMDI1LTAxLTE1VDEwOjMwOjAwWiIsImxhc3RfaWQiOjF9.signature",
  "has_more": true,
  "window": {
    "from": "2025-01-15T00:00:00Z",
    "to": "2025-01-16T00:00:00Z"
  },
  "limit": 50,
  "sort": "desc"
}
```

The list API returns `request_body_preview` (first 200 characters of the request body) instead of the full body. Use the detail API for full content.
If body capture was off at request time, `request_body_preview` is `null` even though the audit metadata still exists. `response_body_stored` means captured response bytes were stored, independent of `is_stream`; rows with `response_body_stored=false` have no stored response body. Rows whose `request_log_id` is `null` are orphaned audit rows from deleted request logs, and they remain visible in the audit APIs.
Rows are ordered by `(created_at DESC, id DESC)`. Pagination is keyset-based: when `has_more=true`, pass the returned `next_cursor` with the same window, sort, and filters. The audit list response does not include `total` or `offset`.

### 5.2 Get Audit Log Detail
```
GET /api/audit/logs/{id}
```
Response `200`:
```json
{
  "id": 1,
  "profile_id": 1,
  "request_log_id": 42,
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

When body capture is enabled and non-empty response bytes are captured, `response_body` stores those bytes and `response_body_stored=true`; `is_stream` does not prevent storage. For translated OpenAI sibling-operation attempts, `request_body` and `response_body` remain raw upstream-native payloads or SSE bytes. They do not store the translated client-facing request or response shape.
If body capture is disabled for a request, both `request_body` and `response_body` are `null`. Rows with `response_body_stored=false` have no stored response body, including old rows that were written before streaming response capture was available.

Response `404`: Audit log not found.

### 5.3 Audit Log Retention

Audit log list and detail APIs remain pinned to Default profile id `1`. Normal audit-log cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "audit_logs"`, or the stored `audit_logs_retention_days` value from `/api/settings/log-retention`.

The retired audit cleanup endpoints are not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

Audit rows retain weak request metadata in `request_log_id`, `request_log_created_at`, and `ingress_request_id`. A request detail link can be missing after request-log retention expires before audit-log retention.

### 5.3A Management Job Status and Cancel
```
GET /api/management/jobs
GET /api/management/jobs/{job_id}
POST /api/management/jobs/{job_id}/cancel
```

`GET /api/management/jobs` returns recent Default-profile jobs only. Audit-delete jobs are profile-scoped. Global log-retention jobs use `profile_id = 0`; they are not included in the list response, but the `status_url` returned by `POST /api/maintenance/log-retention/jobs` can be used to read them by ID through `GET /api/management/jobs/{job_id}`:
```json
{
  "items": [
    {
      "id": "job_0123456789abcdef01234567",
      "type": "audit_delete",
      "state": "queued",
      "requested_by": "profile:2",
      "requested_at": "2026-04-19T12:00:00Z",
      "started_at": null,
      "finished_at": null,
      "scope": { "before": "2025-01-01T00:00:00Z" },
      "reason": "operator retention cleanup",
      "progress": {
        "rows_matched_estimate": 0,
        "rows_deleted": 0,
        "batches_completed": 0,
        "last_cursor": ""
      },
      "attempt_count": 0,
      "last_heartbeat_at": null,
      "cancel_requested": false,
      "error_code": null,
      "error_message": null
    }
  ],
  "has_more": false,
  "next_cursor": null
}
```

`GET /api/management/jobs/{job_id}` first resolves the Default-profile job and then falls back to a global log-retention job with the same id. `POST /api/management/jobs/{job_id}/cancel` follows the same resolution order, marks the resolved job for cancellation, and returns `202` with the job object. Unknown or out-of-scope jobs return `404`.

### 5.4 Redaction Rules

All audit log entries have sensitive header values redacted before storage:
- `authorization` → `Bearer [REDACTED]`
- `x-api-key` → `[REDACTED]`
- `x-goog-api-key` → `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, or `auth` (case-insensitive) → value replaced with `[REDACTED]`

Request and response bodies are not header-redacted and may contain user-provided secrets or PII. Body capture is request-time provenance via `audit_capture_bodies_at_request`; when disabled, both `request_body` and `response_body` are `null`. For translated OpenAI attempts, stored bodies remain upstream-native because provider conversion owns request, response, and stream shape conversion while runtime owns audit storage.

### 5.5 Body Size Limits

When body capture is enabled for the request, Prism stores the captured request and response body strings for the upstream attempt. Current storage does not define a documented truncation marker.

---

## 6. Loadbalance API

Loadbalance APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

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

This endpoint is pinned to Default profile id `1` and creates the canonical explicit Ban Policy defaults for that profile only: `Default single routing`, `Default fill-first routing`, and `Default round-robin routing`.

Response `200`:
```json
{
  "items": [
    {
      "id": 12,
      "profile_id": 1,
      "name": "Default fill-first routing",
      "legacy_strategy_type": "fill-first",
      "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
      "ban_mode": "off",
      "retry_base_delay_ms": 60000,
      "retry_backoff_multiplier": 2.0,
      "retry_jitter_ratio": 0.2,
      "retry_max_delay_ms": 900000,
      "cycle_retry_attempt_limit": 3,
      "ban_cumulative_retry_attempt_threshold": 0,
      "ban_duration_seconds": 0
    }
  ],
  "created_count": 1,
  "created_names": ["Default fill-first routing"],
  "existing_names": ["Default single routing"]
}
```

The response includes the full current strategy list in `items` plus creation metadata so the caller can tell which canonical rows were created versus already present.

Returns `409` when one or more canonical default names are already occupied by non-canonical strategies in Default profile id `1`. In that case, the error payload includes `code` plus `detail.conflicting_names` with the conflicting names.

### 6.3 Create Loadbalance Strategy
```
POST /api/loadbalance/strategies
```
Request:
```json
{
  "name": "round-robin-primary",
  "legacy_strategy_type": "round-robin",
  "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
  "ban_mode": "temporary",
  "retry_base_delay_ms": 45000,
  "retry_backoff_multiplier": 3.5,
  "retry_jitter_ratio": 0.2,
  "retry_max_delay_ms": 720000,
  "cycle_retry_attempt_limit": 3,
  "ban_cumulative_retry_attempt_threshold": 6,
  "ban_duration_seconds": 1800
}
```
Response `201`: Created strategy object.

Validation rules:
- `name` must be unique within the effective profile scope.
- `legacy_strategy_type` must be `single`, `fill-first`, or `round-robin`.
- `failure_status_codes` values must be unique valid HTTP status integers (`100..599`); the backend sorts them before persistence and response serialization.
- Retry-window delay, backoff, jitter, max delay, and cycle retry attempt limit must stay within backend bounds.
- `cycle_retry_attempt_limit` is optional; omitted create/update payloads default it to `3`. When provided, it must be from `1` to `50`.
- `ban_mode` is `off`, `temporary`, or `until_reset`.
- `ban_mode = "off"` requires `ban_cumulative_retry_attempt_threshold = 0` and `ban_duration_seconds = 0`.
- `ban_mode = "temporary"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds` from `1` to `86400`.
- `ban_mode = "until_reset"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds = 0`.
- Runtime retry-cycle exhaustion is inclusive: `cycle_retry_attempts >= cycle_retry_attempt_limit` schedules the retry-window transition.
- Runtime banning is inclusive and explicit: `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`. Prism never derives the ban threshold from `cycle_retry_attempt_limit`.
- Upstream request timing is controlled by the shared backend timeout settings rather than per-strategy fields.

### 6.4 Get Loadbalance Strategy
```
GET /api/loadbalance/strategies/{strategy_id}
```
Response `200`: Strategy object in the effective profile scope.
Returns `404` when the strategy does not exist in the effective profile.

### 6.5 Update Loadbalance Strategy
```
PUT /api/loadbalance/strategies/{strategy_id}
```
Request: Full replacement of mutable strategy fields using the same shape as create.
Response `200`: Updated strategy object.

Strategy responses include the persisted explicit Ban Policy strategy document:

```json
{
  "id": 12,
  "profile_id": 1,
  "name": "round-robin-primary",
  "legacy_strategy_type": "round-robin",
  "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
  "ban_mode": "temporary",
  "retry_base_delay_ms": 45000,
  "retry_backoff_multiplier": 3.5,
  "retry_jitter_ratio": 0.2,
  "retry_max_delay_ms": 720000,
  "cycle_retry_attempt_limit": 3,
  "ban_cumulative_retry_attempt_threshold": 6,
  "ban_duration_seconds": 1800,
  "attached_model_count": 2,
  "created_at": "2026-03-25T08:00:00Z",
  "updated_at": "2026-03-25T08:05:00Z"
}
```

### 6.6 Delete Loadbalance Strategy
```
DELETE /api/loadbalance/strategies/{strategy_id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the strategy is still attached to one or more models; the response detail includes `attached_model_count`.

### 6.7 List Current Loadbalance State for a Model
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
      "window_started_at": "2026-03-30T08:00:00Z",
      "window_request_count": 4,
      "in_flight_non_stream": 1,
      "in_flight_stream": 0,
      "cycle_retry_attempts": 2,
      "cumulative_retry_attempts": 5,
      "next_retry_at": "2026-03-30T08:02:00Z",
      "last_retry_delay_ms": 60000,
      "ban_mode": "temporary",
      "banned_until_at": "2026-03-30T08:30:00Z",
      "last_failure_kind": "transient_http",
      "last_success_at": null,
      "live_p95_latency_ms": 540,
      "state": "banned",
      "created_at": "2026-03-30T08:00:00Z",
      "updated_at": "2026-03-30T08:01:00Z"
    }
  ]
}
```

Returns `404` when the model config does not exist in the effective profile.

`state` is derived from the connection-global Ban Policy runtime state and is one of `available`, `retry_wait`, or `banned`. `until_reset` bans stay `banned` until the current-state reset endpoint clears them; temporary bans stay `banned` until `banned_until_at`; retry windows stay `retry_wait` until `next_retry_at`; otherwise the connection is `available`. Current-state items expose QPS and in-flight admission counters plus live retry-cycle counters for each private connection directly owned by the model. They intentionally omit `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` because current state is connection-global, while policy thresholds belong to the model strategy snapshot recorded on events.

### 6.8 Reset Current Loadbalance State for a Connection
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
Reset clears retry-window counters, next retry timing, ban state, and the related round-robin cursor for an attached model when one exists.

### 6.9 List Loadbalance Events
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
      "profile_id": 1,
      "connection_id": 1,
      "event_type": "banned",
      "failure_kind": "transient_http",
      "cycle_retry_attempts": 2,
      "cumulative_retry_attempts": 6,
      "cycle_retry_attempt_limit": 3,
      "ban_cumulative_retry_attempt_threshold": 6,
      "next_retry_at": "2026-03-30T08:02:00Z",
      "last_retry_delay_ms": 60000,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "ban_mode": "until_reset",
      "banned_until_at": null,
      "last_success_at": null,
      "summary": {
        "event": "Connection was banned",
        "reason": "The retryable HTTP failure pushed cumulative retry attempts to 6, meeting the configured cumulative ban threshold of 6 attempts.",
        "operation": "Prism removed this connection globally until the ban expires or an operator resets it.",
        "cooldown": "1 minute"
      },
      "created_at": "2026-03-30T08:01:00Z"
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0
}
```

Loadbalance event types are `retry_scheduled`, `retry_exhausted`, `banned`, `unbanned`, `recovered`, and `admission_rejected`. They record retry-cycle attempts, cumulative attempts, next retry timing, last retry delay, optional ban metadata, optional success time, and the model, endpoint, and vendor snapshots for operator review. Events produced by Ban Policy evaluation also expose immutable policy snapshots as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold`, so historical event detail explains the threshold that was active when the event was written.

### 6.10 List Loadbalance Incidents
```
GET /api/loadbalance/incidents
```

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | integer | 50 | Max recent incident events |
| `since_hours` | integer | 24 | Recent event lookback window |

Response `200`:
```json
{
  "active_bans": [
    {
      "connection_id": 3,
      "ban_mode": "temporary",
      "banned_until_at": "2026-03-30T08:30:00Z",
      "cumulative_retry_attempts": 7,
      "next_retry_at": null,
      "state": "banned",
      "created_at": "2026-03-30T08:00:00Z",
      "updated_at": "2026-03-30T08:01:00Z"
    }
  ],
  "recent_events": [
    {
      "id": 22,
      "profile_id": 1,
      "connection_id": 3,
      "event_type": "banned",
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "summary": {
        "event": "Connection was banned",
        "reason": "The retryable HTTP failure pushed cumulative retry attempts to 7.",
        "operation": "Prism removed this model-private connection from routing until the ban expires or an operator resets it.",
        "cooldown": "1 minute"
      },
      "created_at": "2026-03-30T08:01:00Z"
    }
  ],
  "generated_at": "2026-03-30T08:05:00Z"
}
```

`active_bans` is the current Ban Policy runtime state for the effective profile. `recent_events` uses the loadbalance event item shape and includes recent `banned`, `unbanned`, `recovered`, and `retry_exhausted` rows without requiring a `model_id` filter.

### 6.11 Get Loadbalance Event Detail
```
GET /api/loadbalance/events/{id}
```
Response `200`: Single event object with the same Ban Policy retry-window metadata, policy snapshot fields, and summary fields as the list item.

### 6.12 Loadbalance Event Retention

Loadbalance event list and detail APIs remain pinned to Default profile id `1`. Normal cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "loadbalance_events"`, or the stored `loadbalance_events_retention_days` value from `/api/settings/log-retention`.

The retired load-balance cleanup endpoint is not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

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
Response `200`:
```json
{
  "authenticated": false,
  "auth_enabled": true,
  "username": null
}
```

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
```json
{
  "authenticated": true,
  "auth_enabled": true,
  "username": "admin"
}
```

### 7.4 Logout
```
POST /api/auth/logout
```
Clears session cookies and revokes the current refresh token.
Response `200`:
```json
{
  "authenticated": false,
  "auth_enabled": true,
  "username": null
}
```

### 7.5 Refresh Session
```
POST /api/auth/refresh
```
Uses the `refresh_token` cookie to issue a new session. Implements token family rotation. Response `200` uses the same session object shape as login.

### 7.6 Get Session
```
GET /api/auth/session
```
Returns the current authenticated session state.
Response `200`:
```json
{
  "authenticated": true,
  "auth_enabled": true,
  "username": "admin"
}
```

---

## 8. Error Responses

Resource-scope errors follow this format:
```json
{
  "code": "profile_scope_profile_not_found",
  "detail": "Profile 1 not found"
}
```

| Status Code | Meaning |
|---|---|
| 400 | Bad request (invalid input) |
| 404 | Resource not found |
| 409 | Conflict (duplicate scoped identifier) |
| 502 | Upstream service error |
| 503 | No active Terminal Targets available |

---

## 10. API Reference Source

This markdown document is the source of truth for current runtime and management API semantics.
