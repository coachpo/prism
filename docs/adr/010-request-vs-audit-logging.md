# ADR 010: Request Logging vs Immutable Audit Logging

Status: proposed

## Context

Prism needs operational request logs and audit logs. The target architecture must prevent mutable operational views from replacing immutable audit evidence.

## Decision

Separate request logging from immutable audit logging. Request logs support operational querying, dashboard aggregation, and mutable retention. Audit logs record immutable request/response metadata and optional redacted bodies according to the audit policy captured at request time.

## Consequences

Operators can inspect runtime behavior without weakening audit guarantees. Audit capture boundaries become testable by operation and stream mode.

## Rejected alternatives

- Single table for operational and audit purposes.
- Capture bodies by default.
- Reconstruct audit state from request logs after the fact.

## Implementation notes

The accounting sink emits separate request-log and audit-log records from the same typed event, with explicit capture policy.

## Tests required

Audit enabled/disabled tests, body capture tests, redaction tests, request-log retention tests, and audit immutability tests.
