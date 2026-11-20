# Management API

The [shared HTTP reference](api-reference.md) defines profile context, request limits, and error envelopes. See the [observability API](observability-api.md) for statistics, history, audit, and retention endpoints.

## Resources and Settings

### Profiles

The profile management API is frozen. Prism preserves the `profiles` table and all `profile_id` storage columns, but no longer exposes `/api/profiles*` management routes. Profile-scoped management APIs are pinned to Default profile id `1`.

---

### Catalog Management

Prism exposes no standalone global catalog management surface (no catalog CRUD/navigation page); catalog management lives inside the per-model detail page (models.dev metadata, source-linked pricing, and the pi.dev template panel). Model compatibility is carried by each model's required `api_family`; catalog metadata does not participate in runtime routing.

---

### Model Configurations

#### List Models

```
GET /api/models
```

Response `200`: Array of model objects. Each item includes required
`direct_request_enabled`, `incoming_model_target_count`, and a non-persisted
`configuration_warnings` array. A non-entry model with no incoming Model Target
rows reports the explanatory `model_target_unreferenced` warning.

#### Get Model

```
GET /api/models/{id}
```

Response `200`: Full model object with required `api_family` and
`direct_request_enabled`, optional `loadbalance_strategy_id`, ordered
`access_targets` (Model Target and Terminal Target rows share one global
`position` order), `incoming_model_target_count`, and non-persisted
`configuration_warnings`. Model create/update does not author access targets;
use `/api/models/{id}/targets` for mixed access-target authoring and
model-owned Terminal Target ownership edges. `direct_request_enabled=false`
does not remove the node from the graph; it only removes the client-entry
qualification.

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
  "direct_request_enabled": true,
  "openai_accepted_format": "dual_native",
  "is_enabled": false,
  "initial_terminal_target": {
    "endpoint_id": 1,
    "name": "Primary production key",
    "upstream_model_id": "provider/model-a",
    "openai_text_capability": "dual_native"
  }
}
```

`initial_terminal_target` is optional. Omitting it creates only the model; when
present, the model, endpoint if created inline, Terminal Target, and owner edge
are written in one transaction and the response includes the created target
summary with its non-empty `upstream_model_id`. Omitting only
`initial_terminal_target.upstream_model_id` writes the new entry `model_id` as
the concrete default. Explicit `null`, blank, or a value longer than 200
Unicode characters rejects the whole composite create with `422` and no model,
endpoint, connection, or owner-edge write.

Response `201`: Created model object and, when requested, its initial Terminal
Target summary.

Validation rules:

- `model_id` must be unique within the effective profile scope. A create request requires `loadbalance_strategy_id`; updates cannot explicitly clear it.
- `api_family` is required on every model contract and remains the authoritative runtime compatibility field.
- `direct_request_enabled` is optional on create (omitted defaults to `true`) and optional on update (omitted preserves the stored bit); explicit JSON `null` or a non-boolean is rejected with `422` before any write. It is the only persisted entry qualification—there is no three-state entry/outbound/both enum. A false model remains a valid recursive Model Target node and reports `model_target_unreferenced` when no incoming model-target row exists.
- `is_enabled` defaults to `false` when omitted. Enabling a model still requires at least one enabled access target in the stored graph.
- Create and update payloads reject `access_targets`, exact-facade fields, and retired model-owned context capability fields.
- Ordered same-profile, same-`api_family`, same-`openai_accepted_format` model targets are managed through `/api/models/{id}/targets`. Cross-mode target authoring is rejected with `422 target_openai_mode_mismatch` (disabled targets included).
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
  "direct_request_enabled": false,
  "is_enabled": true
}
```

Update payloads use the same field contract as create and do not mutate access targets. Existing access targets and private Terminal Targets are preserved and remain managed by model-scoped target and connection routes. Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile. Changing `openai_accepted_format` that would break an existing mode-equal relation (own connection targets, outbound model targets, or inbound referrers) also returns `409`. Changing `direct_request_enabled` is a routing-affecting mutation and refreshes the last-good planning snapshot before the next request.

#### Delete Model

```
DELETE /api/models/{id}
```

Response `200`: `{ "deleted": true }`. Returns `409` if other models still reference this model through model targets. When deletion succeeds, the owner model's private connection rows and their internal owning access-target rows are removed in the same operation.

---

### Model Catalog Metadata (models.dev)

Catalog metadata is management-only projection data sourced from the fixed official models.dev catalog (`https://models.dev/api.json`, MIT License). It never participates in `api_family` compatibility truth, capability gating, routing, or the runtime snapshot; metadata writes never invalidate planning. The backend client is restricted: HTTPS only, same-origin redirects only, a 10-second whole-request timeout, a 16 MiB body budget, ETag/304 revalidation, single-flight fetches, `json.Number` decoding, and fail-closed schema validation. Remote I/O always happens outside database transactions; commits verify the operator's previewed catalog revision so stale source data cannot be written.

The HTTP-neutral catalog boundary is owned by `backend/internal/domain/modelsdev/` for transport, schema, matching, and pricing-plan decisions. Management workflow ownership remains split between the models catalog read/bind/refresh/override/unbind routes and the connections catalog-pricing preview/commit/assignment routes; only cached catalog snapshots cross into write transactions.

#### Get Model Catalog Binding

```
GET /api/models/{id}/catalog
```

Response `200`: `{ bound, match_source ("unique_match" | "manual"), provider_id, catalog_model_id, catalog_revision, fetched_at, updated_at, source, override, effective, auto_match? }`. `source` is the immutable snapshot refreshed only by explicit refreshes; `override` holds per-field manual values; `effective` merges override over source field by field. Unbound models return `bound: false` with null projections plus an `auto_match` hint computed from the cached catalog when available. `GET /api/models/{id}` embeds the same object as `catalog`. The source name never changes the model's `display_name`.

#### Match Preview

```
POST /api/models/{id}/catalog/match-preview
```

Fetches the catalog outside any transaction and reports exact-id matches across the api_family's canonical providers (`openai`→`openai`, `anthropic`→`anthropic`, `gemini`→`google`). Response `200`: `{ committable, provider_id?, catalog_model_id?, candidates[], reason ("unique_match" | "ambiguous" | "no_match"), catalog_revision, fetched_at }`. A unique exact match is committable; ambiguous or missing matches are preview-only.

#### Bind

```
POST /api/models/{id}/catalog/bind
```

Request: `{ provider_id?, catalog_model_id?, expected_catalog_revision, expected_prism_model_id, expected_api_family }`. With explicit coordinates both fields are required and must exist in the fetched catalog (`422 models_dev_offering_unknown` otherwise); without them the unique exact match is applied, while ambiguity or absence rejects with `409` plus the candidate list. A revision mismatch returns `409 models_dev_catalog_stale`. The write transaction takes the model row lock, re-verifies the confirmed Prism identity (`409 models_dev_model_changed` on a rename or family edit), then takes the binding row lock and re-resolves the carry-forward from the locked current row: a different offering clears prior manual overrides, the same offering keeps overrides and the original match source. After binding, refreshes never re-guess the coordinates.

#### Refresh Preview / Commit

```
POST /api/models/{id}/catalog/refresh/preview
POST /api/models/{id}/catalog/refresh/commit
```

Preview fetches the current catalog revision, diffs it against the stored source snapshot field by field (`changes[]` with `added`/`removed`/`changed`), and publishes the binding CAS snapshot: `provider_id`, `catalog_model_id`, `binding_updated_at`, `catalog_revision`, `fetched_at`. Commit requires `expected_provider_id`, `expected_catalog_model_id`, `expected_binding_updated_at`, and `expected_catalog_revision`, locks the model and binding rows, rejects with `409 models_dev_binding_stale` when the coordinate or token no longer matches, then replaces only the source columns plus revision/fetch stamp (`409 models_dev_not_bound` when unbound; `409 models_dev_offering_missing` when the bound offering vanished from the catalog — fail closed, nothing is wiped).

#### Manual Overrides

```
PUT /api/models/{id}/catalog/override
DELETE /api/models/{id}/catalog/override
DELETE /api/models/{id}/catalog
```

`PUT` accepts `{expected_provider_id, expected_catalog_model_id, override}`, where `override` is a sparse object of known metadata fields; each present key writes that override, and an explicit JSON `null` restores the field to its source value (per-field restore). Unknown fields, wrong types, out-of-range numbers, or non-enum statuses return `422`. The write confirms that the displayed coordinate is still bound, then merges over the locked current row and rewrites only the override columns plus the CAS token, so a stale draft cannot cross a rebind while concurrent same-offering sparse overrides of different fields never lose updates. `DELETE /override` clears every manual override only when the confirmed `{expected_provider_id, expected_catalog_model_id, expected_binding_updated_at}` snapshot still matches. `DELETE /catalog` uses the same snapshot to unbind (remove the binding row) without touching runtime identity, rejects `409 models_dev_binding_stale` when a concurrent rebind/refresh/override intervened, and stays idempotent once the row is gone. All are planning-neutral.

