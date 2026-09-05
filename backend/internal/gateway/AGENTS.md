# Gateway Ownership

Keep gateway abstractions independent of Prism's concrete route registry in `../httpapi/runtime/operations.go`.

- [Core](core/AGENTS.md) owns envelopes, hook execution, route metadata, and shared errors.
- [Provider](provider/AGENTS.md) owns native upstream behavior. Do not add provider branches to `core/`, `routing/`, or accounting.
- `routing/planner.go`, `reservation_manager.go`, `retry_policy.go`, and `redirects.go` own ordering, reservation admission, retry windows, and redirect narrowing. Runtime must release owned reservations when attempts finish.
- `accounting/event.go` owns normalized accounting events. `Event.Normalize` and `SetPricingEvidence` must not mutate or retain caller-owned pointer storage; keep pricing kind, selection state, and card role independent.
- Keep route reasons canonical across core, routing, runtime observability, and accounting. New reasons or usage sources must pass accounting normalization.
