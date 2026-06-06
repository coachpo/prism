# API Specification: Prism

Local `./start.sh` backend base URL follows the selected bootstrap file's `server.port`; with the checked-in `config.json`, that is `http://localhost:18000`

Container and custom deployments use the listener configured in the plaintext bootstrap file; the manual Docker examples commonly publish `http://localhost:8000`.

Prism does not expose a backend-local `/metrics` operations endpoint. Configure OTLP metrics and traces in startup JSON, send them to an OpenTelemetry Collector or Grafana Alloy, and connect Prometheus/Grafana/Tempo or another backend from that collector layer. The retained `/api/stats/*` routes remain product-facing request-history and aggregate APIs.

## 0. Profile Context Semantics
- Prism has three route classes:
  - Global management routes, which omit `X-Profile-Id`.
  - Profile-scoped management routes, which require `X-Profile-Id` and resolve against the selected profile.
  - Runtime proxy routes, which always use the active profile and ignore management scope overrides.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always use the active profile and ignore management scope overrides.
- Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/auth/*`, `/api/realtime/*`, `/api/sidecars/*`, `/api/settings/auth*`, `/api/config/vendors/*`, `/api/config/bootstrap`, and `/api/config/bootstrap/validate`. `POST /api/config/profile/import/preview` is profile-scoped and requires `X-Profile-Id`.
- Profile-scoped management routes include `/api/config/profile/import`, `/api/settings/costing`, `/api/settings/timezone`, `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, `/api/models/*`, `/api/endpoints/*`, `/api/connections/*`, and the other non-global `/api/config/profile/*` routes.
- Detail endpoints return `404` when a resource exists in another profile but not in the effective profile context.
- Scope-control failures return structured JSON with `code` and `detail`, where `code` is stable for machine handling and `detail` is safe to show to operators.


## 1. Management API (`/api/*`)

### 1.0 Bootstrap Config

The startup bootstrap contract is a plaintext `config.json` management surface. It is not a PostgreSQL-backed settings bundle. Backend-owned canonical defaults are the source of truth for freshly seeded files, including disabled telemetry and a standalone database URL on port `5432` unless `DATABASE_URL` is set; the root launcher sets `DATABASE_URL` to the local PostgreSQL DSN on host port `15432` before local seeding. The entrypoint has a narrow repair path for stale files rejected only because they still contain the retired `docsEnabled` field; other invalid legacy shapes fail validation. API-managed writes update the file and immediately apply fields that are marked `hot_apply`; structural fields, including every telemetry exporter/metrics/tracing field, are durable for the next Prism start. Existing valid files are preserved until the operator resets manually by stopping Prism, removing or relocating the bootstrap file, and restarting.

#### Get Bootstrap Config
```
GET /api/config/bootstrap
```

Response `200` returns safe metadata only. Raw secret values never appear in the payload. Safe bootstrap API values use snake_case for runtime fields, so the raw `runtime.transport.requestTimeout` file setting appears as `runtime.transport.request_timeout` in API payloads, and raw `runtime.sideEffects.attemptTimeout` appears as `runtime.side_effects.attempt_timeout`.

GET is a read of the managed file plus the live applied baseline. Current responses always include `apply_capabilities`. They include `apply_result` only when file values differ from the live applied baseline, such as after a failed hot apply or an external file edit that changes a hot field. Current no-drift responses omit `planned_changes` and `apply_result`. `restart_required` is true only when the file contains restart-required drift from the live baseline. External edits to `config.json` are not watched automatically; operators must use the Startup tab or API PUT to publish hot-eligible file edits into the running process.
```json
{
  "config_path": "config.json",
  "schema_version": 1,
  "file_revision": 12,
  "loaded_revision": 12,
  "document_etag": "sha256:abc123",
  "loaded_document_etag": "sha256:abc123",
  "created_at": "2026-04-28T00:00:00Z",
  "updated_at": "2026-04-28T00:00:00Z",
  "restart_required": false,
  "writable": true,
  "apply_capabilities": {
    "http.cors_allowed_origins": { "mode": "hot_apply" },
    "runtime.transport.request_timeout": { "mode": "hot_apply" },
    "runtime.side_effects.attempt_timeout": { "mode": "restart_required" },
    "server.port": {
      "mode": "restart_required",
      "confirmation_token": "server-port-change"
    },
    "database.url": {
      "mode": "restart_required",
      "confirmation_token": "database-url-change"
    }
  },
  "values": {
    "server": {
      "host": "0.0.0.0",
      "port": 8000
    },
    "runtime": {
      "transport": {
        "max_idle_conns": 100,
        "max_idle_conns_per_host": 16,
        "max_conns_per_host": 16,
        "request_timeout": "300s",
        "idle_conn_timeout": "90s",
        "response_header_timeout": "0s",
        "tls_handshake_timeout": "10s",
        "expect_continue_timeout": "1s"
      },
      "side_effects": {
        "attempt_timeout": "10s"
      }
    },
    "mail": {
      "enabled": false,
      "from": null,
      "reply_to": null,
      "smtp": null
    },
    "telemetry": {
      "enabled": false
    }
  },
  "secrets": {
    "database.url": {
      "configured": true,
      "editable": true,
      "masked": "postgres://prism:***@localhost:15432/prism?sslmode=disable"
    },
    "runtime.secretEncryptionKey": {
      "configured": true,
      "editable": false,
      "masked": "preserve-only"
    },
    "mail.smtp.password": {
      "configured": false,
      "editable": true,
      "masked": ""
    },
    "telemetry.exporter.auth.authorizationHeader": {
      "configured": false,
      "editable": true,
      "masked": ""
    }
  }
}
```

The underlying `config.json` file must include raw `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout` as Go duration strings. Fresh seeds set them to `"300s"` and `"10s"`. Missing either required field fails validation and startup by design. `runtime.transport.requestTimeout` remains the whole-request upstream provider HTTP timeout and is hot-applicable through PUT. `runtime.sideEffects.attemptTimeout` is the per-attempt background side-effect enqueue budget, is restart-required, and is not hot-applied. Runtime buffering is automatic and not user-configurable. The same bootstrap contract also owns the temporary restart-required rollout controls `runtime.routing.plannerMode` (`legacy`, `shadow`, `enforced`) and `runtime.routing.openaiTerminalTranslationMode` (`off`, `safe_only`). `legacy` + `off` is the rollback position; `shadow` keeps serving through the legacy resolver while the compiled planner runs in parallel and persists only compact mismatch summaries when the two outcomes diverge.

Raw runtime startup config uses camelCase JSON field names in the file:

```json
{
  "runtime": {
    "transport": {
      "requestTimeout": "300s",
      "idleConnTimeout": "90s",
      "responseHeaderTimeout": "0s",
      "tlsHandshakeTimeout": "10s",
      "expectContinueTimeout": "1s"
    },
    "sideEffects": {
      "attemptTimeout": "10s"
    }
  }
}
```

The underlying `config.json` file may also include an optional top-level `telemetry` block. Missing telemetry and `telemetry.enabled=false` both mean disabled no-op OpenTelemetry export. Enabled telemetry requires exporter endpoint, protocol (`grpc` or `http/protobuf`), compression (`none` or `gzip`), timeout, auth mode (`none` or `authorization_header`), TLS values, metrics enabled, traces enabled, and traces sampling ratio. These fields are restart-required: PUT persists them, but Prism rebuilds providers only on restart. `telemetry.exporter.auth.authorizationHeader` is secret-managed and appears only in `secrets` metadata plus `secret_updates`.

Raw telemetry startup config uses camelCase JSON field names in the file:

```json
{
  "telemetry": {
    "enabled": true,
    "exporter": {
      "endpoint": "http://otel-collector:4318",
      "protocol": "http/protobuf",
      "compression": "gzip",
      "timeout": "10s",
      "auth": {
        "mode": "authorization_header",
        "authorizationHeader": "Bearer collector-token"
      },
      "tls": {
        "insecureSkipVerify": false,
        "caFile": "/etc/prism/otel-ca.pem"
      }
    },
    "metrics": {
      "enabled": true
    },
    "traces": {
      "enabled": true,
      "samplingRatio": 1
    }
  }
}
```

Use the startup JSON as Prism's steady-state telemetry source. `OTEL_*` environment variables are not Prism's supported long-term configuration path, and Prism does not expose a backend-local `/metrics` scrape endpoint. Export OTLP to Collector or Alloy and attach Prometheus/Grafana/Tempo from there.

The underlying `config.json` file may also include an optional top-level `mail` block. Missing `mail` and `mail.enabled=false` both mean disabled no-op auth email delivery with no SMTP network activity. Seeded configs use `{ "mail": { "enabled": false } }`.

Enabled SMTP startup config uses camelCase JSON field names in the file:

```json
{
  "mail": {
    "enabled": true,
    "from": "Prism <noreply@example.com>",
    "replyTo": "support@example.com",
    "smtp": {
      "host": "smtp.example.com",
      "port": 587,
      "mode": "starttls_required",
      "ehloHostname": "prism.example.com",
      "auth": "plain",
      "username": "smtp-user",
      "passwordFile": "/run/secrets/prism-smtp-password",
      "timeout": "15s",
      "tlsServerName": "smtp.example.com"
    }
  }
}
```

Supported `mail.smtp.mode` values are `starttls_required`, `implicit_tls`, and `plaintext_local_only`. `plaintext_local_only` is valid only for localhost or loopback SMTP hosts, and auth over non-local plaintext is forbidden. `mail.smtp.auth` accepts `none` or `plain`; `plain` requires `mail.smtp.username` plus exactly one of `mail.smtp.password` or `mail.smtp.passwordFile`. `mail.smtp.timeout` must parse as a Go duration such as `15s`.

Safe bootstrap API values omit plaintext secrets and use snake_case for API fields, such as runtime `request_timeout`, `side_effects.attempt_timeout`, mail `reply_to`, `ehlo_hostname`, `password_file`, `tls_server_name`, telemetry `sampling_ratio`, `insecure_skip_verify`, and `ca_file`. `mail.smtp.password` and `telemetry.exporter.auth.authorizationHeader` appear only in `secrets` metadata and in `secret_updates`. To keep the current secret, send a `preserve` action. To change it, send a `replace` action with a non-placeholder value. Safe GET and validate responses never return the password or telemetry authorization-header value.

The durable field registry is exposed through `apply_capabilities`. Hot-apply fields are `http.cors_allowed_origins`; auth TTL and cookie metadata fields `auth.access_token_ttl_seconds`, `auth.refresh_token_ttl_seconds`, `auth.reset_code_ttl_seconds`, `auth.access_cookie_name`, `auth.refresh_cookie_name`, and `auth.cookie_secure`; mail fields `mail.enabled`, `mail.from`, `mail.reply_to`, `mail.smtp.host`, `mail.smtp.port`, `mail.smtp.mode`, `mail.smtp.ehlo_hostname`, `mail.smtp.auth`, `mail.smtp.username`, `mail.smtp.password`, `mail.smtp.password_file`, `mail.smtp.timeout`, and `mail.smtp.tls_server_name`; runtime fields `runtime.transport.max_idle_conns`, `runtime.transport.max_idle_conns_per_host`, `runtime.transport.max_conns_per_host`, `runtime.transport.idle_conn_timeout`, `runtime.transport.request_timeout`, `runtime.transport.response_header_timeout`, `runtime.transport.tls_handshake_timeout`, and `runtime.transport.expect_continue_timeout`; and management admission fields `database.management_admission.m2_max_concurrent` and `database.management_admission.m3_max_concurrent`. `runtime.side_effects.attempt_timeout` and all `telemetry.*` fields are intentionally absent from hot-apply fields.

Restart-required fields are listener fields `server.host` and `server.port`; `database.url`; PostgreSQL pool fields `database.pools.total_max_conns`, `database.pools.management.max_conns`, `database.pools.management.min_idle_conns`, `database.pools.runtime_execution.max_conns`, `database.pools.runtime_execution.min_idle_conns`, `database.pools.runtime_telemetry.max_conns`, `database.pools.runtime_telemetry.min_idle_conns`, `database.pools.runtime_feedback.max_conns`, `database.pools.runtime_feedback.min_idle_conns`, `database.pools.realtime.max_conns`, `database.pools.realtime.min_idle_conns`, `database.pools.cache_refresh.max_conns`, `database.pools.cache_refresh.min_idle_conns`, `database.pools.background_jobs.max_conns`, and `database.pools.background_jobs.min_idle_conns`; runtime field `runtime.side_effects.attempt_timeout`; telemetry fields `telemetry.enabled`, `telemetry.exporter.endpoint`, `telemetry.exporter.protocol`, `telemetry.exporter.compression`, `telemetry.exporter.timeout`, `telemetry.exporter.auth.mode`, `telemetry.exporter.auth.authorizationHeader`, `telemetry.exporter.tls.insecure_skip_verify`, `telemetry.exporter.tls.ca_file`, `telemetry.metrics.enabled`, `telemetry.traces.enabled`, and `telemetry.traces.sampling_ratio`; and secret fields `runtime.secretEncryptionKey`, `auth.jwtSigningKey`, and `stateTransfer.bundleEncryptionKey`. Confirmation tokens are required for `server.host`, `server.port`, `database.url`, `auth.jwtSigningKey`, and `stateTransfer.bundleEncryptionKey` changes. There is no hot apply for listener, database URL, pool budgets, telemetry provider settings, `runtime.side_effects.attempt_timeout`, JWT signing keys, state-transfer bundle keys, or the runtime secret encryption key.

#### Validate Bootstrap Config
```
POST /api/config/bootstrap/validate
```

Request bodies follow the same shape as PUT, including required non-zero `expected_revision`, required non-empty `expected_etag`, `values`, `secret_updates`, and optional `confirmations`. Validation checks secret actions, confirmation requirements, field classification, and exact revision/etag concurrency before any file write. It returns the safe response shape with `apply_capabilities` plus `planned_changes`, where `changed_fields[]` contains each changed field and its `mode`. Validate does not write `config.json`, does not publish hot settings, and does not alter the live applied baseline.

#### Update Bootstrap Config
```
PUT /api/config/bootstrap
```

PUT prepares and validates the requested file, validates hot runtime resources before writing, writes `config.json`, then publishes changed hot fields. A successful write returns the same safe response shape as GET, with `apply_capabilities` and `apply_result`. Updates require both the non-zero `expected_revision` and non-empty `expected_etag` to match the current file metadata, secret fields use explicit `preserve` or `replace` actions, redacted placeholders are not persisted, and dangerous changes require confirmation tokens for:
- `server-host-change`
- `server-port-change`
- `database-url-change`
- `auth-jwt-signing-key-change`
- `state-transfer-bundle-encryption-key-change`

`apply_result.applied_now_fields[]` lists hot fields published to the running process during this PUT. `restart_required_fields[]` lists changed structural fields that remain file-durable until restart. `pending_hot_apply_fields[]` lists hot fields written to the file but not yet applied to the live baseline. `failed_hot_apply_fields[]` lists hot fields whose publish step failed. `unchanged_fields[]` lists registered fields that did not change. Top-level `restart_required` is true when restart-required fields differ from the live applied baseline.

`runtime.secretEncryptionKey` is preserve-only in v1.

`mail.smtp.password` is editable through the same secret update map:

```json
{
  "secret_updates": {
    "mail.smtp.password": {
      "action": "replace",
      "value": "new-smtp-password"
    }
  }
}
```

`telemetry.exporter.auth.authorizationHeader` is also secret-managed. Replace it when `telemetry.exporter.auth.mode` is `authorization_header`, preserve it to keep the current collector credential, or clear it when switching auth mode to `none`.

```json
{
  "secret_updates": {
    "telemetry.exporter.auth.authorizationHeader": {
      "action": "replace",
      "value": "Bearer collector-token"
    }
  }
}
```

If hot publish fails after the file write, the response is HTTP `500` and still uses the bootstrap response shape with top-level `apply_result` plus `detail.message = "Failed to apply bootstrap config"` and `detail.failed_hot_apply_fields[]`. The file remains written. Failed hot fields stay pending against the live applied baseline, so a later GET shows `apply_result.pending_hot_apply_fields[]` and a later PUT can retry the publish. Removing `mail` or setting `mail.enabled=false` disables real delivery immediately when hot apply succeeds, or after restart if the change is still pending. If `mail.enabled=true` and SMTP config is incomplete or invalid, validation and startup fail with redacted errors rather than silently using no-op delivery.

### 1.1 Profiles
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
Response `200`: Full model object with nullable vendor metadata, required `api_family`, optional `loadbalance_strategy_id`, exact-facade fields (`facade_enabled`, `facade_selection_policy`, `facade_fallback_policy`), ordered `access_targets`, and attached private connection summaries in the effective profile scope. Public model target authoring uses only same-family model targets by exact `target_model_id`. Existing `target_type="connection"` rows are returned as internal ownership and runtime routing edges for the model's private connections. Model rows do not carry `icon_key`; that metadata stays on `vendors[]`. These backend model routes are the authoritative Release 1 facade authoring surface; frontend facade authoring remains deferred.

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
Response `200`: `items[]`, where each item contains an `endpoint_id` and the models that can reach that endpoint through terminal private connections. Endpoints are reusable and may be referenced by private connections owned by different models.

#### Get Models by Endpoint
```
GET /api/models/by-endpoint/{endpoint_id}
```
Response `200`: Array of models that can reach the endpoint through terminal private connections within the effective profile scope.

#### Create Model
```
POST /api/models
```
Request:
```json
{
  "vendor_id": null,
  "api_family": "openai",
  "model_id": "gpt-4o-public",
  "display_name": "GPT-4o Public",
  "context_window_tokens": 128000,
  "default_output_token_reserve": 4096,
  "max_context_utilization": 0.90,
  "preferred_context_utilization_threshold": 0.70,
  "loadbalance_strategy_id": 7,
  "facade_enabled": true,
  "facade_selection_policy": "weighted_eligible_context",
  "facade_fallback_policy": "redistribute_ineligible_weight",
  "context_overflow_promotion_target_id": "gpt-4o-large",
  "access_targets": [
    {
      "target_type": "model",
      "target_model_id": "gpt-4o-regional",
      "position": 0,
      "weight": 1,
      "target_priority": 0
    }
  ],
  "is_enabled": true
}
```
Response `201`: Created model object.

Validation rules:
- `model_id` must be unique within the effective profile scope.
- `api_family` is required on every model contract and remains the authoritative runtime compatibility field.
- `vendor_id` is optional metadata and may be `null`.
- `facade_enabled` defaults to `false`. When it is `true`, the model must use `api_family = "openai"`, `facade_selection_policy` must be exactly `"weighted_eligible_context"`, and `facade_fallback_policy` must be exactly `"redistribute_ineligible_weight"`.
- Release 1 facade authoring is exact model-ID only. The management API does not accept regex matcher payloads or capability-metadata facade fields.
- Public create and update payloads may author only ordered same-profile, same-`api_family` model targets by exact `target_model_id`.
- Submitted `target_type="connection"`, `connection_id`, or `target_connection_id` entries are rejected. Private connection rows are managed from model detail through model-scoped connection routes.
- Every public model target requires `target_model_id` and `position`; `weight` and `target_priority` are optional on input and default to `1` / `position` when omitted. Positions must stay contiguous starting at `0`; supplied `weight` values must be `>= 1`; supplied `target_priority` values must be `>= 0`.
- Context capability fields are validated on create and update. `default_output_token_reserve` defaults to `4096`, `max_context_utilization` defaults to `0.90`, utilization values must be greater than `0` and less than or equal to `1`, and reserve must be at least `1` when supplied. `preferred_context_utilization_threshold` is nullable; `null` means no preferred band, while a supplied value must be less than or equal to `max_context_utilization`.
- `context_overflow_promotion_target_id` is nullable. When set, it must name an enabled same-profile, same-`api_family`, non-facade model with a strictly larger effective usable context window. It must not point to the same model or to a model that resolves back to the same terminal target as the source.
- Nested facades are rejected at write time: public model targets cannot point at facade-enabled target models, and enabling `facade_enabled = true` on a model that already has inbound model-target referrers is rejected.
- Model target self-reference and target cycles are rejected.
- Deleting a model referenced by another model target returns `409` until the target rows are removed or updated. Deleting an owner model deletes its private connections with the owning target rows.

#### Update Model
```
PUT /api/models/{id}
```
Request (all fields optional):
```json
{
  "vendor_id": null,
  "api_family": "openai",
  "model_id": "gpt-4o-public-updated",
  "display_name": "GPT-4o Public (Updated)",
  "context_window_tokens": 256000,
  "default_output_token_reserve": 2048,
  "max_context_utilization": 0.85,
  "preferred_context_utilization_threshold": null,
  "loadbalance_strategy_id": 9,
  "facade_enabled": true,
  "facade_selection_policy": "weighted_eligible_context",
  "facade_fallback_policy": "redistribute_ineligible_weight",
  "context_overflow_promotion_target_id": "gpt-4o-large",
  "access_targets": [],
  "is_enabled": true
}
```
Update payloads use the same public model-target validation rules as create. Omitted private connection targets are preserved and remain managed by model-scoped connection routes. Omitted facade fields preserve the current stored values, but any effective `facade_enabled = true` state must still satisfy the exact OpenAI policy strings and nested-facade rejection rules after the update is applied. Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile. Returns `400` if access-target validation fails.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `200`: `{ "deleted": true }`. Returns `409` if other models still reference this model through model targets. When deletion succeeds, the owner model's private connection rows and their internal owning access-target rows are removed in the same operation.

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

### 1.4 Connections and Model Access Targets

Connections are model-private endpoint bindings within one profile. A connection carries its owner model's `api_family`, endpoint reference or inline endpoint create payload, health metadata, pricing template, and optional admission limits. Endpoints remain reusable, so many private connections may point at the same endpoint. `model_access_targets.target_type="connection"` is an internal ownership and runtime routing edge, not a public assignment surface for connection IDs.

#### List Connections
```
GET /api/connections
```
Response `200`: Array of private connection objects in the effective profile. This is a read surface. Public `/api/connections` mutation routes reject writes and direct operators to model detail.

#### Get Connection
```
GET /api/connections/{connection_id}
```
Response `200`: Single private connection object in the effective profile. Returns `404` when the connection does not exist in that profile.

#### List Connections Attached to Models
```
POST /api/models/connections/batch
```
Request:
```json
{
  "model_config_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains a `model_config_id` and the private connections owned by that model's enabled or disabled internal connection targets, ordered by target position.

#### Create Model-Private Connection
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
  "context_window_tokens": 128000,
  "default_output_token_reserve": 4096,
  "max_context_utilization": 0.90,
  "preferred_context_utilization_threshold": 0.70,
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
Response `201`: Created private connection object plus its owner routing edge for the model.

Create semantics:
- Exactly one of `endpoint_id` or `endpoint_create` is required.
- The connection `api_family` is derived from the owner model. A conflicting request value is rejected.
- `priority` is rejected with `422`; connection ordering for a model is owned by `/api/models/{model_config_id}/targets` positions.
- Limiter fields are optional. `null` means unlimited. Positive integers apply per-connection request admission limits.
- Context capability fields inherit the owner model's effective values when omitted or reset to `null`, so terminal-target rows persist explicit request-time capability values. `preferred_context_utilization_threshold` follows the same owner-scoped override shape; inherited and explicit values must stay less than or equal to the effective `max_context_utilization`, and `null` means the terminal target has no preferred band.
- `openai_probe_endpoint_variant` selects the lightweight OpenAI health-check target plus payload variant for OpenAI-family connections. Supported values are `responses_minimal` (default), `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`. The derived `openai_upstream_operation` capability is `openai.responses` for blank or `responses_*` variants and `openai.chat_completions` for `chat_completions_*` variants. For non-OpenAI families, providing this field is rejected and omitted values persist as `null`.

#### Update Model-Private Connection
```
PATCH /api/models/{model_config_id}/connections/{connection_id}
```
Request: Mutable connection metadata: `endpoint_id`, `endpoint_create`, `is_active`, `name`, `auth_type`, `custom_headers`, `openai_probe_endpoint_variant`, `context_window_tokens`, `default_output_token_reserve`, `max_context_utilization`, `preferred_context_utilization_threshold`, `pricing_template_id`, `qps_limit`, `max_in_flight_non_stream`, `max_in_flight_stream`.

`endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`. `priority` is rejected with `422`. The owner model and connection `api_family` are immutable.

Response `200`: Updated private connection object. Public `PUT` or `PATCH /api/connections/{connection_id}` rejects mutation requests.

#### List Connection References
```
GET /api/connections/{connection_id}/references
```
Response `200`: Owner references for the private connection, wrapped with the requested connection id. A valid connection has one owner:
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

#### Update Connection Pricing Template

Pricing templates are assigned through the model-private connection update route by setting `pricing_template_id`. Public connection-level pricing-template mutation routes reject writes.

#### Delete Model-Private Connection
```
DELETE /api/models/{model_config_id}/connections/{connection_id}
```
Response `200`: `{ "deleted": true }`.

Deletes the private connection and its internal owner access-target row together, subject to enabled-model target validation. Public `DELETE /api/connections/{connection_id}` rejects mutation requests.

#### Health Check Model-Private Connection
```
POST /api/models/{model_config_id}/connections/{connection_id}/health
```
Sends an api-family-specific lightweight request using the owner model and persists the connection health result. The route validates URL routing, authentication, ownership, and model availability end to end. Public connection-level health-check mutation routes reject writes.

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
  "weight": 1,
  "target_priority": 0,
  "is_enabled": true
}
```

Target semantics:
- Public `POST /api/models/{model_config_id}/targets` accepts `target_type="model"` with exact `target_model_id`, `position`, and optional `weight` / `target_priority`. Omitted public model-target metadata defaults to `weight = 1` and `target_priority = position`.
- Release 1 facade routing consumes exact target-model IDs only. Target payloads do not accept regex matcher fields or capability-metadata expansion.
- Public target authoring rejects submitted `target_type="connection"`, `connection_id`, or `target_connection_id` values. Private connections are created and managed through `/api/models/{model_config_id}/connections`.
- `PUT` and `PATCH /api/models/{model_config_id}/targets/{target_id}` update target metadata within the owning model scope. For internal connection targets, `PATCH` accepts only `position` and `is_enabled`; `weight`, `target_priority`, and pointer fields must stay omitted and immutable.
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position` is the dedicated move route and accepts `to_index`.
- Existing internal `target_type="connection"` rows identify the source model that owns a private connection and provide the runtime terminal routing edge.
- Target positions are contiguous starting at `0` and determine routing order for that source model.
- Target validation is selected-profile scoped, same-family, enabled-target aware, cycle-safe, and nested-facade-safe.

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
  "cache_creation_price": "0",
  "reasoning_price": "15.00"
}
```
Response `201`: Created pricing template object.

Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Create and update ingress normalizes missing, `null`, empty, and whitespace-only values for any of those five fields to `"0"` before decimal validation. Explicit `"0"` is configured free pricing. It is not missing price data. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or for required FX data that cannot be applied.

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

Prism uses a split config-bundle contract with two explicit ownership domains:

- **Profile bundle**: `version: 3`, profile-scoped config only
- **Vendor catalog bundle**: `version: 1`, global vendor metadata only

#### Export Profile Configuration
```
GET /api/config/profile/export
```
Response `200`:
```json
{
  "version": 3,
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
      "api_key_secret_ref": null,
      "position": 0
    }
  ],
  "pricing_templates": [],
  "loadbalance_strategies": [],
  "connections": [
    {
      "connection_ref": "openai-primary",
      "endpoint_name": "Primary OpenAI",
      "api_family": "openai",
      "context_window_tokens": 128000,
      "default_output_token_reserve": 4096,
      "max_context_utilization": 0.90,
      "preferred_context_utilization_threshold": 0.70,
      "is_active": true,
      "name": "Primary production key",
      "auth_type": null,
      "custom_headers": {},
      "openai_probe_endpoint_variant": "responses_minimal",
      "pricing_template_name": null,
      "qps_limit": null,
      "max_in_flight_non_stream": null,
      "max_in_flight_stream": null
    }
  ],
  "models": [
    {
      "model_id": "gpt-4o",
      "vendor_key": "openai",
      "api_family": "openai",
      "display_name": "GPT-4o",
      "context_window_tokens": 128000,
      "default_output_token_reserve": 4096,
      "max_context_utilization": 0.90,
      "preferred_context_utilization_threshold": 0.70,
      "loadbalance_strategy_name": "Default fill-first routing",
      "facade_enabled": false,
      "facade_selection_policy": null,
      "facade_fallback_policy": null,
      "context_overflow_promotion_target_id": null,
      "is_enabled": true,
      "access_targets": [
        {
          "target_type": "connection",
          "connection_ref": "openai-primary",
          "position": 0,
          "is_enabled": true
        }
      ]
    }
  ],
  "profile_settings": {
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "timezone_preference": "Europe/Helsinki",
    "endpoint_fx_mappings": []
  },
  "header_blocklist_rules": [],
  "user_agent_client_rules": [],
  "secret_payload": {
    "kind": "encrypted",
    "cipher": "fernet-v1",
    "key_id": "sha256:...",
    "entries": []
  }
}
```
The response includes a `Content-Disposition` header to trigger a file download: `attachment; filename="prism-profile-config-v3-YYYY-MM-DD.json"`.

Profile export semantics:
- `bundle_kind` is always `profile_config`.
- `GET /api/config/profile/export` returns the safe redacted default bundle.
- `POST /api/config/profile/export/with-secrets` returns the dangerous full secret-bearing bundle and requires `X-Prism-Dangerous-Confirm: profile-export`.
- `vendor_refs` are non-authoritative hints keyed by actual referenced `vendor_key` values only.
- Vendorless models export `vendor_key: null` and do not add entries to `vendor_refs`.
- Safe exports never include plaintext `endpoints[].api_key`.
- Safe exports null reusable endpoint secret refs and do not include `secret_payload.entries[]`.
- Dangerous exports include `secret_payload.entries[]` and reusable endpoint secret refs.
- Export fails if a stored endpoint secret cannot be decrypted before bundle encryption.
- Profile bundles preserve top-level private connection records, model `access_targets`, same-family model routing, exact-facade model flags (`facade_enabled`, `facade_selection_policy`, `facade_fallback_policy`), and attached loadbalance strategy references. Each exported `connection_ref` must be owned by exactly one model access target.
- Export serializes exact facade state by exact model ID only. Release 1 profile bundles do not include regex matcher fields or capability-metadata facade expansion.
- Export includes `models[].context_overflow_promotion_target_id` as a nullable exact model ID. Import validates the same enabled, same-family, non-facade, larger-window target rules as model CRUD.
- Exported model-to-model access targets carry explicit `weight` and `target_priority`. Internal connection targets continue to omit both metadata fields.
- Export and preview always serialize effective context capability defaults explicitly: omitted model or connection reserves become `default_output_token_reserve: 4096`, and omitted utilization becomes `max_context_utilization: 0.90`. Import may accept legacy omissions, but it normalizes them before persistence and any later export.

#### Preview Profile Import
```
POST /api/config/profile/import/preview
```
Request: Full profile bundle using `version: 3` and `bundle_kind: "profile_config"`.

This preview route is profile-scoped and requires `X-Profile-Id`.

Response `200`:
```json
{
  "ready": true,
  "version": 3,
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
  "replacement_scope": {
    "target": "selected_profile",
    "endpoints": 2,
    "pricing_templates": 4,
    "loadbalance_strategies": 2,
    "models": 5,
    "connections": 10,
    "header_blocklist_rules": 2,
    "user_agent_client_rules": 1,
    "profile_settings": true
  },
  "untouched_scope": {
    "other_profiles": true,
    "existing_global_vendor_metadata": true,
    "request_logs": true
  },
  "vendor_summary": {
    "create_count": 1,
    "reuse_count": 2,
    "warning_count": 0
  },
  "secret_summary": {
    "endpoint_secret_refs": 1,
    "secret_payload_entries": 1,
    "decryptable_secret_refs": 1
  },
  "preview_token": "ptok_...",
  "bundle_fingerprint": "sha256:...",
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
- The backend validates bundle kind/version, top-level private connection references, ordered model access targets, vendor resolution, and secret decryption before returning `ready: true`.
- Preview rejects any `connection_ref` used by multiple models or any imported connection ref that cannot be owned by exactly one model.
- Preview rejects profile bundle versions other than `3`; older profile bundle versions return `400`.
- Profile bundles stay on `version: 3` during planner rollout and do not include the temporary bootstrap-owned runtime rollout controls.
- Preview returns a server-issued preview token, and apply must send that token in `X-Prism-Preview-Token`.
- Preview rejects plaintext or otherwise non-encrypted `secret_payload.entries[].ciphertext` values.
- When bundle key validation or secret decryption fails, preview returns `ready: false` with `blocking_errors[]` and does not mutate profile state.

#### Import Profile Configuration
```
POST /api/config/profile/import
```
Request: Full profile bundle using `version: 3` and `bundle_kind: "profile_config"`.

This import route is profile-scoped and requires `X-Profile-Id`.

Apply semantics:
- `X-Prism-Preview-Token` is required on apply.
- Missing preview token returns `400`.
- Invalid, stale, or mismatched preview token returns `409`.
- The preview/apply linkage lives in the header only; the raw bundle JSON stays unchanged.

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
- The profile import lanes replace profile-scoped rows only, including endpoints, private connections, model configs, model access targets, profile settings, loadbalance strategies, header blocklist rules, and user-agent client rules that belong to the effective profile.
- Global vendor rows, other profiles, and request logs remain untouched.
- `models[].vendor_key` is optional; when omitted or `null`, the imported model persists with `vendor_id = null` and `vendor = null`.
- When `models[].vendor_key` is present, the backend resolves or creates the matching shared vendor row by that key only.
- Global vendor rows are resolved by `vendor_key` only.
- If a referenced `vendor_key` already exists globally, import reuses that vendor row.
- If vendor hints differ from existing global metadata, import does not fail and does not mutate the existing global vendor row.
- If a profile bundle would create a new vendor whose proposed name collides with an existing global vendor name, preview/import fail before profile replacement starts.
- When endpoint `position` is present, import uses it as the ordering hint; when omitted, import falls back to endpoint file order. Persisted endpoint positions are normalized to contiguous `0..N-1` values.
- Exported model access targets are ordered by `position`. During import, access-target positions are normalized to contiguous `0..N-1` values while preserving relative payload order.
- Legacy bundle omissions are normalized before validation and persistence: missing facade fields become `facade_enabled = false` with nil policies, and missing model-target `weight` / `target_priority` become `1` / `position`.
- Import accepts only the Release 1 exact facade policy strings `weighted_eligible_context` and `redistribute_ineligible_weight`; regex matcher fields and capability-metadata facade expansion remain unsupported.
- Import rejects nested facades in the final imported graph, including model targets that point at facade-enabled target models.
- Import decrypts bundle secrets before any destructive mutation begins, then re-encrypts them into Prism's normal at-rest secret storage.
- Endpoints with `api_key_secret_ref: null` import as no-auth endpoints with an empty stored endpoint secret.
- Wrong bundle key or unreadable secret payloads fail before profile replacement starts.
- Internal IDs (`endpoint_id`, `connection_id`, `pricing_template_id`) remain omitted from the profile bundle. The contract uses name-based references such as `endpoint_name`, `pricing_template_name`, `connection_ref`, and exact `target_model_id` values.
- Exported/imported models carry ordered `access_targets` entries with model targets and internally owned private connection targets plus `position` and `is_enabled` metadata.
- Exported/imported private connections live at the top level and include `api_family`, endpoint and pricing-template name references, context capability fields including `preferred_context_utilization_threshold`, OpenAI probe variant metadata, `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited for limiter fields and no preferred band for the preferred-threshold field.
- Import rejects `connection_ref` values used by multiple models, duplicate private connection ownership inside the bundle, and existing DB ownership collisions detected before profile replacement starts.
- Exported pricing templates always carry the five pricing fields as concrete strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Profile bundle v3 import normalizes missing, null, or blank pricing inputs for any of those fields to `"0"` before validation.
- Exported/imported loadbalance strategies include routing family in `legacy_strategy_type`, including `cheapest_eligible_context`.
- Their explicit Ban Policy shape carries failure status codes, retry-window fields, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, ban mode, and ban duration. Import rejects removed keys and accepts only `off`, `temporary`, or `until_reset` for `ban_mode`.
- Other profile config version numbers are unsupported.

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
  "mutation_scope": {
    "target": "global_vendor_catalog",
    "create_count": 1,
    "update_count": 1,
    "unchanged_count": 0
  },
  "untouched_scope": {
    "profiles": true,
    "profile_scoped_config": true,
    "request_logs": true
  },
  "preview_token": "ptok_...",
  "bundle_fingerprint": "sha256:...",
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

Apply semantics:
- `X-Prism-Preview-Token` is required on apply.
- Missing preview token returns `400`.
- Invalid, stale, or mismatched preview token returns `409`.
- The preview/apply linkage lives in the header only; the raw bundle JSON stays unchanged.

Vendor catalog semantics:
- Vendor catalog bundles are authoritative for global vendor metadata only.
- Vendor catalog import upserts vendor metadata by `key`.
- Vendor catalog preview/import mutate only the shared vendor catalog.
- Vendor catalog preview/import reject duplicate bundle keys, duplicate bundle names, and global name collisions before mutation.
- Vendor catalog import is independent from the profile backup/import flow.
- Vendor catalog exports and imports stay in the same split-bundle workflow, while vendor catalog payloads keep their own `version: 1` contract.

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

When auth email delivery is disabled, this workflow remains no-op-compatible. When SMTP is enabled, delivery uses the configured transport. Recovery-email verification send failures return a generic error and roll back the pending email state.

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

There is no standalone `/api/settings/monitoring` route or `/api/monitoring/*` family in the current live API contract. Current operator-facing observability and routing-health surfaces are provided through `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, and the manual connection health endpoints.

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

### 1.10 Sidecars (Global CLIProxyAPI Control Plane)

Sidecar routes are global management routes. They omit `X-Profile-Id` and operate on instance-wide CLIProxyAPI registrations. Prism stores sidecar metadata plus optional normalized provider inventory for display. Auth-files are live control-plane reads from CLIProxyAPI, and CLIProxyAPI remains the source of truth for live auth/provider state.

#### List Sidecars
```
GET /api/sidecars
```
Response `200`: `{ "items": SidecarInstance[] }`.

#### Create Sidecar
```
POST /api/sidecars
```
Request includes `name`, `base_url`, required `management_password`, optional `environment_label`, `enabled`, `sync_interval_seconds`, `request_timeout_seconds`, `allow_private_network`, `allow_insecure_http`, and `skip_tls_verify`.

Response `201`: Created sidecar. Raw management passwords are never returned; responses include `credential_state.management_password_configured` and may include the mask string `********` only.

#### Get, Update, Delete Sidecar
```
GET /api/sidecars/{sidecar_id}
PATCH /api/sidecars/{sidecar_id}
DELETE /api/sidecars/{sidecar_id}
```
`PATCH` accepts the create fields as optional values. Supplying `management_password` rotates the stored credential and resets management-auth state. `DELETE` soft-deletes the registration and returns `204`.

#### Test Connection and Sync
```
POST /api/sidecars/{sidecar_id}/test-connection
POST /api/sidecars/{sidecar_id}/sync
GET /api/sidecars/{sidecar_id}/sync-status
```
Connection tests call CLIProxyAPI management auth through the backend and return `state`, `management_auth_state`, and `status_code`. Manual sync returns `state` (`succeeded`, `skipped`, or `failed`), the updated `sidecar`, `sync_status`, `provider_snapshot_count`, and optional `error_code`/`error_detail`. Manual sync on a disabled sidecar returns `409`; invalid management auth returns `424`; other upstream failures return `502`.

Prism reads CLIProxyAPI `/auth-files` live on demand as a strict top-level `files` envelope. Missing `files`, legacy `auth_files`, `files: null`, and non-array `files` are contract failures. An empty `files: []` response is valid live state and returns an empty Prism `items` list. Provider inventory is a separate display supplement and is never an auth-file fallback.

#### Live Auth-Files and Provider Inventory
```
GET /api/sidecars/{sidecar_id}/auth-files
GET /api/sidecars/{sidecar_id}/providers
GET /api/sidecars/{sidecar_id}/provider-snapshots
```
`auth-files` returns live auth-file observations from CLIProxyAPI. Provider snapshots are optional normalized display inventory from CLIProxyAPI provider endpoints for Gemini, Claude, Codex, Vertex, and OpenAI-compatible credentials. Payloads are redacted before storage or display; auth-file metadata may include safe UI gating hints such as `path_present` and `delete_supported` without exposing file paths or secrets.

#### Auth-File Models Discovery
```
GET /api/sidecars/{sidecar_id}/auth-files/models?name={auth-file-name-or-id}
```
This route is a read-only passthrough to CLIProxyAPI `GET /v0/management/auth-files/models?name=...`. Response `200` returns `{ "models": [...] }`; model items include `id` and may include `display_name`, `type`, and `owned_by`. A successful `{ "models": [] }` means discovery is supported but no models are currently available for that auth file. Upstream 404/not-found behavior is returned as Prism `404` with an explicit unsupported detail. Prism does not mutate auth state, does not persist model discoveries, and does not use retained auth rows or provider inventory as fallback model payloads.

#### Auth-File Mutations
```
DELETE /api/sidecars/{sidecar_id}/auth-files/{auth_id}
PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/status
PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields
```
Status mutations accept `disabled`. Field mutations are priority-only: the `/fields` request body requires `priority`; no other auth-file fields are mutable through Prism. Single auth-file delete accepts `{ "confirm_name": "..." }` and no batch/delete-all/upload/download surface. Mutations flow through Prism's backend service so the browser never calls CLIProxyAPI directly.

Before any upstream PATCH or DELETE, Prism fetches live `/auth-files` state and allows mutation only when exactly one live row matches `{auth_id}`, that row has stable non-name-derived identity, and its current `name` is unique in the live auth list. Prism refuses duplicate-name collisions, name-derived/degraded rows, ambiguous live matches, and missing live rows. Local retained observations never authorize mutation and there is no retry control for stale cached data. Successful PATCH responses return `state: "succeeded"` with the refreshed live auth-file row when the post-mutation refresh succeeds, or `state: "succeeded_sync_failed"` plus `sync_error` when the upstream mutation succeeded but Prism could not refresh local truth.

Delete is stricter than PATCH: Prism requires the live row to be file-backed, non-runtime-only, non-path-like by name, confirmed by an exact `confirm_name` match against the current live auth name, and backed by known-supported CLIProxyAPI delete capability from normal `/auth-files` management response metadata or a bounded non-mutating empty `DELETE /v0/management/auth-files` probe for CPA builds with metadata before calling CLIProxyAPI `DELETE /v0/management/auth-files` with `{ "names": [name] }`. Missing or unknown capability refuses the delete without issuing a named upstream `DELETE`. Successful clean delete returns `state: "succeeded"` and omits the auth-file row when the row is absent after the delete-only refresh, including the final remaining auth file case. If the upstream delete succeeds but Prism cannot refresh local truth, the response returns `state: "succeeded_sync_failed"`, the pre-delete live auth-file row, and `sync_error`.

Authfile priority has two context-specific zero semantics. In CLIProxyAPI live `/auth-files` listing behavior, `priority=0` is a valid explicit baseline/lowest numeric bucket; higher numeric values are preferred, and a returned `priority: 0` is not absent or removed. The upstream CLIProxyAPI `/auth-files/fields` PATCH surface is the exception: sending `priority: 0` is the API-specific clear/remove sentinel for stored priority metadata, so Prism forwards that literal field-update value and then refreshes live auth-files from CLIProxyAPI state.

---

## 2. Runtime Proxy API

Prism's runtime proxy is an explicit allowlist, not a full vendor API clone. It forwards only the operations listed in this section through the active profile. Other vendor routes, including stored-object, list, retrieve, delete, cancel, compact, embedding, model-list, file, batch, and admin APIs, are outside Prism's runtime contract unless they appear in this allowlist.

Runtime proxy routes ignore management `X-Profile-Id` overrides and always use the active runtime profile. Selected-profile management scope changes configuration reads and writes only; it does not switch proxy traffic.

### 2.1 Supported Runtime Operations

| Operation | Canonical operation name | Supported request |
|---|---|---|
| OpenAI chat completions | `openai.chat_completions` | `POST /v1/chat/completions` |
| OpenAI Responses | `openai.responses` | `POST /v1/responses` |
| OpenAI image generations | `openai.images.generations` | `POST /v1/images/generations` |
| OpenAI image edits | `openai.images.edits` | `POST /v1/images/edits` |
| Anthropic Messages | `anthropic.messages` | `POST /v1/messages` |
| Anthropic token count | `anthropic.count_tokens` | `POST /v1/messages/count_tokens` |
| Gemini generate content | `gemini.generate_content` | `POST /v1beta/models/{model}:generateContent` |
| Gemini stream generate content | `gemini.stream_generate_content` | `POST /v1beta/models/{model}:streamGenerateContent` |
| Gemini token count | `gemini.count_tokens` | `POST /v1beta/models/{model}:countTokens` |

Each allowlisted row maps to one canonical operation name persisted as `operation_name` in runtime telemetry. Operation names are part of the runtime contract, not aliases for broader vendor route groups. The Gemini `{model}` path binding is one non-empty path segment. Nested Gemini model paths are not part of this runtime contract.

### 2.2 Unsupported Routes and Methods

Unsupported runtime routes return a Prism JSON `404` response before Prism reads the request body, resolves a model, contacts a provider, creates runtime admission state, submits runtime side effects, or writes runtime persistence rows. The current error detail is `Runtime operation not found`.

Wrong methods on supported runtime paths return a Prism JSON `405` response before the same downstream seams run. The response includes `Allow: POST`, and the current error detail is `Method not allowed for runtime operation`.

### 2.2A Context-aware routing failures

When the attached strategy is `cheapest_eligible_context`, Prism performs local preflight context estimation before provider transport for OpenAI Chat Completions and OpenAI Responses requests that have deterministic request-local input. The estimator methods are `openai_chat_heuristic_v1` and `openai_responses_heuristic_v1`. They add estimated input tokens plus an explicit request output limit when present, then `default_output_token_reserve`, then fallback `4096`. Hard-fit legality uses `floor(context_window_tokens * max_context_utilization)`, with the default utilization normalized to `0.90`. The nullable `preferred_context_utilization_threshold` creates an optional preferred band at `floor(context_window_tokens * preferred_context_utilization_threshold)`; `null` means no preferred band. Fitting candidates above the preferred band but within hard fit are discretionary, and candidates above hard fit are ineligible.

When Prism cannot bound an OpenAI Chat Completions or Responses request locally, it passes the request through the normal resolved target path instead of returning local `400 context_estimation_unavailable`. This pass-through is non-stream and stream agnostic at the planning layer, but later overflow replay is non-stream only. Prism still returns local failures for non-OpenAI operations, unsupported translated shapes, and other planner errors that are not missing context estimation.

When context fit is evaluated and no terminal target fits, Prism returns HTTP `413` before provider transport with:

```json
{
  "error": "context_window_exceeded",
  "detail": "No configured target can fit the estimated request context.",
  "estimated_total_context_tokens": 4216,
  "largest_usable_context_window_tokens": 4096
}
```

The matching request-log detail can include `routing.context_routing` with `policy`, `estimation_method`, `estimated_input_tokens`, `reserved_output_tokens`, `estimated_total_context_tokens`, `usable_context_window_tokens`, `cost_ranking_method`, `selected_terminal_target_id`, `selected_endpoint_id`, `selected_context_band`, `selected_usable_context_window_tokens`, `selected_estimated_blended_cost_micros`, and `skipped_terminal_targets[]`. Skipped targets include `context_band`, with hard-fit rejects reported as `ineligible`. Candidate ranking is band-first: preferred candidates sort before discretionary candidates, then within each band Prism ranks priced candidates before unpriced candidates by estimated blended request cost, access-target position, terminal target ID, and target ID.

### 2.2B OpenAI sibling-operation translation

OpenAI Chat Completions and Responses targets can be siblings for runtime planning. Translation eligibility is explicit and terminal-target based: native-capable targets keep `operation_translation_mode = "none"`, while compatible sibling targets may use `openai_responses_to_chat_completions` or `openai_chat_completions_to_responses` only when the selected connection's `openai_upstream_operation` differs from ingress and the request shape is in Prism's supported subset. Blank or default probe metadata resolves request-side translation as native Responses capability, not as a translated Chat target.

Unsupported translated request shapes reject before provider transport with `openai_request_translation_unsupported` when translation compatibility is the blocker. Public Responses requests with stateful shapes such as `previous_response_id` can pass through when missing context estimation is the only blocker, but they still reject if translation compatibility fails.

Translated non-stream and stream responses are rewritten back to the ingress operation shape for the client. Runtime usage remains canonical from the raw upstream payload or terminal stream event, translated responses strip unsafe entity headers before writing to the client, and audit body capture stays upstream-native rather than translated.

Ingress observability remains stable: `operation_name` is always the client-visible operation. Additive upstream fields use `upstream_operation_name`, `operation_translation_mode`, and `upstream_request_path` for request logs, usage events, and request-log detail. `upstream_request_path` is the sanitized operation path Prism sent upstream, not an unbounded raw URL.

### 2.2C Exact OpenAI facade routing (Release 1)

Release 1 exact facade routing is backend-first and exact-ID only. Planning starts from the requested model's exact active-profile lookup and activates only when the requested OpenAI model has `facade_enabled = true`, `facade_selection_policy = "weighted_eligible_context"`, and `facade_fallback_policy = "redistribute_ineligible_weight"`.

Facade planner behavior:
- evaluates only same-family model targets from that exact requested model
- reuses the existing child-target context, translation, and runtime-state eligibility evaluation
- redistributes configured weights across the eligible subset only
- uses target-set-aware weighted cursoring for stable eligible sets
- returns one selected child plan; later provider retry or failover can continue only inside that selected child model's own terminal-target strategy and never jumps sideways to sibling facade targets

Release 1 exact facade routing does not add regex model matching, capability-metadata expansion, frontend facade authoring, or response-body model rewriting.

Facade rejections stay aligned with the selected child evaluation branch before provider transport: translated-shape rejection returns `400 openai_request_translation_unsupported`, hard no-fit returns `413 context_window_exceeded`, and no eligible child target returns `503`.

### 2.2D CLIProxyAPI context overflow promotion

The known upstream for context overflow promotion is CLIProxyAPI, not official OpenAI. Prism does not claim one official OpenAI-shaped overflow envelope. CLIProxyAPI can serve native OpenAI-compatible and translated sibling-operation paths through different executors, so Prism treats promotion as a conservative Prism-classified replay path over the response it actually receives.

Promotion scope is intentionally narrow:
- Only `openai.chat_completions` and `openai.responses` are eligible.
- Only non-stream responses are eligible. Streaming promotion is not implemented in v1.
- Replay happens before downstream commit from the shared non-stream response branch.
- Replay uses the original buffered ingress body exactly once.
- Only a model's explicit `context_overflow_promotion_target_id` can be used. Prism never searches sibling facade targets, pricing metadata, display names, or vendor rows for a larger model.
- Strict-mode promotion is not implemented in v1.

The selected-child exact-facade restriction is preserved. If a public facade selected one child model, promotion eligibility is evaluated on that selected child only. Prism does not reopen the facade sibling set, and no sibling retry is allowed after child selection.

The classifier is status plus body, never status alone. Eligible statuses are `400`, `413`, `422`, and body-confirmed `429`. Plain `429` never promotes; rate-limit, quota, capacity, auth, model lookup, malformed JSON, and ambiguous validation bodies are returned without promotion unless the body carries explicit context-overflow evidence. Native non-stream paths can classify OpenAI-style top-level `error` objects or unambiguous flat CLIProxyAPI gateway JSON. Translated non-stream paths accept only top-level `error` objects; translated flat-gateway JSON is rejected for promotion in v1 and the original source response is returned.

If promotion starts, Prism closes the source response body and executes the promoted model once. The promoted model can still use its own ordinary terminal strategy, but a second promotion is never attempted. Final status, usage, pricing, and `usage_request_events` attribution come from the final response returned to the client. Failed source attempts remain visible as attempt-level `request_logs` rows and optional audit rows under the same `ingress_request_id`.

### 2.3 OpenAI Operations

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
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.chat_completions`.

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
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses`.

#### Image Generations
```
POST /v1/images/generations
```
Request uses the upstream OpenAI-compatible image generation body, including body-bound `model`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.images.generations`.

#### Image Edits
```
POST /v1/images/edits
```
Request uses the upstream OpenAI-compatible image edit body, including body-bound `model`. JSON and multipart request bodies are supported for model binding and forwarding.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.images.edits`.

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

#### Count Tokens
```
POST /v1beta/models/{model}:countTokens
```
Request uses the upstream Gemini native token-count body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.count_tokens`.

### 2.6 Streaming

Streaming stays operation-native: `openai.chat_completions`, `openai.responses`, and `anthropic.messages` use their upstream-compatible request body flags, while `gemini.stream_generate_content` uses `POST /v1beta/models/{model}:streamGenerateContent`. Streaming responses are proxied directly from upstream.
For Gemini, the `gemini.stream_generate_content` path is authoritative: `POST /v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`. `gemini.generate_content` remains the non-stream generate-content operation.

### 2.7 Token Usage Extraction

The gateway extracts token usage from upstream responses and logs canonical disjoint token components to `request_logs`. Extraction is selected by the resolved canonical operation name and its hook collection. `input_tokens` is base input only, `output_tokens` is base output only, cache-read input, cache-creation input, and reasoning output are separate fields, and `total_tokens` uses the provider total when one is supplied.

**Non-streaming responses:**
| Canonical operation name | Response format | Extraction path |
|---|---|---|
| `openai.chat_completions`, `openai.responses` | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `anthropic.messages` | `{"usage": {"input_tokens": N, "cache_read_input_tokens": N, "cache_creation_input_tokens": N, "output_tokens": N}}` | Base input, cache-read input, cache-creation input, and base output stay separate; total is derived when upstream omits it |
| `anthropic.count_tokens` | `{"input_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `gemini.generate_content`, `gemini.stream_generate_content` when handled as non-stream JSON | `{"usageMetadata": {"promptTokenCount": N, "cachedContentTokenCount": N, "candidatesTokenCount": N, "thoughtsTokenCount": N, "totalTokenCount": N}}` | Base input subtracts cache-read input; base output subtracts reasoning output; provider `totalTokenCount` stays authoritative |
| `gemini.count_tokens` | `{"totalTokens": N}` or `{"total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `openai.images.generations`, `openai.images.edits` | Media response bodies | Token fields remain `null`; media hooks copy the upstream response without estimating tokens |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from operation-specific terminal events:
| Canonical operation name | Usage events | Extraction |
|---|---|---|
| `openai.chat_completions` | Final usage chunk before `[DONE]` | Same canonical disjoint fields as non-streaming usage |
| `openai.responses` | `response.completed` event with a `usage` object when provided by upstream | Same canonical disjoint fields as non-streaming usage |
| `anthropic.messages` | `message_start` usage plus cumulative `message_delta.usage.output_tokens` | Base input, cache-read input, cache-creation input, and final base output stay separate |
| `gemini.stream_generate_content` | Stream terminal or final chunk carrying `usageMetadata` | Same canonical disjoint fields as Gemini non-stream `usageMetadata` |

If token data cannot be extracted from the provider response, runtime usage token fields are logged as `null`. Preflight context estimation for `cheapest_eligible_context` is routing metadata only and does not replace provider usage extraction or post-response costing. Completed streams that lack required usage keep `MISSING_TOKEN_USAGE`; interrupted or no-terminal streams with missing required tokens use `STREAM_USAGE_UNAVAILABLE` when their classified stream outcome made terminal usage unavailable. Aggregate `cached_tokens` is derived-only from cache-read plus cache-creation input tokens and is not a persisted runtime component.

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

### 4.0 Dashboard Stats
```
GET /api/stats/dashboard
```
This is the canonical overview dashboard read path. It returns one backend-computed aggregate snapshot for the effective profile, including overview metrics, API-family rows, recent requests, top-spending models, strategy-family counts, the legacy Routing Health Map, and the backend-owned topology graph. The same `DashboardSnapshot` shape is nested under realtime `dashboard.update.snapshot`, so REST bootstrap and websocket updates share one schema.

Query parameters: none. Legacy `window` query values are ignored. The endpoint always returns the canonical aggregate snapshot and does not expose the old top-level `window`, `covers`, `freshness`, or `metrics` shape.

Response `200`:
```json
{
  "generated_at": "2026-04-19T12:00:00Z",
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
  "strategy_summary": {
    "legacy_count": 8,
    "unassigned_count": 2
  },
  "recent_requests": [],
  "top_spending_models": [],
  "routing_health_map": {
    "nodes": [],
    "links": [],
    "endpointCount": 0,
    "modelCount": 0,
    "activeConnectionTotal": 0,
    "activeTerminalTargetTotal": 0,
    "trafficRequestTotal24h": 0
  },
  "topology_graph": {
    "nodes": [
      {
        "id": "terminal-target-1",
        "kind": "connection",
        "product_kind": "terminal_target",
        "label": "Primary production key",
        "status": "inactive",
        "terminal_target_id": 1,
        "connection_id": 1,
        "endpoint_id": 12,
        "active": false,
        "health_status": "healthy",
        "recent_request_count": 2,
        "recent_success_rate": 100,
        "last_request_at": "2026-04-19T11:55:00Z"
      }
    ],
    "edges": [
      {
        "id": "terminal-target-binding-1",
        "kind": "connection_to_endpoint",
        "product_kind": "terminal_target_to_endpoint",
        "source_node_id": "terminal-target-1",
        "target_node_id": "endpoint-12",
        "terminal_target_id": 1,
        "connection_id": 1,
        "endpoint_id": 12
      }
    ],
    "stats": {
      "model_count": 1,
      "active_model_count": 1,
      "disabled_model_count": 0,
      "terminal_target_count": 1,
      "active_terminal_target_count": 0,
      "inactive_terminal_target_count": 1,
      "endpoint_count": 1,
      "edge_count": 1
    }
  }
}
```

`routing_health_map` and `topology_graph` are assembled by the backend from selected-profile model, access-target, endpoint, connection, request-log, and usage-event data. API clients must not rebuild route edges from separate management reads. Disabled models remain as muted `status="disabled"` model nodes, inactive terminal targets remain as muted `status="inactive"` nodes with `active=false`, and terminal-target nodes retain `connection_id` as the persisted compatibility identifier. During the additive compatibility wave, the backend keeps topology compatibility kinds (`kind="connection"`, `kind="model_to_connection"`, and `kind="connection_to_endpoint"`) while exposing product-facing terminal-target meaning through `product_kind`.

### 4.1 Usage Snapshot
```
GET /api/stats/usage-snapshot
```
This is the REST analytics snapshot contract for API callers and debugging. The `/dashboard?tab=analytics` UI receives the same snapshot shape through `analytics.snapshot` WebSocket messages, but this REST endpoint remains supported and documented.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Snapshot range preset. Supported values: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |

The snapshot is backed by `backend/internal/httpapi/management/stats/service.go` together with the aggregation types and query helpers in `backend/internal/domain/stats/snapshot.go` and `backend/internal/domain/stats/types.go`.

The snapshot is aggregated from persisted usage-event rows. `/api/stats/dashboard` is the canonical overview aggregate that also includes the backend-computed Routing Health Map. Exact request investigation remains on `/request-logs`, while dashboard and other pages continue to use the shared stats routes below.

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
      "resolved_target_model_id": "gpt-4o",
      "resolved_target_model_label": "GPT-4o",
      "api_family": "openai",
      "vendor_id": 1,
      "vendor_key": "openai",
      "vendor_name": "OpenAI",
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

The list route is the slim browse contract used by `/request-logs` and other row-summary consumers. It keeps one row per upstream attempt, returns `filter_options.endpoints` for the endpoint dropdown and `filter_options.models` for the model dropdown, includes requested-model labels, final-target labels, `stream_outcome`, and `stream_error_kind` for display, and does not treat vendor as a server filter. The current request-log page uses page sizes `100`, `300`, and `500`, with `100` as the frontend default. This retained-history route is the operator drill-in surface for investigation, not a dashboard aggregate or an OTLP metrics endpoint.

`filter_options` always includes both `endpoints` and `models`. `filter_options.models` is request-log scoped and contains `{ model_id, model_label }` entries; when no current model options exist, the backend still returns `models: []` instead of omitting the field. `ingress_request_id` groups multiple attempt rows that belong to one incoming runtime request. `model_id` stays the requested model and `resolved_target_model_id` captures the final target model for that attempt, while `resolved_target_model_label` surfaces the matching display label.

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
    "vendor_id": 1,
    "vendor_key": "openai",
    "vendor_name": "OpenAI",
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
    "terminal_target_id": 1,
    "selected_terminal_target_id": 1,
    "context_routing": {
      "policy": "cheapest_eligible_context",
      "selected_terminal_target_id": 1,
      "estimation_method": "openai_chat_heuristic_v1",
      "estimated_input_tokens": 15,
      "reserved_output_tokens": 4096,
      "estimated_total_context_tokens": 4111,
      "usable_context_window_tokens": 115200,
      "cost_ranking_method": "estimated_blended_request_cost_then_access_target_position_then_terminal_target_id",
      "selected_endpoint_id": 12,
      "selected_context_band": "preferred",
      "selected_usable_context_window_tokens": 115200,
      "selected_estimated_blended_cost_micros": 1250,
      "skipped_terminal_targets": []
    },
    "endpoint_base_url": "https://api.openai.com",
    "endpoint_description": "Primary production key",
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

Exact facade attempts add nested `routing.context_routing.facade_selection` metadata without rewriting the top-level requested/resolved model fields:
```json
{
  "facade_selection": {
    "facade_model_id": "gpt-4o-public",
    "selected_target_model_id": "gpt-4o-regional",
    "selected_weight": 3,
    "eligible_total_weight": 4,
    "exclusion_reasons": [
      {"reason": "translation_rejection", "count": 1}
    ],
    "exclusion_summary": "translation_rejection=1"
  }
}
```
The same planner output also surfaces in traces through additive `prism.runtime.facade_*` attributes: `prism.runtime.facade_model_id`, `prism.runtime.facade_selected_target_model_id`, `prism.runtime.facade_selected_weight`, `prism.runtime.facade_eligible_total_weight`, and `prism.runtime.facade_exclusion_summary`.

Response `404`: returned when the request ID is missing or out of scope for the effective profile.

Stream telemetry values are stable strings. `stream_outcome` is one of `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown`. `stream_error_kind` is nullable and, when present, is one of `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event`. `stream_error_detail` appears only on exact request-log detail responses; it is sanitized diagnostic text, not provider content, headers, or secrets.

The request-log sheet consumes this grouped detail contract. The frontend keeps audit loading separate and lazy: opening the `overview` tab uses only this response, while the `audit` tab resolves linked audit payloads on demand with `request_log_id` plus a UTC window derived from `summary.created_at`. The derived frontend window is `created_at` minus 12 hours through `created_at` plus 12 hours, serialized explicitly as canonical audit `from` and `to` query parameters.

The request-history, spending, throughput, usage-snapshot, model-metrics, connection-success-rate, and dashboard aggregate APIs in this section remain product-facing PostgreSQL-backed surfaces. OTLP metrics and traces are exported through the startup-configured Collector/Alloy path and are not surfaced as a backend-local `/metrics` compatibility route.

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

### 4.8 Global Log Retention Settings
```
GET /api/settings/log-retention
PUT /api/settings/log-retention
```

These routes are global and do not use `X-Profile-Id`. They store the instance-wide normal retention policy for all profiles. Request-log, audit-log, statistics, and load-balance list/detail APIs still filter by selected profile, but retention settings do not.

Request and response fields:
| Field | Type | Description |
|---|---|---|
| `request_logs_retention_days` | integer or null | Global request-log retention days. `null` disables the stored policy. |
| `audit_logs_retention_days` | integer or null | Global audit-log retention days. `null` disables the stored policy. |
| `statistics_retention_days` | integer or null | Global `usage_request_events` retention days. `null` disables the stored policy. |
| `loadbalance_events_retention_days` | integer or null | Global load-balance event retention days. `null` disables the stored policy. |

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

The response sets `Location` to the same job status URL. The job type is `log_retention`, uses `profile_id = 0`, and applies across all profiles.

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

Audit APIs are profile-scoped and require `X-Profile-Id`.

### 5.1 List Audit Logs
```
GET /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `request_log_id` | integer | none | Filter audit rows linked to one request log |
| `vendor_id` | integer | none | Filter by vendor ID |
| `model_id` | string | none | Filter by model ID |
| `status_code` | integer | none | Filter by response status code |
| `endpoint_id` | integer | none | Filter by endpoint ID |
| `connection_id` | integer | none | Filter by connection ID |
| `from` | datetime | required | Inclusive start of bounded time range (RFC 3339) |
| `to` | datetime | required | Exclusive end of bounded time range (RFC 3339) |
| `limit` | integer | 50 | Max results (1-200) |
| `cursor` | string | none | Opaque keyset cursor returned as `next_cursor` |
| `sort` | string | `desc` | Only `desc` is supported |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row. The `from` and `to` window is required and may not exceed 7 days, including when `request_log_id` is supplied. The backend has no fallback, default audit window, or legacy time-window aliases for request-log lookups. Unsupported query keys return `400` with `audit_filter_unsupported`.

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

When body capture is enabled and non-empty response bytes are captured, `response_body` stores those bytes and `response_body_stored=true`; `is_stream` does not prevent storage. For translated OpenAI sibling-operation attempts, `request_body` and `response_body` remain raw upstream-native payloads or SSE bytes. They do not store the translated client-facing request or response shape.
If vendor body capture is disabled, both `request_body` and `response_body` are `null`. Rows with `response_body_stored=false` have no stored response body, including old rows that were written before streaming response capture was available.

Response `404`: Audit log not found.

### 5.3 Audit Log Retention

Audit log list and detail APIs remain selected-profile scoped. Normal audit-log cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "audit_logs"`, or the stored `audit_logs_retention_days` value from `/api/settings/log-retention`.

The retired audit cleanup endpoints are not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

Audit rows retain weak request metadata in `request_log_id`, `request_log_created_at`, and `ingress_request_id`. A request detail link can be missing after request-log retention expires before audit-log retention.

### 5.3A Management Job Status and Cancel
```
GET /api/management/jobs
GET /api/management/jobs/{job_id}
POST /api/management/jobs/{job_id}/cancel
```

`GET /api/management/jobs` returns recent jobs in scope. Audit-delete jobs are profile-scoped. Global log-retention jobs use `profile_id = 0` and are visible to operators as instance maintenance jobs:
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

`GET /api/management/jobs/{job_id}` returns the same job object. `POST /api/management/jobs/{job_id}/cancel` marks an in-scope job for cancellation and returns `202` with the job object. Unknown or out-of-scope jobs return `404`.

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

This endpoint is selected-profile scoped through `X-Profile-Id` and creates the canonical explicit Ban Policy defaults for that profile only: `Default single routing`, `Default fill-first routing`, and `Default round-robin routing`.

Response `200`:
```json
{
  "items": [
    {
      "id": 12,
      "profile_id": 3,
      "name": "Default fill-first routing",
      "legacy_strategy_type": "fill-first",
      "ban_mode": "temporary"
    }
  ],
  "created_count": 1,
  "created_names": ["Default fill-first routing"],
  "existing_names": ["Default single routing"]
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
- `legacy_strategy_type` must be `single`, `fill-first`, `round-robin`, or `cheapest_eligible_context`.
- `failure_status_codes` must be a unique, sorted list of valid HTTP status integers (`100..599`).
- Retry-window delay, backoff, jitter, max delay, and cycle retry attempt limit must stay within backend bounds.
- `cycle_retry_attempt_limit` is required and valid from `1` to `50`.
- `ban_mode` is `off`, `temporary`, or `until_reset`.
- `ban_mode = "off"` requires `ban_cumulative_retry_attempt_threshold = 0` and `ban_duration_seconds = 0`.
- `ban_mode = "temporary"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds` from `1` to `86400`.
- `ban_mode = "until_reset"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds = 0`.
- Runtime retry-cycle exhaustion is inclusive: `cycle_retry_attempts >= cycle_retry_attempt_limit` schedules the retry-window transition.
- Runtime banning is inclusive and explicit: `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`. Prism never derives the ban threshold from `cycle_retry_attempt_limit`.
- Upstream request timing is controlled by the shared backend timeout settings rather than per-strategy fields.

### 6.4 Update Loadbalance Strategy
```
PUT /api/loadbalance/strategies/{strategy_id}
```
Request: Full replacement of mutable strategy fields using the same shape as create.
Response `200`: Updated strategy object.

Strategy responses include the persisted explicit Ban Policy strategy document:

```json
{
  "id": 12,
  "profile_id": 3,
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

### 6.5 Delete Loadbalance Strategy
```
DELETE /api/loadbalance/strategies/{strategy_id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the strategy is still attached to one or more models; the response detail includes `attached_model_count`.

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
Reset clears retry-window counters, next retry timing, ban state, and the related round-robin cursor for an attached model when one exists.

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
      "cycle_retry_attempts": 2,
      "cumulative_retry_attempts": 6,
      "cycle_retry_attempt_limit": 3,
      "ban_cumulative_retry_attempt_threshold": 6,
      "next_retry_at": "2026-03-30T08:02:00Z",
      "last_retry_delay_ms": 60000,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "vendor_id": 1,
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

### 6.8 Get Loadbalance Event Detail
```
GET /api/loadbalance/events/{id}
```
Response `200`: Single event object with the same Ban Policy retry-window metadata, policy snapshot fields, and summary fields as the list item.

### 6.9 Loadbalance Event Retention

Loadbalance event list and detail APIs remain selected-profile scoped. Normal cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "loadbalance_events"`, or the stored `loadbalance_events_retention_days` value from `/api/settings/log-retention`.

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

### 8.1 WebSocket Transport
```
WS /api/realtime/ws
```

Authentication:
- When operator auth is disabled, the socket can connect without cookies.
- When operator auth is enabled, the backend validates the configured access-token cookie before allowing subscriptions.

Supported channels:
- `dashboard`
- `analytics`

Common client -> server messages:
```text
{ "type": "ping" }
{ "type": "pong" }
{ "type": "unsubscribe" }
```

Common server -> client messages include:
- `authenticated`
- `heartbeat`
- `subscribed`
- `unsubscribed`
- `error`
- `pong`

### 8.2 Dashboard WebSocket Channel

The dashboard channel is the overview channel for the main dashboard page. It is profile-scoped by the message payload and broadcasts incremental `dashboard.update` payloads after request activity. It is separate from the scoped Analytics channel.

Client -> server messages:
```text
{ "type": "subscribe", "profile_id": 2, "channel": "dashboard" }
{ "type": "unsubscribe_channel", "channel": "dashboard" }
```

Dashboard server -> client messages include `dashboard.update`.

Example `dashboard.update` payload:
```json
{
  "type": "dashboard.update",
  "request_log": {
    "id": 101,
    "profile_id": 2,
    "model_id": "gpt-4o",
    "model_label": "GPT-4o",
    "request_path": "/v1/chat/completions"
  },
  "snapshot": {
    "generated_at": "2026-04-19T12:00:00Z",
    "coverage_24h": {
      "from": "2026-04-18T12:00:00Z",
      "to": "2026-04-19T12:00:00Z"
    },
    "coverage_30d": {
      "from": "2026-03-20T12:00:00Z",
      "to": "2026-04-19T12:00:00Z"
    },
    "health": { "lag_seconds": 0, "stale": false, "stale_after_seconds": 120 },
    "metric_snapshot": { "total_requests": 42, "success_rate": 97.62 },
    "api_family_rows": [],
    "strategy_summary": { "legacy_count": 8, "unassigned_count": 2 },
    "recent_requests": [],
    "top_spending_models": [],
    "routing_health_map": { "nodes": [], "links": [], "endpointCount": 0, "modelCount": 0 }
  }
}
```

`dashboard.update` is a thin envelope over `request_log` plus `snapshot`. It no longer carries the old top-level per-section compatibility fields. The embedded `snapshot.top_spending_models` rows reuse the same `{model_id, model_label, total_cost_micros}` shape as `GET /api/stats/spending`.

### 8.3 Analytics WebSocket Channel

The analytics channel serves `/dashboard?tab=analytics`. A subscription is scoped by `{profile_id,preset}` in the message payload. Supported presets are `1h`, `6h`, `24h`, `7d`, `30d`, and `all`.

Client -> server messages:
```text
{ "type": "subscribe", "profile_id": 2, "channel": "analytics", "preset": "24h" }
{ "type": "refresh", "profile_id": 2, "channel": "analytics", "preset": "24h" }
{ "type": "unsubscribe_channel", "channel": "analytics", "preset": "24h" }
```

`subscribe` sends `subscribed` and then an initial `analytics.snapshot` for that exact scope. `refresh` sends a fresh direct `analytics.snapshot` for an existing connection-local subscription. `unsubscribe_channel` is preset-scoped and connection-local, so it does not include `profile_id`.

Analytics server -> client messages include:
- `analytics.snapshot`
- `analytics.error`

Each `analytics.snapshot` is a full replacement for one `{profile_id,preset}` scope. The `snapshot` field uses the same `UsageSnapshotResponse` shape as `GET /api/stats/usage-snapshot`. The `endpoint_model_statistics_by_endpoint_id` field is keyed by endpoint ID as a string and carries the endpoint model statistics rows that are otherwise available from `GET /api/stats/endpoints/{endpoint_id}/models` for API and debug callers.

Example `analytics.snapshot` payload:
```json
{
  "type": "analytics.snapshot",
  "channel": "analytics",
  "profile_id": 2,
  "preset": "24h",
  "sequence": 3,
  "generated_at": "2026-05-04T12:00:00Z",
  "snapshot": {
    "generated_at": "2026-05-04T12:00:00Z",
    "time_range": {
      "preset": "24h",
      "start_at": "2026-05-03T12:00:00Z",
      "end_at": "2026-05-04T12:00:00Z"
    },
    "currency": { "code": "USD", "symbol": "$" },
    "overview": { "total_requests": 42, "success_rate": 97.62 },
    "service_health": { "request_count": 42, "success_count": 41, "failed_count": 1, "cells": [] },
    "request_trends": { "hourly": [], "daily": [] },
    "token_usage_trends": { "hourly": [], "daily": [] },
    "token_type_breakdown": { "hourly": [], "daily": [] },
    "cost_overview": { "total_cost_micros": 1250000, "hourly": [], "daily": [] },
    "endpoint_statistics": [],
    "model_statistics": [],
    "proxy_api_key_statistics": []
  },
  "endpoint_model_statistics_by_endpoint_id": {
    "12": [
      {
        "model_id": "gpt-4o",
        "model_label": "GPT-4o",
        "request_count": 42,
        "success_rate": 97.62
      }
    ]
  }
}
```

Example `analytics.error` payload:
```json
{
  "type": "analytics.error",
  "channel": "analytics",
  "profile_id": 2,
  "preset": "24h",
  "code": "snapshot_failed",
  "message": "Failed to build analytics snapshot"
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

## 10. API Reference Source

This markdown document is the source of truth for current runtime and management API semantics.
agement API semantics.
urce of truth for current runtime and management API semantics.
agement API semantics.