#### Bounded Candidate Search

```
GET /api/models/{id}/catalog/candidates?q=&scope=family|all&limit=&offset=
```

Response `200`: `{ items[] (provider_id, provider_name, model_id, name), total, limit, offset, scope, catalog_revision, fetched_at }` — every page publishes the snapshot revision and fetch stamp it was computed from, without fabricating a freshness enum. `limit` clamps to 100; `scope=family` restricts to the api_family's canonical providers and `all` covers every catalog provider for manual binding. All `/api/models/{id}/catalog*` responses — including this one, the match preview, and every error — carry `Cache-Control: private, no-store`; the model-detail `GET /api/models/{id}` response also carries it while the legacy embedded models.dev catalog field remains.

---

### Client Model Configuration Export

The export surface defaults to Pi 0.84.3 `prism-pi-models.json` (Pi `models.json` format) and separately supports OpenCode 1.18.27 `opencode-prism.json`, both from Prism-managed model truth. It is management-authenticated; `source` and `render` are read-only with respect to runtime state (no runtime cache invalidation, `M3` management lane), while the eight model-level Pi source routes below use the `M2` lane and the two additive migrations `000027_model_pi_catalog_bindings.sql`/`000028_model_pi_binding_identity.sql`. `source`, `render`, the model-level Pi routes, and all their error responses carry `Cache-Control: private, no-store`. The pi.dev structured directory (`https://pi.dev/api/models`) is the Pi template source; OpenCode reads only the persisted safe models.dev metadata described below. Upload enhancement, default-model management, `enhancements`/`default_model_config_id`/deprecated candidate wires, `clientConfigExtract` and `jsonc-parser` remain absent.

#### Client Readiness and Repair

Each Pi/OpenCode source model includes `readiness: {status: "ready"|"blocked", blocking_reasons: string[], repair_path: string}`. The management projection invokes that client's existing pure renderer for one model with a placeholder local origin and no credentials; it does no network I/O. This keeps source readiness and render gates identical, including required metadata, static operation reachability and the client's independent binding rules. `repair_path` points to `/route/models/{id}`; the OpenCode export page may mount the existing metadata editor in place and re-read source after a saved correction. Pi initial selection, toggles, bulk selection, refresh reconciliation and preset restoration require both structural `selectable` and `readiness.status=ready`; absent readiness fails closed. Unselected rows keep binding repair actions, and a newly repaired row is explicitly checked. The backend structural `selectable` field retains its separate meaning. The sampled readiness is not a runtime-health assertion. Price omission remains a separate warning, never zero-price evidence.

Pi catalog blocks in source, search and the per-model read expose `failure_code` on failure, with `fetched_at` and `checked_at` when available. Safe classifications are `timeout`, `format`, `version`, `checksum`, `too_large`, `cancelled`, and `unavailable`; successful reads clear the failure. Source readiness uses retained binding facts even when live catalog evidence is stale or unavailable. models.dev and Pi remote errors are sanitized inside their catalog owners and never echo response bodies, headers or credential-bearing URLs.

#### Pi Catalog, Transport and Candidate Matching

The pi.dev typed catalog is fetched only outside any DB transaction via restricted HTTPS (`4 s` timeout, same-origin `https` redirects, `16 MiB` budget, `Accept: application/json`, `ETag`/`If-None-Match` and `304` revalidation, `singleflight` coalescing, `last-known-good` retention, `X-Pi-Model-Catalog-Minimum-Version` validation against `0.84.3`, strict `Content-Type: application/json`, and HTML rejection; browsers never fetch pi.dev directly). The trusted `catalog_revision` is validated as the response body's own `SHA-256` (`sha256-…`): a missing or mismatched `X-Pi-Model-Catalog-Revision` header fails the fetch closed and is never satisfied by falling back to the `ETag`, which is a `304` validator only and carries no trust of its own. `200` with a matching revision returns a fresh catalog, `304` revalidates the existing `LKG`, transport/timeout/illegal-schema/version-too-new/revision-mismatch keep `LKG` as `stale`, and no `LKG` fails. Candidates are derived by complete `model_id` case-sensitive exact equality plus final Pi API compatibility (`openai-responses` for `dual_native`/`responses_only`, `openai-completions` for `chat_completions_only`, `anthropic-messages` for Anthropic, `google-generative-ai` for Gemini); no name/slug/contains/fuzzy. `0`/`1`/`many` states are explicit, special characters are preserved verbatim, `qwen3.8-flash` Chat has two `openai-completions` candidates (`qwen-token-plan`, `qwen-token-plan-cn`), Anthropic has one `opencode-go` (`anthropic-messages`), Responses has none, and `openrouter/qwen/qwen3.8-flash` does not match `qwen3.8-flash` — this fixture is `TestCatalogCandidatesExactCaseSensitiveIDPlusAPIOnly` in `backend/internal/domain/pidev/client_test.go`. A single candidate may be proposed for one-click bind; multiple candidates — even sharing one template — never auto-merge/first/lex/provider-preference and always require an explicit `provider_id`/`catalog_model_id` bind. `Catalog.SearchModelIDs` is the separate bounded directory discovery rule behind `POST .../pi/search`: model-id-only case-insensitive literal containment (provider id, display name, `baseUrl`, and every other field are never searched), no fuzzy/slug/token/edit-distance/wildcard expansion, restricted to entries whose `api` equals the model's current final Pi API, ranked exact then prefix then substring then provider id then model id byte-wise on the original case-sensitive values over the whole same-API hit set before the `offset` window is cut, defaulting to a 20-row page, clamping to 100, echoing the requested `offset` and the full match total, and returning equally-unselected hits — a one-hit search is still not a selection. The search response also carries `checked_at` next to `fetched_at`, keeping the content-download stamp and the last revalidation (304-included) stamp distinguishable. The case-sensitive `Candidates` rule above is untouched by it. Catalog `compat` is sanitized through an API-specific Pi 0.84.3 allowlist before persistence; unknown/cross-API keys and provider-routing/fallback directives are removed with sorted `dropped_fields` evidence, while manual overrides reject the same keys. `provider`/`baseUrl`/`cost` are validation-only and never retained downstream; `headers`, `samplingParams`, and unknown optional fields are likewise dropped as evidence, so none can override Prism identity, protocol, gateway, credential or pricing truth.

#### Persisted Pi Catalog Binding

