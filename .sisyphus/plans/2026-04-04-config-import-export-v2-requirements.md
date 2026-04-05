# Config Import/Export v2 Requirements

## Status

Active requirements draft.

## Decision Summary

Prism will replace the current `version: 1` config import/export contract with a breaking `version: 2` design built around two explicit ownership domains:

1. **Profile bundles** are authoritative only for profile-scoped state.
2. **Vendor catalog bundles** are authoritative only for global vendor metadata.

Profile bundles will no longer export plaintext endpoint API keys. Exported endpoint secrets will be emitted only as encrypted bundle secrets, and import will decrypt them during preflight before re-encrypting them into Prism's normal at-rest storage format.

## Why This Change Is Required

The current design has three structural problems:

- profile-scoped import still carries global `vendors[]` metadata and fails on shared-vendor collisions
- config export decrypts stored endpoint API keys back into plaintext JSON
- frontend validation mirrors only part of the backend contract and currently assumes plaintext `endpoints[].api_key`

These requirements define a replacement workflow rather than an incremental patch.

## Goals

- Make profile import/export portable without making profile bundles authoritative over global vendor rows.
- Remove plaintext API keys from exported bundles.
- Make import validation authoritative on the backend, including secret decryption and global-state checks.
- Keep import atomic: if preflight fails, the target profile remains unchanged.
- Align backend schemas, frontend types, frontend validation, and operator UX around one contract.

## Non-Goals

- Backward compatibility with `version: 1` bundles.
- Browser-side cryptography for config export/import.
- Partial import of valid sections when secret decryption or reference resolution fails.
- Using vendor `name`, `description`, or `icon_key` as stable identifiers.

## Ownership Model

### 1. Profile bundle scope

The profile bundle is authoritative for:

- endpoints
- pricing templates
- loadbalance strategies
- models
- connections
- profile settings
- non-system header blocklist rules

The profile bundle is **not** authoritative for global vendor metadata.

### 2. Global vendor catalog scope

The vendor catalog bundle is authoritative for:

- vendor `key`
- vendor display metadata
- vendor audit defaults

Global vendor metadata must move out of the profile bundle authority path.

## Required Bundle Types

### A. Profile bundle

The backend must expose a `version: 2` profile bundle with:

- `bundle_kind: "profile_config"`
- profile-scoped config data
- `vendor_refs` keyed by `vendor_key`
- encrypted secret payload metadata and entries

`vendor_refs` are reference hints, not authoritative global vendor updates.

### B. Vendor catalog bundle

The backend must expose a separate `version: 2` vendor catalog bundle with:

- `bundle_kind: "vendor_catalog"`
- authoritative global vendor metadata

The profile workflow and vendor-catalog workflow must be independent.

## Profile Bundle Contract

### Top-level shape

The new profile bundle must contain:

```json
{
  "version": 2,
  "bundle_kind": "profile_config",
  "exported_at": "2026-04-04T15:00:00Z",
  "vendor_refs": [
    {
      "key": "openai",
      "name_hint": "OpenAI",
      "icon_key_hint": "openai"
    }
  ],
  "endpoints": [
    {
      "name": "openai-main",
      "base_url": "https://api.openai.com",
      "api_key_secret_ref": "endpoint:openai-main:api_key",
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
        "ref": "endpoint:openai-main:api_key",
        "ciphertext": "enc:..."
      }
    ]
  }
}
```

### Required contract changes

- `vendors` must be replaced by `vendor_refs` in the profile bundle.
- `user_settings` must be renamed to `profile_settings`.
- `profile_settings` must include `timezone_preference`.
- exported connections must preserve `openai_probe_endpoint_variant` so export and import round-trip the same profile behavior.
- endpoints must no longer carry plaintext `api_key`.

## Vendor Resolution Rules

Profile import must resolve vendors by `vendor_key` only.

### Required behavior

- If `vendor_key` already exists globally, import must reuse the existing vendor row.
- If `vendor_key` does not exist globally, import may create a new vendor from the provided hints.
- If `vendor_key` exists and hint metadata differs, import must **not** fail and must **not** mutate the existing global vendor row.
- Preview must report when imported hints differ from existing global metadata.

This removes the current `audit_enabled`/`icon_key` collision behavior from the profile import path.

## Secret Handling Requirements

### Export

- Profile export must never include plaintext endpoint API keys.
- The backend must decrypt the stored endpoint secret, then re-encrypt it into the bundle `secret_payload`.
- If a stored endpoint secret cannot be decrypted, export must fail loudly. It must not silently emit an empty string.

### Import

- Import preview and import execution must decrypt every required `secret_payload` entry before any destructive mutation begins.
- Decrypted bundle secrets must then be persisted using Prism's normal at-rest secret encryption path.
- Missing secret entries, unreadable ciphertext, or wrong-key failures must fail the whole import.

### Secret payload requirements

