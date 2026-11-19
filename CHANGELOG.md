# Prism Changelog

This history covers the surviving repository tags from `v1.0.9` through `v1.1.21`. See [STATUS.md](STATUS.md#deployment) for deployment observations and image availability where recorded.

## v1.1.21

- Reroute a request after a provider error in the first SSE event without cooling down the rejected Terminal Target or incrementing its shared Ban Policy failure state. The next launch is recorded as `reroute`. See [ADR 0001](docs/adr/0001-keep-stream-start-rejections-request-scoped.md).

## v1.1.20

- Simplified the management interface by removing redundant annotations, repeated counts, and normal-state messages.

## v1.1.19

- Added failover when a provider sends an error in the first event of a successful HTTP SSE response. This tag did not produce a published app image.

## v1.1.18

- Added configurable request-scoped upstream rejection codes to routing strategies. Migration `000035_request_scoped_reroute` adds the strategy field and persisted reroute attribution.

## v1.1.17

- Carries forward the catalog reasoning-price fix from `v1.1.16`.

## v1.1.16

- Use the output price for reasoning tokens when a models.dev pricing source omits a separate reasoning price. This tag did not produce a published app image.

## v1.1.15

- Refined token contracts and dashboard state handling.

## v1.1.14

- Added personal investigation and maintenance workflows, including sampled route explanation, bounded batch model maintenance, request comparison, and portable saved views and export presets.

## v1.1.13

- Restored visible loading feedback during table pagination.

## v1.1.12

- Added OpenCode provider and model configuration export with client-specific transport, metadata, and credential handling.

## v1.1.11

- Improved ingress-chain read performance and preserved filtered chains and timestamp ties during pagination.

## v1.1.10

- Changed native runtime client header forwarding to use the effective blocklist rather than an allowlist.

## v1.1.9

- Corrected scroll clipping and seams between pinned table columns.

## v1.1.8

- Improved model-page workflows and default strategy selection. Corrected image-only OpenAI capability authoring.

## v1.1.7

- Restored the tracked operator SQL and runbook for direct-entry reclassification.

## v1.1.6

- Fixed export copy on insecure origins and removed redundant per-model base URLs from the Pi output.

## v1.1.5

- Preserved model configuration warnings as a JSON array.

## v1.1.4

- Separated client-addressable model entries from recursive Model Targets. The additive migration preserves existing entries until the operator reclassifies them explicitly.

## v1.1.3

- Added metric scopes and identity filters to the model list.

## v1.1.2

- Unified external catalog management and added evidence-based output-rate metrics.

## v1.1.1

- Introduced Pi-only configuration export with independent pi.dev catalog bindings.

## v1.1.0

- Added multi-level routing attribution, client model configuration export, models.dev metadata and pricing integration, and expanded Observe usage metrics.

## v1.0.24

- Reworked pricing templates into typed cards and added ingress-chain pagination.

## v1.0.23

- Applied the request-time header blocklist when scrubbing audit data and aligned the UI with the audit header array contract.

## v1.0.22

- Added input-threshold tier pricing to templates and corrected Observe top selection for unattributed entities.

## v1.0.21

- Removed the hot configuration snapshot pipeline and reorganized durable management-job ownership.

## v1.0.20

- Removed the bootstrap `runtime.transport` connection and timeout limits. Added metric-window totals to the Observe trend table.

## v1.0.19

- Corrected audit body persistence to bind raw bytes to PostgreSQL `BYTEA` columns.

## v1.0.18

- Added prompt cache-read share to Observe and request detail. Quarantined invalid runtime telemetry rows.

## v1.0.17

- Added per-Terminal Target routing schedules. Changed runtime failure reporting and outbound header handling.

## v1.0.16

- Added static routing diagnostics and corrected mixed target ordering, capability presentation, and Observe chart binding.

## v1.0.15

- Updated the Go toolchain and CI actions.

## v1.0.14

- Completed ingress-chain and cost-segment views. Corrected pricing and retention boundary identity and protected request and audit responses from caching.

## v1.0.13

- Added compare-and-set guards for Terminal Target pricing assignments and a preflight confirmation keyword for destructive retention. Fixed the Observe chart caption clipping.

## v1.0.12

- Fixed attempt-view sorting, distinguished request-log read errors from empty results, and removed duplicate card borders.

## v1.0.11

- Updated the frontend Nano ID override to 3.3.18.

## v1.0.10

- Corrected routing policy table overflow, pricing reference counts, launcher port ownership, and in-page scroll preservation.

## v1.0.9

- Earliest surviving tag. This commit squashes earlier history, leaving no per-version changes before this baseline in the repository.
