# Operator design system

[DESIGN.md](../../../DESIGN.md) is authoritative. `index.ts` is this layer's public import surface.

- Keep components route-agnostic: callers supply localized copy and state. API calls, routing, profile state, polling, and feature-specific columns/forms stay outside this directory.
- `foundation.ts` exports the token/density contract checked by `../../../tests/lib/design_token_contract.test.mjs`. Token changes require the corresponding `pnpm run test:lib` check from `frontend/`.
- Reuse `OperatorMissingValue`/`OperatorValue` for absent evidence, `OperatorStalenessBadge` for stale evidence, and `OperatorErrorState` for failed reads.
- Keep required primitive labels, focus behavior, and keyboard composition intact.
- Add only components shared by actual operator surfaces; do not wrap every primitive or move feature-specific copy into this layer.
