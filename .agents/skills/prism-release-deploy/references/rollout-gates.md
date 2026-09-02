# Rollout gates

## Per-service sequence

1. Re-discover the project and assert the currently running image, health, database schema, config hash, entity counts, backup capacity, and absence of owner/orphan anomalies.
2. Create a verified quiesced backup. Keep PostgreSQL running while only the app is stopped.
3. Deploy the exact image manifest reference and wait up to the configured health deadline.
4. Require migration history advancement without loss of preflight entity counts, unchanged config hash, no owner-backed missing upstream ID, and no false historical backfill.
5. If provider smoke is authorized, require Chat and Responses stream/non-stream visible output plus request/usage attribution.
6. Observe health, immutable image reference, and restart count for the configured interval.
7. Only after success, apply the separately confirmed keep-three retention policy.

## Failure behavior

- Release/CI/image failure: no deployment-host writes.
- Backup failure: restart and verify the original app, then stop the rollout.
- A failure: do not touch B.
- Migration failure with unchanged schema: the original image may be restarted only after the schema check proves it remains compatible.
- Health/smoke/observation failure after schema advancement: stop the app, preserve PostgreSQL and backup, and require an explicit restore decision.
- Retry provider calls once only for 429/5xx. Transport errors, 2xx empty visible output, invalid SSE completion, and attribution mismatch stop the rollout.
