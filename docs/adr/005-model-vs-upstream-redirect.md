# ADR 005: Model Redirect vs Upstream Redirect Semantics

Status: proposed

## Context

Required OpenAI-family redirect behavior has two meanings: model redirect and upstream redirect. The target architecture must separate them so logs and route plans remain explainable.

## Decision

A model redirect changes the effective model and re-enters normal load balancing. An upstream redirect does not change the effective model; it pins or narrows candidate upstreams for the current RoutePlan.

## Consequences

Requested model and effective model remain distinct. Accounting can record route reason, selected upstream, and redirect type without rewriting user intent.

## Rejected alternatives

- Treat all redirects as model rewrites.
- Treat upstream redirect as provider fallback.
- Retry sibling models after a redirect without re-planning.

## Implementation notes

Model redirect runs before upstream candidate ordering. Upstream redirect runs during candidate selection and must be visible in RoutePlan metadata.

## Tests required

Model redirect re-entry tests, upstream pin/narrow tests, requested/effective model assertions, selected upstream assertions, and route reason assertions.