- `secret_payload.kind` must be `encrypted` for profile bundles.
- `secret_payload` must contain `cipher`, `key_id`, and `entries`.
- each secret entry must carry a stable `ref` that matches a config object field such as `endpoint:<name>:api_key`
- the backend must validate that every `api_key_secret_ref` resolves to exactly one encrypted entry

## Encryption Key Requirements

Prism must introduce a dedicated config setting for bundle encryption and decryption.

### Required setting

- `CONFIG_BUNDLE_ENCRYPTION_KEY`

### Default behavior

- If `CONFIG_BUNDLE_ENCRYPTION_KEY` is unset, Prism must default it to `SECRET_ENCRYPTION_KEY`.
- The docs must explicitly state that this default is acceptable for local development convenience but should be overridden in real deployments.

### Key metadata

- The bundle must expose a non-secret `key_id` derived from the configured bundle key.
- `key_id` exists only for diagnostics and mismatch reporting. It must not reveal the raw key.

## API Workflow Requirements

### Profile workflow

The backend must replace the current single import path with:

- `GET /api/config/profile/export`
- `POST /api/config/profile/import/preview`
- `POST /api/config/profile/import`

### Vendor catalog workflow

The backend must add separate global vendor endpoints:

- `GET /api/config/vendors/export`
- `POST /api/config/vendors/import/preview`
- `POST /api/config/vendors/import`

Exact route names may change during implementation, but the profile workflow and vendor-catalog workflow must remain separate.

## Import Preview Requirements

The frontend must stop treating local Zod validation as the authoritative import gate.

The backend preview endpoint must return:

- bundle kind and version status
- counts for imported profile objects
- vendor resolution results
- secret decryption status
- blocking errors
- non-blocking warnings

Preview is required so the UI can show operator-facing failures before destructive import.

## Import Execution Semantics

Import execution must follow this order:

1. parse request
2. validate bundle version and bundle kind
3. validate schema shape
4. validate internal references
5. resolve vendors by `vendor_key`
6. decrypt all required secrets
7. build the complete mutation plan
8. acquire locks
9. replace profile-scoped state in one transaction
10. enqueue follow-up probes after commit

If any step before mutation fails, the target profile must remain unchanged.

## Atomicity and Idempotency

- Profile import must be atomic.
- Re-importing the same valid bundle into the same target profile must yield the same logical configuration.
- Wrong-key and unreadable-secret failures must not produce partial profile replacement.

## Frontend Requirements

- Remove frontend assumptions that `endpoints[].api_key` is plaintext.
- Update frontend types to the `version: 2` bundle shape.
- Keep frontend validation structural only: file type, JSON parse, bundle kind, and basic schema shape.
- Move authoritative import feedback to the preview endpoint response.
- Update the Backup section copy to describe encrypted secrets instead of plaintext-secret export warnings.

## Backend Requirements

- Replace the existing `ConfigExportResponse` and `ConfigImportRequest` contracts with `version: 2` profile and vendor-catalog bundle schemas.
- Remove profile import conflict checks that compare profile-bundle vendor metadata against existing global vendor fields.
- Fail export if stored endpoint secrets cannot be decrypted.
- Fail preview/import if bundle secrets cannot be decrypted with the configured bundle key.
- Preserve existing database-at-rest secret encryption behavior after import.

## Documentation Requirements

Implementation of this design must update the live docs that own the final contract:

- `docs/API_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- relevant backend/frontend AGENTS surfaces if ownership or route maps change

This requirements doc is the active design note, not the final source of truth.

## Migration Requirements

- Ship the new workflow as `version: 2` only.
- Do not keep the old `version: 1` import path in the steady-state design.
- If existing backup migration is needed, provide a one-off converter or migration note rather than dual runtime support.

## Acceptance Criteria

### Backend acceptance criteria

- Exported profile bundles contain no plaintext endpoint API keys.
- Export fails if any stored endpoint secret is unreadable.
- Preview reports vendor reuse vs vendor creation by `vendor_key`.
- Profile import does not fail when vendor hints differ from existing global vendor metadata.
- Wrong bundle key fails preview and import before mutation.
- Re-importing the same bundle reproduces the same logical profile configuration.

### Frontend acceptance criteria

- The settings backup UI no longer assumes or exposes plaintext exported API keys.
- Import uses preview before execute.
- Frontend types and Zod schemas match the new `version: 2` contract.
- Preview errors and warnings are rendered from backend responses.

### Contract acceptance criteria

- `timezone_preference` is part of the profile bundle contract.
- `openai_probe_endpoint_variant` round-trips through export and import.
- Profile bundle and vendor-catalog bundle are independent and explicitly typed by `bundle_kind`.

## Required Test Plan

Implementation must add or update tests that prove:

- encrypted secret export for endpoints
- wrong-key preview failure
- wrong-key execution failure with no profile mutation
- missing secret entry failure
- vendor reuse by `vendor_key`
- vendor creation when `vendor_key` is absent globally
- repeated import idempotency
- frontend preview-driven import UX
- frontend schema alignment with the backend `version: 2` bundle
