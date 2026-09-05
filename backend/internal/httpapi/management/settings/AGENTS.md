# Settings Guidance

Keep global retention policy/preflight/job acceptance here; execution belongs to `../../../platform/managementjobs/`. Costing/timezone and API-family audit settings remain profile-scoped. Auth transitions belong to [auth](../auth/AGENTS.md), and bootstrap files remain platform-owned.

- Timezone shares the costing CAS; there is no standalone timezone route. An established reporting-currency epoch changes only through the migration preview/commit flow.
- Currency migration uses bounded owner snapshots, chunked drafts, signed cursors, complete role-keyed cards, immutable evidence, and one atomic active-epoch cutover (`currency_migration_*.go`). Current cards never come from legacy template evidence. `archive_unused_fx` writes its archive ledger without changing the active epoch, templates, or source FX authoring.
- Audit PUT replaces exactly the `openai`, `anthropic`, and `gemini` policies with `disabled|metadata_only|body_capture`. Preserve group revision/CAS, replay identity, and affected-writer admission in `audit_policy_transaction.go`.
- Preserve NULL retention policies on fresh installs and classify retained values without silent cleanup or clamping. Consume actual coverage through the stats owner projection, including its revisions, fence, freshness, and gaps; policy days and row minima do not prove coverage.
- Retention enablement (`null -> N`), shortening, and manual cleanup require a fresh preflight and keyword confirmation. Keep token scope and owner/principal/revision checks sealed; durable replay is checked before rejecting an already consumed token.
- Manual acceptance queues a durable job and does not revoke coverage. Only the platform executor's final publish advances the deletion floor/epoch; HTTP handlers never delete rows or partitions inline.
- Keep shared operation identity and flat typed settings errors in `settings_operations.go`, `settings_request_identity.go`, and `problems.go` rather than adding branch-specific replay or error conventions.

`routes_test.go` covers local HTTP contracts. Currency cutover belongs in [currency migration contract tests](../../../../tests/contract/s11_currency_migration_contract_test.go); coverage handoff and retention fencing belong in [management integration tests](../../../../tests/integration/management_audit_stats_phase7_test.go).
