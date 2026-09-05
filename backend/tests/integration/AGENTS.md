# Integration Tests

Use this suite for process and persistence boundaries broader than one handler/package; share `harness.go` instead of adding process/container setup inside tests.

- `startup_test.go` and `launcher_startup_contract_test.go` own bootstrap/default seeding, existing-file preservation, and the root launcher's local wiring. Keep restart-only configuration and parse-only retired fields covered.
- `migrations_test.go` and `testdata/migrations/schema.sql` own migration history and normalized schema evidence. Use the checked-in migration runner, never test-local schema rewrites.
- `direct_request_enabled_migration_test.go` preserves retained rows/defaults. `direct_request_entry_reclassification_plan_test.go` verifies the operator SQL at `../../../scripts/operations/direct-request-entry-reclassification.sql` on disposable fixtures; it does not authorize applying that SQL to an instance.
- `dockerfile_contract_test.go` guards the single app image's non-root UID/GID, writable config ownership, and bootstrap path. `process_control_test.go` owns verification/child-process lifecycle.
- Partitioned retention tests cover the four managed log datasets and their owning store/jobs; alerting outbox tests cover webhook durability. Do not move narrow management response tests here from the contract suite.
