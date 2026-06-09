# ADR 012: Testing Strategy Using Mock Upstreams and Golden Fixtures

Status: proposed

## Context

The existing route matrix, rejected-route isolation, hook residency, conversion, overflow, pricing, and observability tests are key preservation assets. The new gateway needs stronger adapter and phase tests.

## Decision

Use mock upstreams and golden fixtures as the migration contract. Every operation family gets route-matrix tests, adapter contract tests, streaming tests, accounting tests, and golden request/response fixtures.

## Consequences

Migration can proceed in small PRs without trusting provider network behavior. Golden fixtures make conversion and provider normalization changes reviewable.

## Rejected alternatives

- Manual provider testing as primary proof.
- Snapshot tests without semantic assertions.
- Removing old tests before new phase coverage exists.

## Implementation notes

Keep old tests green until an operation fully migrates, then delete obsolete tests only after equivalent new coverage passes.

## Tests required

Mock upstream route matrices, golden conversion fixtures, stream terminal fixtures, admission concurrency tests, context overflow fixtures, and accounting assertions.
