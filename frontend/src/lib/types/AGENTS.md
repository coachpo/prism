# Backend-aligned types

- Preserve server field names and nullable/optional distinctions. This directory mirrors wire contracts; frontend form schemas, view models, and derived labels belong with their consumers.
- Add contracts to the owning leaf and re-export through `../types.ts`. Models.dev metadata types stay in `model-catalog.ts`; Pi export/binding/search types stay in `model-export.ts`. Separate catalog revisions, identities, and CAS tokens must not collapse into a weak shared union.
- Model contracts require `direct_request_enabled`, `incoming_model_target_count`, and `configuration_warnings`. Routing summaries remain authoritative full-graph projections; family-discriminated create shapes omit OpenAI-only fields for other families.
- `routing.ts` carries connection `upstream_model_id` and JSON custom parameters. `request-logs.ts` preserves retained attempt `upstream_model_id` separately from winner `final_upstream_model_id`; historical NULL remains unknown. Request-log IDs are decimal strings even where counts/micros are JSON numbers.
- Request-log entry, attempt, and final model fields remain distinct. Pricing selection state and card role are independent nullable evidence; peak schedule evidence is independently nullable.
- `model-stats.ts` retains all three scoped blocks and nullable trusted cost/coverage. `currency-migration.ts` retains complete role-keyed cards and explicit template kinds, rather than a projected base/offpeak scalar.

Cross-check affected backend structs and contract tests when changing these types; do not repair a mismatch by inventing frontend aliases.
