# Page domains

`../features/` mounts protected routes; this directory contains active page components and reusable domain helpers. Preserve that import boundary when changing an existing surface rather than assuming these files are obsolete.

- `LoginPage.tsx` is the public auth entry. It must not acquire protected-shell provider or data dependencies.
- Model CRUD/mixed target forms belong to [models](models/AGENTS.md); Terminal Target and models.dev helpers belong to [model-detail](model-detail/AGENTS.md). Requests investigation is owned by [request-logs](request-logs/AGENTS.md); Settings shell/resource orchestration is owned by [settings](settings/AGENTS.md).
- Endpoint, Ban Policy, and proxy-key presentation has local deltas in [endpoints](endpoints/AGENTS.md), [loadbalance-strategies](loadbalance-strategies/AGENTS.md), and [proxy-api-keys](proxy-api-keys/AGENTS.md).
- `pricing-templates/DeletePricingTemplateDialog.tsx` only renders deletion/conflict state from the pricing feature's usage and deletion owners. Preserve concrete reference evidence and refetch on stale CAS; do not put another collection/mutation owner in that dialog directory.
