# ADR 004: QPS/RPM/TPM/IPM/Concurrency Reservation and Overflow

Status: proposed

## Context

Current runtime admission supports QPS and stream/non-stream in-flight limits. The audit found RPM, TPM, and IPM as stats concepts, not runtime reservation controls.

## Decision

Implement admission as typed reservations for QPS, RPM, TPM, IPM, and concurrency. When a candidate upstream is exhausted, routing tries the next eligible upstream according to the RoutePlan.

## Consequences

Quota behavior is enforceable, not merely observable. Rejections and overflows produce explicit route reasons and load-balance events.

## Rejected alternatives

- Dashboard-only RPM/TPM/IPM.
- Global process-wide limits without route context.
- Best-effort overflow without reservation leases.

## Implementation notes

Reservations must release on completion, cancellation, or failed pre-commit attempts.

## Tests required

Concurrent reservation tests, QPS overflow tests, RPM/TPM/IPM rejection tests, lease-release tests, and accounting assertions for selected upstream and route reason.
