# Statistics Handler Guidance

Keep HTTP/query shaping here and read-model semantics in `../../../domain/stats/`. `request_log_handlers.go`/`request_log_export.go` own Requests list/detail/CSV; `observe_handlers.go` owns signed Observe contexts and fragments.

- Resolve Requests attempt, ingress-chain, and CSV `time_range` bounds through the same actual-coverage owner used by SQL. `all` uses the owner's earliest bound; a complete empty domain stays explicitly empty. Do not infer completeness from page size, policy days, row minima, or browser time.
- Preserve `known|legacy_unknown` coverage and explicit gaps for dirty/stale owner materialization. An append-only coverage change does not revoke an already sealed Observe predicate.
- Scope is `ingress|final_execution|route_attempt`, with the domain's scope-specific group/filter grammar. Query contexts bind scope and frozen usage/request/event owners; fragment parameters cannot override token scope.
- Reject present malformed timestamps, ids, limits, cursors, metrics, groups, or filters with typed errors. Defaults apply only to absent fields. Ordinary Requests triage is unsigned; true Observe final/attempt selectors consume `query_context` on list and export.
- Preserve caliber, dataset coverage, and sample/missing counts in metric responses. Route-attempt aggregates make no served-final cost claim; output-rate reads use persisted delivery evidence rather than deriving a rate from TTFT.
- Dashboard aggregate snapshots apply only to matching default windows. Recent activity stays a separate bounded read; invalidation comes from the management side-effect seam, not a public stats mutation or inline snapshot rebuild.

`request_log_query_test.go` and `observe_query_contract_test.go` cover local parsing; domain tests cover classifiers and predicates. Database-backed scope, query-context, and CSV behavior belong in [three-scope](../../../../tests/contract/three_scope_stats_contract_test.go), [Observe context](../../../../tests/contract/observe_query_context_test.go), and [export](../../../../tests/contract/request_logs_export_contract_test.go) contract tests. Coverage append handoff and retention fencing belong in [management integration tests](../../../../tests/integration/management_audit_stats_phase7_test.go).
