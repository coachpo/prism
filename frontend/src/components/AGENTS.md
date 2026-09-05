# Shared components

- Keep reusable widgets presentation-focused. Protected shell state belongs to [layout/app-layout](layout/app-layout/AGENTS.md); primitive composition belongs to [ui](ui/AGENTS.md).
- `ApiFamilyIcon.tsx`, `apiFamilyPresentation.ts`, and `ApiFamilySelect.tsx` share the API-family icon/label mapping. Extend those owners rather than copying provider labels into pages.
- Shared preference controls belong here; route-specific data, filters, and mutation state remain with feature/page owners.
- Ban Policy configuration renders under `../features/loadbalance/`; routing-health presentation renders under `../features/routing-health/` and its retained helpers in `../features/observe/`.
