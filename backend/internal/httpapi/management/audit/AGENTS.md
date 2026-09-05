# Audit Handler Guidance

`service.go` owns audit list/detail/raw-body reads; `job_routes.go` owns management-job status, evidence, and cancellation. Audit query semantics belong to `../../../domain/audit/`, and job execution belongs to `../../../platform/managementjobs/`.

- Keep list query parsing bounded and descending-only, rejecting unsupported filters. Resolve the SQL interval and coverage from the shared actual-coverage owner in the same repeatable-read transaction; a retention floor alone does not prove actual history coverage.
- Preserve the request-time audit-capture check when a lookup is tied to a request log. Missing capture is distinct from an empty audit result.
- Raw-body downloads serve the exact stored byte prefix with safe attachment and private/no-store headers; do not reinterpret or reconstruct the payload.
- Global retention-job queries require `scope=global` and `type=log_retention` together. Keep their signed evidence paging distinct from ordinary profile job reads.
- Job creation and retention policy changes stay in `../settings/`; cancellation here delegates to the platform job state machine.
