# ADR 003: ModelProfile / UpstreamEndpoint / RouteRule / RoutePlan Configuration Model

Status: proposed

## Context

The target gateway needs explicit configuration records for model, upstream, route, pricing, and capability decisions. The audit found route-planning data spread through runtime snapshots and request plans.

## Decision

Use explicit `ModelProfile`, `UpstreamEndpoint`, `RouteRule`, `RoutePolicy`, `RoutePlan`, `PriceCatalog`, and `ProviderCapability` records.

## Consequences

Routing decisions become inspectable and testable. Accounting can preserve requested model, effective model, selected upstream, and route reason from the phase that knows them.

## Rejected alternatives

- Keep a broad mutable `requestPlan` as the cross-phase contract.
- Infer route facts after execution.
- Encode route behavior only in provider or endpoint metadata.

## Implementation notes

RoutePlan should be immutable once execution starts, except for typed attempt outcomes.

## Tests required

Registry validation tests, route-plan golden tests, model redirect tests, upstream redirect tests, and accounting field assertions.