`model_pi_catalog_bindings` (`000027_model_pi_catalog_bindings.sql`, purely additive, independent of the models.dev `model_catalog_bindings` table in [Model Catalog Metadata](#model-catalog-metadata-modelsdev)) holds at most one row per model: the frozen coordinate (`provider_id`, `catalog_model_id`, `api`), the frozen Prism identity `prism_model_id_at_bind` added by `000028_model_pi_binding_identity.sql`, `bind_source` (`single_candidate`/`manual`), the trusted `catalog_revision`, `fetched_at`/`updated_at`, `source_*`/`override_*` column pairs for the seven safe leaves `name`/`reasoning`/`input`/`context_window`/`max_tokens`/`thinking_level_map`/`compat`, and sorted `source_dropped_fields` evidence. It is the render authority: `source`/`render` read it directly and never re-derive a selection from a live fetch.

```text
GET /api/models/{model_config_id}/pi
POST /api/models/{model_config_id}/pi/bind
POST /api/models/{model_config_id}/pi/search
POST /api/models/{model_config_id}/pi/refresh/preview
POST /api/models/{model_config_id}/pi/refresh/commit
PUT /api/models/{model_config_id}/pi/override
DELETE /api/models/{model_config_id}/pi/override
DELETE /api/models/{model_config_id}/pi
```

`POST .../pi/search` accepts `{model_id_query, limit?, offset?}` (a negative `offset` is a `422`) and returns catalog status/revision, `fetched_at` and `checked_at`, Prism export identity, and bounded same-API candidate evidence that echoes the requested `offset` and the full `total`; a `stale` response is read-only evidence and never a bind authority. `GET /api/models/{model_config_id}/pi` is the single-model Pi management read: one best-effort `piCatalogForRead` fetch outside any transaction, then one profile-scoped read-only `REPEATABLE READ` transaction over the model row and its persisted binding, then the pure projection. It returns `{model (model_config_id, model_id, api_family, pi_api), catalog (status/revision/minimum_version/etag/fetched_at/checked_at), candidate_status, candidates, binding_status, binding_renderable, binding}` where a catalog outage degrades only the live evidence block (`status=unavailable`, `candidate_status=catalog_unavailable`) and never manufactures an unbound or non-renderable binding. It never loads export targets, pricing plans, source digests, credentials, render results, or any runtime graph, and it shares the live-candidate/binding-health projections with the export source so both surfaces compute identical axes. `bind` requires `expected_catalog_revision`, `expected_prism_model_id`, and `expected_pi_api`; it fetches and revision-checks the catalog outside the write transaction, then re-verifies those expectations and re-resolves the coordinate inside it under row lock, so every rejection writes nothing. With explicit `provider_id`/`catalog_model_id`, both fields are required and the pair must be a real entry in the fetched revision whose `api` equals the model's current final Pi API — a deliberate cross-directory `catalog_model_id` is therefore bindable, while an unknown pair rejects `422 pi_candidate_unknown` and a different-API pair rejects `422 pi_candidate_api_mismatch`. Without them the unchanged default flow decides: a unique complete-id-plus-API candidate applies automatically (`bind_source=single_candidate`), zero candidates reject with `422 pi_candidate_not_in_catalog`/`pi_candidate_api_mismatch`, and multiple candidates reject with `409 pi_candidate_ambiguous` plus the complete candidate list. A mismatched identity/API expectation rejects `409 pi_model_changed`. Every accepted bind also freezes the confirmed Prism model id into `prism_model_id_at_bind`. Rebinding the same coordinate with the same confirmed identity is idempotent and leaves its source, revision, overrides, stamps, and original `bind_source` unchanged; rebinding the same coordinate after a Prism rename preserves frozen source, overrides, revision, `fetched_at`, dropped evidence and `bind_source` while advancing the identity snapshot and `updated_at` CAS token; rebinding a different coordinate freezes that candidate and clears overrides. `refresh/preview` diffs the seven source fields plus dropped-path evidence and returns `provider_id`, `catalog_model_id`, `api`, `binding_updated_at`, and `catalog_revision`; `refresh/commit` requires the five matching `expected_*` values, locks and revalidates the current model/binding, re-resolves the coordinate against that revision, then replaces only source/dropped fields and stamps. A coordinate/version/revision change or missing/API-changed candidate returns `409` with no write. `override` `PUT` accepts a sparse object of the seven fields, each validated against the bound API through the same Pi 0.84.3 schema `RenderPi` enforces, with explicit JSON `null` restoring only that field to source; `DELETE /override` clears every override; `DELETE /pi` unbinds without touching runtime identity. All eight model-level routes are `Default-profile`, `M2`, planning-neutral.

#### Source, Render Fact Chain

Pi has two static `Default-profile` routes with `M3`, `planning-neutral`, `private, no-store`. OpenCode has its own literal source/render pair below; neither uses a variable-platform compatibility route or an interim `resolve` step.

```text
GET /api/models/exports/pi/source
POST /api/models/exports/pi/render
```

`source` does one best-effort pi.dev fetch outside any transaction and one `REPEATABLE READ` DB snapshot, returning only direct-request entries whose `direct_request_enabled = true`, each with two independently-computed evidence layers: live catalog evidence (`pi_candidates`/`candidate_status`: `not_in_catalog`/`api_mismatch`/`single`/`multiple`/`catalog_unavailable`, including sanitized templates and dropped paths) and persisted binding evidence (`pi_selected`, `pi_binding_status`: `unbound`/`bound`/`bound_drifted`, `pi_binding_renderable`, `pi_binding_prism_model_id`, `pi_api`, source/override/effective metadata, dropped paths, bind source, revision, and stamps), so one row can honestly report `not_in_catalog` and `bound`/renderable at the same time, alongside layered `prism`/`merged` metadata and provenance, pricing risk and warnings, the pinned `0.84.3` target version, and a clock-free `source_digest`. A successful live fetch marks the binding drifted when its coordinate/API vanished or its sanitized template/dropped evidence differs; catalog unavailability never invents drift. Such live drift remains renderable from the frozen row, while a mismatch between the current Prism full-model-ID/final-Pi-API pair and the binding's frozen `prism_model_id_at_bind` snapshot makes `pi_binding_renderable=false` — the directory model id is never the identity gate, so a deliberate cross-directory bind renders normally. `render` performs no network I/O and reads no live or LKG pi.dev state: it accepts only `expected_source_digest`, `model_config_ids`, `base_url`, `provider_id`, `credential`, and exactly one `selections` coordinate assertion per selected model. Missing assertions return `422 candidate_unselected`; extra, mismatched, or incompatible assertions return `422 candidate_invalid`; both carry `model_config_id`. A digest mismatch returns `409 export_source_stale` before rendering.

Source rows retain disabled or otherwise non-selectable direct entries with an explicit `unselectable_reason`: `model_disabled`, `no_accepted_text_operations`, or `no_reachable_terminal_target`. Pure image models do not export a coding-client API. Selection requires a statically routable primary conversation operation; `dual_native` exports through Responses, and the shared routing analyzer limits pricing evidence to Terminal Targets reachable for the exported client operation set. The export does not assess live Ban, admission, or upstream health.

The `source_digest` is `SHA-256` over canonical JSON of the sorted, clock-free fact set: target version, models, targets, pricing, Prism metadata, each model's bound `pi_selected` coordinate plus its frozen Prism identity snapshot and `catalog_revision`, and its effective `PiTemplate` including dropped-path evidence. It deliberately excludes the current live pi.dev fetch's revision/status/candidates, every directory-search result, and derived binding-drift status (`json:"-"` on `SourceFacts.PiCatalog` and the corresponding `ModelFact` fields): that transient fetch outcome never changes frozen rendered bytes. A different-coordinate bind/rebind, a same-coordinate bind that re-confirms a changed Prism identity, refresh commit, or override moves the digest; a same-coordinate same-identity bind is a no-op. Two coordinates therefore produce different digests even when their sanitized templates are identical.

Output preserves only safe pi.dev leaves `name`/`reasoning`/`input`/`contextWindow`/`maxTokens`/`thinkingLevelMap`/`compat`; Prism's `model_id`, `api`/`baseUrl` derived from the gateway origin, `provider`/`credential` and current pricing remain authoritative and pricing stays `fail-closed` across Terminal Targets statically reachable for the exported client operation set (`USD`/`PER_1M`, five components, `reasoning_price == output_price`, lossless tier mapping for Pi's single strict positive tier; `pricing_component_missing` on any null component including `reasoning_price=null`, plus distinct warnings for no template, currency/unit mismatch, target conflict, peak/valley and tier unrepresentable; explicit `"0"` alone means free). `pi.dev` headers/samplingParams/fallback/routing are never proxied into the file.

Credentials have only `include=false` (omit) or `include=true` with a manually entered, trimmed, non-empty string. Pi 0.84.3 rejects an empty `apiKey`; the UI blocks it and the backend returns `422 credential_api_key_required` before snapshot work. Export never reads stored endpoint keys and the typed value never enters source URLs, PostgreSQL, query caches, browser storage, logs, errors or warnings. All `source`/`render` and error responses are `private, no-store`; the rendered file ends with exactly one newline, `content_sha256` is the SHA-256 of those UTF-8 bytes, the response carries the fixed `prism-pi-models.json` name and `application/json;charset=utf-8` MIME, and full-content copy, Blob download and a true new-tab raw view reuse the same bytes. The browser may also copy a locally derived `{ "<provider_id>": { ...provider... } }\n` fragment for merging beneath an existing `models.json` `providers` object. Closing the result or leaving the route clears content and revokes its Blob URL.

The Pi target preserves its source controls without default-model/upload-enhancement controls, and honestly distinguishes live catalog `fresh`/`stale`/`unavailable`, `not_in_catalog`, `api_mismatch` and `multiple` evidence from persisted `unbound`/`bound`/`bound_drifted` binding status, shows the selected candidate's seven safe template fields and dropped paths before bind/rebind confirmation, surfaces refresh/override affordances per model, and keeps the provider-merge fragment. Bind and rebind share one 更换来源 dialog for every model whose final Pi API is determinable, keeping the default exact-candidate layer and the bounded directory-search layer visibly separate; nothing is preselected, a one-hit search stays evidence, the Prism-owned final export identity is shown before confirmation, and confirm is blocked unless the catalog is `fresh`. The exported file's provider key is labelled 导出 Provider 键名 and a binding's directory origin 目录 Provider, so the two domains are never both called `Provider ID`; row actions beyond three use an overflow menu.


#### OpenCode 1.18.27 Source and Render

`internal/domain/modelexport/` owns the independent OpenCode fact digest, safe metadata projection, target-specific price gates and deterministic renderer; it uses no database, HTTP transport or client SDK dependency. `internal/httpapi/management/models/export_opencode_*` owns snapshot queries, ingress addressability, typed requests and responses. It reuses the current export eligibility/routing/price facts, but does not load Pi bindings or a live models.dev/pi.dev catalog. `internal/platform/http/management_route_specs.go` assigns both routes Default profile `1`, `M3`, private/no-store and no planning invalidation:

```text
GET /api/models/exports/opencode/source
POST /api/models/exports/opencode/render
```

Both handlers read all model, route/target, current price and saved source/override metadata facts inside a single read-only `REPEATABLE READ` transaction per request. Source returns `{target_version, source_digest, models[], warnings?}`. Rows contain Prism identity and state, `selectable`/`unselectable_reason`, `npm`, `api_path`, current `targets`/`price_risk`, and these evidence objects:

| Field | Contract |
| --- | --- |
| `source_metadata`, `override_metadata` | Persisted safe catalog leaves, with absent values omitted and explicit false/zero/empty arrays retained |
| `merged_metadata` | Valid final export values in the same catalog leaf names |
| `metadata_provenance` | Per final leaf: `prism_display_name`, `models_dev_source`, `models_dev_override`, or `model_id` |
| `missing_metadata` | Leaves with no valid final value; absence is not zero |
| `metadata_issues` | `{field, reason, source}` entries identifying missing or invalid inputs and limit-group conflicts |

Only `name`, `family`, `release_date`, `attachment`, `reasoning`, `tool_call`, `temperature`, `modalities_input`, `modalities_output`, `limit_context`, `limit_input` and `limit_output` are queried from `model_catalog_bindings`. A present manual override takes precedence; an invalid override remains visible as invalid rather than silently exposing the source value. A non-empty Prism display name overrides catalog naming; otherwise a valid effective catalog name is used, falling back to the unchanged model ID. The renderer nests `modalities_*` and `limit_*` into the target schema. It does not import directory model IDs, SDKs, prices, options, variants, interleaved reasoning, Pi templates or client credentials.

Context/output must be positive integers no greater than `9007199254740991`; optional input must be a non-negative safe integer. Input and output individually cannot exceed context; they are not required to sum to context. Missing/invalid required limits, an invalid optional input, or a group conflict produces `invalid_metadata_limits` and blocks selection. Explicit input zero can be represented in the file, although OpenCode's budget consumer can apply its own zero-value fallback. Boolean fields preserve false; missing capability fields remain omitted even when OpenCode later assigns a default.

Eligibility first uses the existing enabled direct-entry and statically routable primary-text-operation gates. The full unchanged ID must also survive the client's URL construction and Prism's registered operation matcher. Body-based OpenAI/Anthropic IDs preserve `/`; Gemini path IDs that cannot do so return `unaddressable_model_id`. This adds an export eligibility explanation without widening runtime protocol support. It does not assess live upstream availability, admission or Ban state.

| Prism text contract | Per-model `provider.npm` | Per-model `provider.api` |
| --- | --- | --- |
| OpenAI `chat_completions_only` | `@ai-sdk/openai-compatible` | gateway origin + `/v1` |
| OpenAI `responses_only` or `dual_native` | `@ai-sdk/openai` | gateway origin + `/v1` |
| Anthropic Messages | `@ai-sdk/anthropic` | gateway origin + `/v1` |
| Gemini | `@ai-sdk/google` | gateway origin + `/v1beta` |

These SDKs are built into [OpenCode 1.18.27](https://github.com/anomalyco/opencode/blob/4b7e19e315cca414121ba1d61523fef74bb3ae8b/packages/opencode/src/provider/provider.ts); no Prism dependency is added. A single custom provider can mix them. The file uses singular `provider`, with key `prism` or `prism-` followed by a lowercase letter/digit and then lowercase letters, digits, `_` or `-`. An omitted key defaults to `prism`. It sets neither a global `options.baseURL` nor a per-model `id` override; authored model-map keys are the complete Prism IDs.

OpenCode supports only the base cost fields consumed by that version: `input`, `output`, `cache_read`, `cache_write`. They are emitted only when every reachable target has the same standard USD/PER_1M price with all five Prism components configured and reasoning equal to output. Explicit zero is preserved. Any tier (including 200k), peak/valley shape, missing/conflicting component, currency/unit mismatch or reasoning differential omits the entire object with stable warning codes. No `tiers` or `context_over_200k` pricing is emitted; omission can appear as a zero estimate in OpenCode and is not a free-price assertion.

Render accepts exactly:

```json
{
  "expected_source_digest": "<digest returned by source>",
  "model_config_ids": [3],
  "base_url": "http://127.0.0.1:8080",
  "provider_id": "prism",
  "credential": { "include": false }
}
```

`model_config_ids` must be non-empty and refer to currently selectable entries; duplicates are normalized and order is deterministic. An origin must be HTTP(S), with no path, user information, query or fragment. Unknown request fields or malformed JSON return `400`; invalid selections, origin/provider parameters and credential combinations return `422`. There are no Pi coordinate assertions in this request. Manual credentials use `{"include":true,"api_key":"<final non-empty value>"}` with whitespace trimmed; a non-empty key sent with `include=false` is rejected. No-key output contains only `env:["PRISM_API_KEY"]`; manual output carries `options.apiKey`. Neither mode reads any stored key.

The clock-free digest includes the target version, normalized models and reachable targets/prices, plus effective metadata, provenance and validity issues. It excludes Pi facts, catalog availability, binding coordinates/timestamps, unrelated metadata and source values shadowed by valid overrides. A consumed-fact change returns `409 export_source_stale` before rendering, and the caller must refresh source. Source and render are read-only; neither mutates a binding or runtime snapshot.

The response is `{target_version, source_digest, content, content_sha256, file_name, mime_type, model_results, warnings?}`. `file_name` is `opencode-prism.json`, MIME is `application/json;charset=utf-8`, and `content` is deterministic UTF-8 JSON with exactly one terminal newline. The hash covers those exact bytes. Because [OpenCode substitutes configuration variables before parsing JSON](https://github.com/anomalyco/opencode/blob/4b7e19e315cca414121ba1d61523fef74bb3ae8b/packages/opencode/src/config/variable.ts), literal `{env:...}` and `{file:...}` in IDs, names or final keys encode the opening brace as `\u007b`; JSON parsing restores the literal value without variable/file expansion. The provider merge fragment preserves the same escaping. Full copy, Blob download and new-tab viewing reuse `content`; closing, replacing, switching or leaving clears the result and revokes Blob URLs. Credentials never enter source responses, URLs, Prism databases, query/HTTP caches, browser storage or logs.

The UI mounts the existing metadata editor in the export context for corrections, preserves valid selections after an authoritative source re-read, and shows all final values, provenance and missing/invalid reasons. OpenCode can apply SDK/model-ID-based inference defaults after loading; the exporter does not claim authored custom reasoning adaptation. See [Operations](operations.md#client-configuration-export-quick-guide) for loading and provider-merge instructions. Package-local renderer tests, management contract tests and the existing browser journey cover their respective boundaries; `tests/runtime/opencode_*_test.go` provides the separately invoked real-client round-trip boundary documented in [CONTRIBUTING](../CONTRIBUTING.md#real-opencode-export-acceptance).

---

### Bounded Batch Maintenance

```text
POST /api/models/batch/model_limits/preview
POST /api/models/batch/model_limits/apply
POST /api/models/batch/model_strategy/preview
POST /api/models/batch/model_strategy/apply
POST /api/models/batch/target_pricing/preview
POST /api/models/batch/target_pricing/apply
```

All routes use Default profile `1`, management `M2`, and `private, no-store`. A request contains `items` (1–20 unique positive ids), `confirm_manual_overrides`, optional `preview_token` and action-specific inputs. `model_limits` uses model ids with explicit positive safe-integer `context_limit`/`output_limit` (`output <= context`); it accepts only omitted or `opencode` client and writes the existing models.dev overrides without changing Pi bindings. `model_strategy` uses model ids and a positive strategy `reference_id`. `target_pricing` uses Terminal Target ids and a positive pricing-template `reference_id`. Mixed-action fields reject.

Responses contain `action`, `preview_token`, `can_apply`, `applied`, and per-item `id`, `label`, `before`, `after`, `manual_override`, optional `error`. Preview hashes the normalized input and locked authoring facts, including owner identity, binding/update tokens and referenced policy/template revision. Apply revalidates the whole batch under deterministic owner locks, checks the token and explicit replacement confirmation, then executes all writes in one transaction. A stale preview is `409`; an invalid matching preview or unconfirmed manual replacement is `422`; neither partially writes. The returned applied values are authoritative transaction readback, and consumers invalidate related collections before continuing.

Pricing assignment shares the existing profile serialization lock before model/target locks, preserving the pricing writer lock order. It retains native text equality/image containment validation even for disabled targets, and increments pricing-reference generation in the same transaction. Strategy application invalidates planning and route-witness generations; pricing application invalidates planning; metadata-only limits are planning-neutral. No schema migration or history rewrite is involved.

---

### Endpoints (Profile-Scoped Credentials)

#### List Endpoints

```
GET /api/endpoints
```

Response `200`: Array of endpoint objects in the effective profile scope, ordered by `lower(name) ASC, name ASC, id ASC`.

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

Optional `expected_updated_at` is an RFC3339 optimistic-concurrency guard mirroring the pricing-template contract: when supplied and different from the stored row `updated_at`, the backend returns `409` with a structured `detail` containing `code="endpoint_stale"`, a message, and the current endpoint state for immediate refresh. When omitted, behavior is unchanged (last write wins).
Response `200`: Updated endpoint object.

#### Delete Endpoint

```
DELETE /api/endpoints/{id}
```

Response `200`: `{ "deleted": true }`.
Returns `409` if any connections still reference this endpoint.

### Terminal Targets and Model Access Targets

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
  "upstream_model_id": "provider/model-a",
  "custom_headers": {
    "X-Custom-Org": "org-123"
  },
  "custom_request_parameters": {
    "provider": {
      "only": ["deepinfra/turbo"],
      "allow_fallbacks": false
    }
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
  "upstream_model_id": "provider/model-a-eu",
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
- `upstream_model_id` is optional only on create. Omission writes the owner model's current entry `model_id` as a concrete value. A supplied value is trimmed at its leading and trailing whitespace only; case, slashes, and internal characters are preserved. Explicit `null`, blank, or more than 200 Unicode characters returns `422` with `field=upstream_model_id`, `reason=required|too_long`, and `limit=200` for the latter. The transaction writes nothing on validation failure.
- `priority` is rejected with `422`; Terminal Target ordering for a model is owned by `/api/models/{model_config_id}/targets` positions.
- Limiter fields are optional. `null` means unlimited. Positive integers apply per-connection request admission limits.
- `openai_text_capability` is the OpenAI text runtime capability source of truth for OpenAI-family Terminal Targets. It accepts `responses_only`, `chat_completions_only`, or `dual_native` and must equal the owner model's `openai_accepted_format`, including matching absence on image-only rows (strict text mode equality). At least one of the text or image dimensions must be present. Non-OpenAI rows must omit it or persist `null`. Cross-mode authoring is rejected with `422`; changing a capability that would break an existing relation is rejected with `409`.
- `custom_request_parameters` is an optional static top-level JSON object (`object | null`). Missing, `null`, and `{}` all persist as unconfigured (`NULL`); a non-empty object is validated (protected keys, 64 KiB compact limit, depth ≤ 16, members ≤ 256, safe integers) and canonicalized before write. Invalid values return `422` with `{"detail":"Invalid custom request parameters","field":"custom_request_parameters","path":...,"reason":...,"limit":...}`; malformed request JSON or unknown fields keep the generic `400`.
- `routing_schedule` is an optional `{timezone, windows[]}` object (`object | null`). Missing and `null` both persist as unconfigured, which means no time restriction and byte-for-byte the pre-feature routing behaviour. A supplied object is validated and normalized before write: at most 32 windows, each with an ISO weekday bitmap (bit0 = Monday, 1–127), a `start_minute` in 0–1439 and an `end_minute` in 1–2880 that is greater than `start_minute` by at most 1440. An `end_minute` above 1440 continues into the next day, and `weekday_mask` names the day the window opens on, not every day it covers. A configuration whose windows together cover the whole week is rejected, because "always available" is expressed by having no schedule at all. Invalid values return `422` with `{"detail":"Invalid routing schedule","field":"routing_schedule","path":...,"reason":...,"index":...}`; an over-length timezone returns `400` and a server without a resolvable timezone database returns `503`, since that is a deployment gap rather than caller input.

#### Update Terminal Target

```
PATCH /api/models/{model_config_id}/connections/{connection_id}
```

Request: Mutable compatibility connection metadata: `endpoint_id`, `endpoint_create`, `is_active`, `name`, `auth_type`, `upstream_model_id`, `custom_headers`, `custom_request_parameters`, `routing_schedule`, `openai_text_capability`, `openai_image_capability`, `pricing_template_id`, `qps_limit`, `max_in_flight_non_stream`, `max_in_flight_stream`. `auth_type` accepts `openai`, `anthropic`, `gemini`, or `gemini_api_key`; it is independent of `api_family` and selects only the upstream credential scheme.

`upstream_model_id` is presence-aware: omission preserves the stored value,
while explicit `null`, blank, or over-200-character input is rejected with the
same field error as create. It cannot be cleared. Renaming the owner model does
not rewrite existing Terminal Targets; only targets created after the rename
default from the new entry ID.

`custom_request_parameters` is a presence-aware whole-value replace: omitting the field keeps the current value, `null`/`{}` clears it to `NULL`, and a non-empty valid object replaces it wholesale; any violation fails the whole PATCH atomically.

`routing_schedule` follows the same presence-aware contract: omitting the field keeps the current configuration and leaves the stored window rows untouched, `null` clears the timezone and deletes every window row, and an object replaces the whole configuration. Windows are never merged, because a wire window carries no stable identity that a merge could match against a stored row. The timezone here is the Terminal Target's own routing clock and is unrelated to `user_settings.timezone_preference`, which only affects how timestamps are displayed and never changes which upstream serves traffic.

`pricing_template_id` is CAS-guarded: sending it requires both `expected_connection_updated_at` (the connection `updated_at` the client last read) and `expected_pricing_template_id` (its current template reference, `null` when unpriced). Missing either field returns `422` with `{"pricing_cas_required": ["expected_connection_updated_at", "expected_pricing_template_id"]}`; a drifted timestamp or template reference returns `409`. Clients that do not move the pricing reference must omit all three fields.

`endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`. `priority` is rejected with `422`. The owner model and connection `api_family` are immutable.

Response `200`: Updated Terminal Target object, represented as a compatibility connection. Public `PUT` or `PATCH /api/connections/{connection_id}` rejects mutation requests.

#### Copy Terminal Target

```
POST /api/models/{model_config_id}/connections/{connection_id}/copies
```

Request:

```json
{
  "destination_model_config_ids": [8, 9],
  "enable_copies": false
}
```

The batch is transactional and validates every destination before inserting
any copy. Each copied Terminal Target preserves the source
`upstream_model_id` exactly; it never substitutes the destination model's
entry ID. The redacted `connection_summary` in every response item includes
that non-empty `upstream_model_id` alongside counts and non-secret metadata.

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

Pricing templates are assigned through the Terminal Target update route by setting `pricing_template_id` together with its two CAS fields. Public connection-level pricing-template mutation routes reject writes.

#### Delete Terminal Target

```
DELETE /api/models/{model_config_id}/connections/{connection_id}
```

Response `200`: `{ "deleted": true }`.

Deletes the Terminal Target and its internal owner access-target row together, subject to enabled-model target validation. Public `DELETE /api/connections/{connection_id}` rejects mutation requests.

Rejected legacy mutation routes return `400` with guidance to use model-scoped Terminal Target routes instead: `POST /api/connections`, `PUT/PATCH/DELETE /api/connections/{connection_id}`, `PUT /api/models/{model_config_id}/connections/{connection_id}`, and `PATCH /api/models/{model_config_id}/connections/{connection_id}/priority`. None of these 400 stubs ever advances a runtime-cache generation, because they never return `2xx`.

#### Model Target Routes

```
GET /api/models/{model_config_id}/targets
POST /api/models/{model_config_id}/targets
PUT /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}/position
DELETE /api/models/{model_config_id}/targets/{target_id}
```

Model target rows define a model's ordered access graph. Public authoring creates same-family model targets only; internal connection rows (Terminal Targets) share the same global mixed list and position space:

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
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position` is the dedicated move route and accepts `to_index`. `to_index` is the zero-based index of the complete mixed list, so an adjacent cross-type move is identical to a same-type move; the response returns the full reordered list.
- Existing internal `target_type="connection"` rows identify the source model that owns a private connection and provide the runtime terminal routing edge.
- Target positions are contiguous starting at `0` across both target types and determine routing order for that source model. Position is an ordering key only, not a priority, tier, or weight replacement. Enable/disable never moves a row; delete compacts positions across both types; creates append to the global mixed tail unless an explicit position inserts at that global index.
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

### Static Routing Diagnostics

```
GET /api/models/{model_config_id}/routing-diagnostics
```

A read-only static analysis of the authored routing graph for one model. It is
pure: it never reads Ban Policy state, retry windows, QPS or in-flight counters,
current-state or round-robin cursors, and it never contacts an upstream. What it
answers is "could this configuration route this operation at all", not "is this
upstream healthy right now".

The analyzer applies the model's strategy to the same single mixed peer sequence
the runtime uses — Model Target and Terminal Target rows share
`model_access_targets.position` and are numbered once across that list. A `single`
strategy therefore truncates that one list, not each target type, and a row the
strategy does not reach reports `truncated_by_single` regardless of which kind of
target it is. The models list embeds a compact `routing_summary` from the same
analyzer, whose `single_truncated_access_target_ids` names the rows the strategy
drops.

Per-target dispositions distinguish why a row cannot serve an operation:
`candidate`, `disabled`, `inactive`, `incompatible`, `no_eligible_leaf`,
`truncated_by_single`, `structural_error`, and — for a Model Target chain that
the analyzer refuses to walk — `cycle` and `depth_exceeded`. The Model Target
walk is bounded and never revisits a model on the current path, because the
analyzer runs on whatever the database holds rather than only on graphs that
passed write-time validation.

Coverage is a separate axis and exists only for Terminal Target rows: a Model
Target declares no capability of its own, so its `coverage` is empty and its
effective capability is whatever its subtree resolves to. Non-OpenAI families
carry no capability matrix and report `not_applicable`.

The field set is fixed by `backend/tests/contract/routing_diagnostics_contract_test.go`
under CI; this section describes intent rather than restating the payload.

Setup/readiness route witnesses use the model-bound projection of the runtime
operation registry as their sole operation catalog. OpenAI, Anthropic, and
Gemini therefore share one family-aware graph traversal without copying route
names into the domain. The graph retains every enabled model node so recursive
Model Targets remain diagnosable, but profile/setup roots, the representative
witness, and selected witness resolution are filtered to
`direct_request_enabled=true`. A witness carries the Terminal Target id, its
actual Endpoint id, lowercase `full|partial|none` coverage, and the optional
routing-schedule qualifier. Non-positive identities and cross-family chains
fail closed. The frontend preserves this projection and the list's
authoritative `routing_summary`; model or target mutations refresh both
diagnostics and the server-computed list/count projection.

### Sampled Route Explanation

`GET /api/models/{model_config_id}/route-explanation?operation={native-operation-name}` is a Default-profile `M1`, `private, no-store` observation. The operation must be a model-bound entry in the runtime operation registry and match the requested model. The handler uses the already-published runtime snapshot without triggering a refresh. `generation` and `published_at` identify that process-local snapshot, not the latest database commit or route-witness generation.

The response carries `observed_at`, `sample_completed_at`, `model_config_id`, `operation`, `completeness` (`sampled` or `partial`), `boundary`, optional `planner_error`, `candidates` and `exclusions`. Candidates include `path`, owner `strategy`/`model_config_id`/`model_id`, `terminal_target_id`, nullable `planner_position`, `schedule`, `runtime_observed`, `reason`, and `capacity`. Exclusions retain the path, model identity, target type and reason. Missing hot-state observations do not become zero occupancy or known availability.

The runtime owner copies existing state under read locks and samples cursor values into an independent store. The actual recursive planner runs on that private copy; its normal cursor operations never touch the live store. Schedule and admission-rejection rules evaluate the sampled input, with no capacity reservation. Work is bounded to 256 expanded graph edges, depth 32 and 4,096 existing state rows/cursors; unavailable or over-budget reads fail visibly. Independent state fragments are not an atomic live snapshot, and no next-request target is guaranteed. No upstream request, live registration, round-robin advance, ban change or audit/request record is created by explanation.

---

### Pricing Templates

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

Request uses a strict discriminated shape. `template_kind` is required and the three variants are mutually exclusive:

```json
{
  "name": "GPT-4o Standard",
  "description": "Operator-authored pricing",
  "template_kind": "standard",
  "card": {
    "input_price": "5",
    "output_price": "15",
    "cached_input_price": null,
    "cache_creation_price": "0",
    "reasoning_price": "15"
  }
}
```

`tiered` uses `base_card` and `tier.input_tokens_above` plus `tier.card`; `peak_valley` uses `peak_card`, `offpeak_card`, and `schedule` (`timezone` is an IANA name and `windows` are `[start_minute,end_minute)` rows). Every card requires input/output and preserves the same configured/NULL specialty shape across the complete card set. `peak` and `offpeak` are role labels, not a price ordering. Zero windows are rejected; an all-week window is valid and means the peak card is always selected. Standard and tiered revisions cannot carry schedule fields or window rows.

Response `201`: Created typed pricing template object. The database source of truth is `pricing_template_cards` keyed by `(revision_id, card_role)` and `pricing_template_windows`; old revision price columns are removed by destructive migration `000023_pricing_template_kind_cards.sql`. `000023` is fresh-only: any retained pricing or currency-migration rows fail before DDL with a readable rebuild error and the transaction rolls back.

Explicit JSON `null` means an unconfigured specialty component; explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` never means zero. A kind change is a complete retype, requires the full target shape and `expected_updated_at`, and creates a new immutable revision.

#### Catalog Price Preview / Commit (models.dev)

```
POST /api/pricing-templates/catalog/preview
POST /api/pricing-templates/catalog/commit
```

Both routes answer `Cache-Control: private, no-store` with the auth/profile `Vary` set because a preview carries state-bound catalog, linked-template, and Terminal Target evidence that intermediaries must not retain. Any relevant pricing-state change invalidates the hash. Unchanged template-only reuse is deliberately idempotent with respect to template and target state, so the preview is not described as a server-enforced one-time token.

Source-linked pricing import from the fixed official models.dev catalog. Preview resolves the offering from a bound `model_config_id` or an explicit `provider_id` + `catalog_model_id` pair, builds the price plan outside any transaction, finds the single **live** source-linked template for that offering (partial unique index that excludes `deleted_at`), flags drift when the template's current shape differs from the planned shape, captures each requested Terminal Target's CAS state, and returns `preview_hash` over offering + revision + plan + template identity + target CAS columns. An optional `model_config_id` may accompany explicit coordinates; it is echoed as display-only `model` evidence and deliberately stays out of the hash because it cannot change what the commit writes. `connection_ids` may be empty, which previews and commits a template-only import that assigns nothing. The response also fixes the unit contract explicitly as `catalog_currency: "USD"` plus `pricing_unit: "PER_1M"`, never converted. The plan fails closed with stable reasons (`reporting_currency_not_usd`, `cost_missing`, `price_not_representable`, `audio_cost_present`, `multiple_tiers`, `tier_not_supported`, `legacy_tier_shape`, `tier_evidence_conflict`, `specialty_shape_mismatch`) and zero writes whenever prices are not losslessly representable; only an OpenAI single `context` tier maps its `size` verbatim onto `input_tokens_above`. A row without `cost.reasoning` plans that card's reasoning price at the row's output price, the models.dev consumer convention, while an explicit value, including zero, is kept. Representation, legacy-tier, and specialty-parity checks judge the raw rows before this fallback, so reasoning evidence on only one tiered row still fails closed as `specialty_shape_mismatch`. Commit replays everything inside one transaction against the cached snapshot (no remote I/O): a stale revision or moved state rejects with `409`; incompatible plans reject with `422` and zero writes; drift requires explicit `confirm_drift`; creating a linked template records its first `revision_source='catalog'` revision, confirmed price drift appends another carrying `catalog_revision`, and unchanged reuse creates no artificial revision. Terminal Target assignment enforces the existing double CAS (`updated_at` + `pricing_template_id`) under sorted row locks, rolling back any opened revision, mutation ledger, generation update, and earlier assignment when a later target conflicts. Template creation or drift advances `pricing_template_generation`; unchanged reuse does not. Independently, the platform route contract advances runtime planning-cache generations and schedules a refresh after every successful catalog commit, including a template-only reuse that leaves pricing state unchanged.

The template and revision read surfaces project that provenance instead of hiding it: `GET /api/pricing-templates`, `GET /api/pricing-templates/{id}`, and the bounded list page carry `catalog_provider_id` + `catalog_model_id` (both null on a manual template, and a half-populated pair fails closed as `pricing_template_catalog_evidence_incomplete`) plus the current revision's `revision_source`/`catalog_revision`, and `GET /api/pricing-templates/{id}/revisions` carries `revision_source`/`catalog_revision` per append-only revision. Because uniqueness is scoped to live rows, soft-deleting a source-linked template releases its coordinates: re-importing the same offering creates a **new** live template while the retired row, its revisions, cards, and historical references survive unchanged.

Successful catalog commits return both `template_id` and the authoritative stored `template_name`, including any collision suffix chosen during creation, so callers never announce an inferred coordinate as the saved template name.

#### Import Pricing Templates

```
POST /api/pricing-templates/import
```

The import envelope is schema-versioned (`schema_version: 3`) and contains the same typed template shapes as create. Unknown fields, missing card keys, blank prices, and legacy flat price fields are rejected. Preview returns each row's `template_kind`, action, current/next version, `template_kind_changed`, `pricing_structure_changed`, and a hash over the complete normalized payload plus current revision/CAS identity. Commit locks the profile, recomputes that same preview inside the write transaction, and accepts only the exact hash before writing the whole batch atomically. Failed or drifted rows do not advance the planning generation.

#### Update Pricing Template

```
PUT /api/pricing-templates/{id}
```

Update preserves the current kind when `template_kind` is omitted. A same-kind shape edit or a retype replaces the complete normalized card/window shape; omitted kind-specific fields are not silently merged into another kind. Retyping requires `expected_updated_at`; stale CAS returns `409`. Any kind/card/threshold/timezone/window change creates an append-only revision and bumps the pricing generation. Metadata-only name/description changes do not create a price revision.
Response `200`: Updated typed pricing template object.

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

Response `200`: Usage payload with `template_id` and `items[]` (`connection_id`, `connection_name`, `model_config_id`, `model_id`, `endpoint_id`, `endpoint_name`)
---

### Settings API

#### Get Auth Settings

```
GET /api/settings/auth
```

Response `200` is the v2 control-plane projection: `revision`, `auth_mode` (`desired`, `effective`, `access_state`, and monotonic generations), effective/desired operator-account state, `transition`, `proxy_key_readiness` (server-clock `counted_at`, active/expired/disabled counts, and a 30-second `safe_active` guard), attribution mode, and `updated_at`. Passwords and one-time proxy-key secrets are never returned.

#### Update Auth Settings

```
PUT /api/settings/auth
```

Request fields:

- `operation_id` (RFC 4122 UUIDv4 intent; replay identity, never a secret)
- `expected_revision`
- `desired_auth_enabled`
- `account_change` (`preserve` or `update`; update carries `username` and optional `new_password`)
- `expected_proxy_key_readiness_generation` when enabling
- `acknowledgements` for zero safe keys, permissive disable, and operator-session invalidation

Lifecycle contract:

- Disabling auth clears the current browser cookies in the response and invalidates stale management sessions immediately.
- Changing the operator username or password invalidates stale management sessions immediately, even when auth remains enabled.
- After invalidation, `GET /api/auth/session` returns `401`, while `GET /api/auth/status` continues to report the live global auth mode.
- The write stages an immutable config version and returns an operation status URL; the effective pointer is flipped only after the final conditional proof. Exact operation replay returns the durable result. Persisted fail-closed transitions (`enabling_fail_closed`, `rollback_required`) block ordinary management with the registered typed `503` and are settled through the operation status endpoint; `disabling_enforced` keeps the old enabled gate until the runtime snapshot adopts the change. Auth staging and proxy-key mutation share the Proxy readiness fence and the affected Requests/Audit writer admission.

#### Proxy API Keys

```
GET /api/settings/auth/proxy-keys
POST /api/settings/auth/proxy-keys
PATCH /api/settings/auth/proxy-keys/{id}
POST /api/settings/auth/proxy-keys/{id}/rotate
DELETE /api/settings/auth/proxy-keys/{id}
```

Proxy-key lifecycle contract:

- List responses are arrays of proxy-key items with `id`, `name`, `key_prefix`, `key_preview`, `is_active`, `expires_at`, `last_used_at`, `last_used_ip`, `notes`, `rotated_at`, `rotation_count`, `created_at`, and `updated_at`.
- Create accepts `name`, optional `notes`, and optional RFC3339 `expires_at`. Response `201` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Update requires a non-empty `name` and accepts optional `notes`, `is_active`, and RFC3339 `expires_at`. Response `200` is the updated item. Omitted or JSON `null` `expires_at` preserves the current expiry; update does not expose a clear-expiry operation.
- Rotate is in-place secret replacement, not lineage creation: the same row keeps its `id`, `name`, `notes`, creator, active state, expiry, and `created_at`, only the secret and its derived `key_prefix`/`key_preview` change, `rotation_count` increments, `rotated_at` records the instant, `last_used_at`/`last_used_ip` are cleared because they described the retired secret, and response `200` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Delete returns `{ "deleted": true }`.

`GET ...?include=setup_readiness&expected_route_witness_generation=<generation>` uses the same Proxy-owner counted readiness snapshot and 30-second safe-active horizon as auth enablement, then joins it with the route-witness owner snapshot. It is a read-only handoff and returns a typed conflict when the requested witness generation is stale.

#### Get Costing Settings

```
GET /api/settings/costing
```

Response `200`:

```json
{
  "profile_id": 1,
  "report_currency_code": "USD",
  "report_currency_symbol": "$",
  "timezone_preference": "Europe/Helsinki",
  "reporting_currency_epoch": 1,
  "currency_effective_at": "2026-01-01T00:00:00Z",
  "pricing_migration_state": "ready",
  "legacy_migration_issues": [],
  "pricing_template_generation": 3,
  "pricing_reference_generation": 4,
  "updated_at": "2026-08-09T12:00:00Z"
}
```

`/api/settings/auth*` routes are global management endpoints. `/api/settings/costing` (including timezone preference) and `/api/settings/audit` are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored. There is no standalone `/api/settings/timezone` route.

#### Update Costing Settings

```
PUT /api/settings/costing
```

Request:

```json
{
  "report_currency_symbol": "€",
  "timezone_preference": "Europe/Helsinki",
  "expected_updated_at": "2026-08-09T12:00:00Z"
}
```

Response `200`: Updated settings object. A report-currency change with an existing epoch is rejected unless it uses the currency migration preview/commit workflow. `endpoint_fx_mappings` is retired and rejected; historical FX is read-only evidence.

Timezone is not a standalone route. It is saved in the costing CAS and changes only timestamp display and `Custom` input interpretation; stored UTC instants, retention cutoffs, and scheduled UTC-day semantics do not change.

#### Currency Migration Owner Handoff

The Settings currency dialog consumes bounded Pricing template pages or an immutable migration inventory. Drafts are created and filled through `/api/settings/costing/currency-migration-drafts` with signed cursors, sealed before preview, and committed through the atomic `/api/settings/costing/currency-migrations/preview` and `/commit` routes. `archive_unused_fx` uses the same preview/commit routes but only archives proven-unused FX evidence; it does not change the active epoch, template prices, or source FX authoring. All profile-scoped routes are pinned to Default profile id `1`.

There is no standalone `/api/settings/monitoring` route, `/api/monitoring/*` family, or Terminal Target probe route in the current live API contract. Current operator-facing observability and routing-health surfaces are provided through `/api/stats/*`, `/api/audit/*`, and `/api/loadbalance/*`.

---

### User-Agent Client Rules (System Global + User Profile-Scoped)

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

### Header Blocklist Rules (System Global + User Profile-Scoped)

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

### Removed Management Surface

The former CLIProxyAPI management control plane and runtime context-overflow promotion surfaces are not part of the current API.

---

## Loadbalance API

Loadbalance APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

### List Loadbalance Strategies

```
GET /api/loadbalance/strategies
```

Response `200`: Array of strategy objects in the effective profile scope.

### Create Loadbalance Strategy Defaults

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
      "failure_status_codes": [401, 403, 408, 422, 429, 500, 502, 503, 504, 529],
      "reroute_status_codes": [400],
      "ban_mode": "off",
      "retry_base_delay_ms": 5000,
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

Returns `409` when one or more canonical default names are already occupied by non-canonical strategies in Default profile id `1`. In that case, `detail` is an object with `message` and `conflicting_names`.

### Create Loadbalance Strategy

```
POST /api/loadbalance/strategies
```

Request:

```json
{
  "name": "round-robin-primary",
  "legacy_strategy_type": "round-robin",
  "failure_status_codes": [401, 403, 408, 422, 429, 500, 502, 503, 504, 529],
  "reroute_status_codes": [400],
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
- `reroute_status_codes` values must be unique 4xx integers (`400..499`) other than `429`, disjoint from `failure_status_codes`; the backend sorts them. Omitted, the set is `[400]` minus any code already in `failure_status_codes`; an explicit `[]` disables rerouting. A reroute status moves the same request to the next candidate without runtime failure feedback: no retry-window write, no retry or ban counting. When no later candidate produces a response, the client receives the last rejection's original status, headers, and body (kept within the failed-response sampler bounds), and the launch after a rejection carries `attempt_trigger = reroute`.
- Retry-window delay, backoff, jitter, max delay, and cycle retry attempt limit must stay within backend bounds.
- `cycle_retry_attempt_limit` is optional; omitted create/update payloads default it to `3`. When provided, it must be from `1` to `50`.
- `ban_mode` is `off`, `temporary`, or `until_reset`.
- `ban_mode = "off"` requires `ban_cumulative_retry_attempt_threshold = 0` and `ban_duration_seconds = 0`.
- `ban_mode = "temporary"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds` from `1` to `86400`.
- `ban_mode = "until_reset"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds = 0`.
- Runtime retry-cycle exhaustion is inclusive: `cycle_retry_attempts >= cycle_retry_attempt_limit` schedules the retry-window transition.
- Runtime banning is inclusive and explicit: `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`. Prism never derives the ban threshold from `cycle_retry_attempt_limit`.
- Outbound provider requests carry no connection or timeout limits; `runtime.sideEffects.attemptTimeout` applies to scheduler-owned runtime activity handoff work rather than per-strategy fields.

### Get Loadbalance Strategy

```
GET /api/loadbalance/strategies/{strategy_id}
```

Response `200`: Strategy object in the effective profile scope.
Returns `404` when the strategy does not exist in the effective profile.

### Update Loadbalance Strategy

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
  "failure_status_codes": [401, 403, 408, 422, 429, 500, 502, 503, 504, 529],
  "reroute_status_codes": [400],
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

### Delete Loadbalance Strategy

```
DELETE /api/loadbalance/strategies/{strategy_id}
```

Response `200`: `{ "deleted": true }`.
Returns `409` when the strategy is still attached to one or more models; the response detail includes `attached_model_count`.

### List Current Loadbalance State for a Model

```
GET /api/loadbalance/current-state
```

Every query parameter is optional and acts as a filter; there is no required
parameter and no existence check. `model_id` matches the **public model id
string**, not the numeric config id, and selects the connections that model
directly owns — a connection reached through a Model Target belongs to the child
model that owns it. An identifier that matches nothing returns `200` with an
empty cohort and `completeness.state = "no_config"`, never `404`; `404` is
reserved for path-addressed resources.

The response is an envelope carrying `generated_at`, `scope`, `instance_id`,
`configuration_revision`, `completeness`, `items`, `has_more` and `next_cursor`.
Each item nests `model`, `endpoint` and `terminal_target` identities alongside
`observation_state` and the nullable runtime fields; a row the process has not
observed reports `observation_state: "unobserved"` with those fields null rather
than synthesized zeros.

State is derived from the connection-global Ban Policy runtime state: `until_reset`
bans stay `banned` until the reset endpoint clears them; temporary bans stay
`banned` until `banned_until_at`; retry windows stay `retry_wait` until
`next_retry_at`; otherwise the connection is `available`. Items expose QPS and
in-flight admission counters plus live retry-cycle counters, and intentionally
omit `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold`
because current state is connection-global while policy thresholds belong to the
model strategy snapshot recorded on events. Note that the admission counters only
advance while the matching limit is configured and positive.

The exact field set is fixed by the contract tests in
`backend/tests/contract/loadbalance_observe_contract_test.go`, which run under CI;
this section describes intent and does not restate the payload.

### Reset Current Loadbalance State for a Connection

```
POST /api/loadbalance/current-state/{connection_id}/reset
```

Returns `connection_id`, `cleared`, and the post-reset `state` snapshot, so the
caller can calibrate the row it just reset without a second read. `cleared=false`
means no process-local state existed for that connection, which is a success.

Reset clears the process-local retry window, next retry timing and ban state. It
does not clear in-flight or QPS admission counters, and it does not move the
round-robin cursor. This state is ephemeral and is lost on backend restart;
retained SQL runtime-state tables are compatibility schema, not the production
hot path.

### List Loadbalance Events

```
GET /api/loadbalance/events
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
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

### List Loadbalance Incidents

```
GET /api/loadbalance/incidents
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
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

### Get Loadbalance Event Detail

```
GET /api/loadbalance/events/{id}
```

Response `200`: Single event object with the same Ban Policy retry-window metadata, policy snapshot fields, and summary fields as the list item.

### Loadbalance Event Retention

Loadbalance event list and detail APIs remain pinned to Default profile id `1`. Normal cleanup is global: the stored `loadbalance_events_retention_days` value plans automatic v2 jobs; one-time cleanup uses a manual-cleanup preflight plus sealed job under `/api/maintenance/log-retention/*`.

The retired load-balance cleanup endpoint is not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

---

## Auth API

Auth APIs are global and do not require `X-Profile-Id`. Session enforcement is cookie-based: a valid access-token cookie passes the management pre-handler; `POST /api/auth/refresh` rotates the refresh-token family. The runtime proxy-key 401 on `/v1` and `/v1beta` is a separate credential surface and never triggers management session expiry.

### Auth Status

```
GET /api/auth/status
```

Returns the tagged `PublicAuthStatus` union. The only legal shapes are `enabled` (with optional `disabling_enforced`), `disabled`, and `transition_fail_closed` (with `enabling_fail_closed` or `rollback_required`). Every branch carries the canonical positive decimal `effective_generation`; `transition_fail_closed` adds `retry_after_seconds`.

```json
{
  "state": "enabled",
  "transition_state": null,
  "login_available": true,
  "effective_generation": "7",
  "retry_after_seconds": null
}
```

### Public Bootstrap

```
GET /api/auth/public-bootstrap
```

Initializes session and returns basic auth state for the login page. A persisted fail-closed/rollback transition is a typed `503` (see [Error Responses](api-reference.md#error-responses)), never reported as disabled or unauthenticated. Response `200` uses the strict session shape:

```json
{
  "authenticated": false,
  "auth_enabled": true,
  "username": null
}
```

Authenticated responses add the server-authored `subject_key` (e.g. `auth:subject:1`); it never appears in public status or anonymous/disabled payloads.

### Login

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

Response `200` returns the authenticated session shape and sets the configured access-token and refresh-token cookies. Errors use the registered flat envelope: `401 auth_invalid_credentials` (generalized, never distinguishes unknown user from wrong password) and `429 auth_login_locked` with `details.retry_at` / `details.retry_after_seconds` plus a same-source `Retry-After` header; no failure counts or subject existence are exposed. A persisted fail-closed transition is the registered typed `503`.

### Logout

```
POST /api/auth/logout
```

Idempotent strict `204`: valid, missing, expired and revoked cookies all revoke nothing new, always send the canonical clear-cookie headers, and never go through the management pre-handler. Auth-disabled race is a registered typed `400 auth_not_enabled` so the client re-bootstraps to the open-access explainer. A persisted fail-closed transition blocks logout with the typed `503`.

### Refresh Session

```
POST /api/auth/refresh
```

Uses the `refresh_token` cookie to issue a new session. Implements token family rotation. Response `200` uses the same authenticated session shape as login; auth-disabled is a live `200 {auth_enabled:false, authenticated:false}` (never a stale `true,false`); a persisted fail-closed transition is the typed `503`; an invalid/revoked refresh token is a `200 {authenticated:false, auth_enabled:true}` with cleared cookies.

### Get Session

```
GET /api/auth/session
```

Protected route. Returns the current authenticated session state with the strict authenticated shape (including `subject_key`) or `401 auth_not_authenticated`.

### Transition Operation Status

```
GET /api/auth/operations/{operation_id}/status
```

Public auth-operation status used by the coordinator to settle transition waits. The operation id is a lookup selector, not authorization; the fixed bounded projection carries no username, mode, operation id, secret or settings.

### Auth Settings

```
GET /api/settings/auth
PUT /api/settings/auth
```

Read/write the operator authentication settings through the v2 desired/effective pointer contract. The auth-control settings surface stays reachable during fail-closed transitions as the repair path. PUT stages an immutable config version, binds the Proxy-owner readiness generation/count at one server-clock instant, and flips the effective pointer only after a fresh conditional publish proof; exact operation replay returns the durable result. Passwords are only slow-hashed at rest and are never included in a response or operation projection. A failed publish proof persists a durable `rollback_required` transition with the initiating operation id.

### Proxy API Keys

```
GET    /api/settings/auth/proxy-keys
POST   /api/settings/auth/proxy-keys
PATCH  /api/settings/auth/proxy-keys/{key_id}
POST   /api/settings/auth/proxy-keys/{key_id}/rotate
DELETE /api/settings/auth/proxy-keys/{key_id}
```

Proxy keys are global rather than profile-scoped. `GET ...?include=setup_readiness&expected_route_witness_generation=<generation>` returns the setup-readiness projection for the sixth setup fact (key-policy configuration and scope application on a specified witness generation). Create/rotate responses carry the one-time raw key with `private, no-store` headers.

---
