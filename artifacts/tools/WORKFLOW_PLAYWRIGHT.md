# Local workflow browser harness

`workflow_playwright.py` drives the real local Prism UI with the bundled
Playwright CLI. It never imports `@playwright/test` and never uses the existing
`prism-matrix` session. Every case accepts only its dedicated literal-IPv4
loopback origin bound by the owner receipt and private fixture manifest.

## Read-only cases

Read-only WFL-001, WFL-002, and WFL-010 use the same two-stage owner as the
mutation cases. Prepare the exact clone before allocation, pass the returned
clone and four runner fingerprints to `matrix_runner.py run-one`, then run the
one allocated attempt:

```bash
python3 artifacts/tools/run_workflow_cases.py prepare-case --case-id WFL-001
# matrix_runner.py run-one --case-id WFL-001 ...returned fingerprints/clone...
python3 artifacts/tools/run_workflow_cases.py run-case --case-id WFL-001 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/WFL-001/primary-attempt-1
```

`workflow_playwright.py` intentionally has no direct `readonly` command. The
owner starts and journals the case-private mock, backend, Vite proxy, clone,
named browser, trace, strict close, and scratch purge. Before the browser opens
it validates the exact attempt and formal controls under the runner lock, then
rechecks them after bounded cleanup.

The three read-only handlers provide:

- `WFL-001`: the canonical route walk, legacy route redirects, redacted browser
  console, indexed accessibility snapshots, and a viewer-compatible trace.
- `WFL-002`: all 12 imported model IDs, chat-only/dual-native/image-only detail
  snapshots, bounded routing-diagnostics evidence, and UI/API assertions for
  the expected `FULL`/`PARTIAL`/`NONE` states.
- `WFL-010`: desktop/narrow snapshots and screenshots, browser-local
  loading/empty/error fixtures, a real last-good refresh followed by a
  browser-local 503 stale transition, imported operation-coverage states,
  keyboard focus, accessible names, viewport reachability, and a WCAG contrast
  baseline. The fixture routes affect only this isolated Chromium context and
  are removed before the case finishes.

The owner returns `1` only after a test assertion failure has been sealed into
every frozen evidence file, `2` for harness/setup or evidence-finalization
failure, and `0` only when the case passes.

Each read-only session primitive owns the trace lifecycle outside the case handler, so a
handler exception still stops and packages the trace before its isolated
session closes. Textual trace events, network records, and textual resources
are redacted during packaging. Binary resources, including screenshots that
cannot be proven secret-free, are omitted; `trace-redaction.json` records that
policy inside the structural trace archive. Member names are scanned before
publication, and the raw `.trace`, `.network`, resource tree, and temporary ZIP
are removed after packaging (including rejected packaging attempts). All raw
Playwright output lives below the mode-0700 run-private lane; strict close then
deletes that complete case/attempt scratch tree and verifies its absence before
the public attempt can pass or report a product failure.

## Frozen WFL-001..WFL-010 contracts

The helper is bound to matrix revision 2 and SHA-256
`c023da4d4d980094bd01957ec347421fb85622dfa1c95b354d482b0cbf7a95ff`.
It fails closed if any WFL `required_evidence` list drifts. Inspect exactly one
case without opening a browser or touching a service:

```bash
python3 artifacts/tools/workflow_playwright.py case-contract --case-id WFL-003
```

Every mutation case has one exact disposable database name:
`prism_matrix_20260813t204518z_case_<case_slug>`. Gold, template, and
`prism_matrix_runtime` are rejected.

The formal single-case owner is `run_workflow_cases.py`. Its `self-test` and
`contract` commands are mutation-free. Every WFL case uses a two-stage handoff:

```bash
python3 artifacts/tools/run_workflow_cases.py prepare-case --case-id WFL-003

python3 artifacts/tools/matrix_runner.py run-one \
  --run-dir artifacts/evidence/20260813T204518Z \
  --case-id WFL-003 \
  --fingerprint 'branch_head=<runner_fingerprints.branch_head>' \
  --fingerprint 'config=<runner_fingerprints.config>' \
  --fingerprint 'database_template=<runner_fingerprints.database_template>' \
  --fingerprint 'source_dump=<runner_fingerprints.source_dump>' \
  --database-clone '<database_clone from prepare-case>'

python3 artifacts/tools/run_workflow_cases.py run-case \
  --case-id WFL-003 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/WFL-003/primary-attempt-1
```

`prepare-case` must run while the matrix checkpoint names that case and before
`run-one`. It writes an atomic mode-0600 `creating` receipt before the backend
build, reconciles only receipt-owned orphan preparation artifacts after a
crash, then returns the physical clone fingerprint as `database_clone` in a
`prepared` handoff. Pass that value and all four values in
`runner_fingerprints` unchanged to the external matrix runner; supplying only
`database_clone` would inherit an older cycle HEAD and is rejected.
The owner never allocates or records an attempt. `run-case` accepts only the
latest unique formal running attempt, atomically binds it to the prepared clone,
and holds the runner lock through service startup, browser execution, frozen
evidence sealing, cleanup, and final control revalidation.
At the handoff consumption edge, before fixture setup or any service/browser
start, it revalidates the mode-0600 config, manifest, and sealed private-value
receipts and their exact semantic bindings, the physical clone fingerprint and
full DB-lane content identity (all public rows and schema objects), and every
pinned runtime/source input.

WFL-009 is part of that same owner and never calls the direct retention CLI.
The WFL-009 clone alone receives a narrow temporary
insert trigger that defers only the first manual `request_logs` job, keeping it
queued for 720 seconds, beyond the frozen 600-second whole-case deadline, for
the real UI cancellation. The owner enforces each matrix timeout with one
absolute process-wide wall-clock deadline. It reserves 90 seconds for cleanup,
re-arms only to that same final deadline when cleanup begins, and keeps formal
post-validation inside the bound. Its clone-local one-row guard
is independent of retained terminal history, and setup rejects any inherited
nonterminal retention job before the backend starts. The second UI-created job
is not deferred and must reach a refreshed succeeded state before retained
rows, partition ownership, and the two exact job identities are accepted. A
queued manual cancellation must report `queued_no_data_changed`; other scope
labels fail the case.

WFL-009 also requires UTC `previewed_at`/`expires_at` values with the exact
five-minute capability TTL, checks that the first capability still has at
least 30 seconds before confirmation, and rechecks the server UTC day before
both destructive lanes and the final projection. Queued cancellation compares
all four datasets' marker/total/harness counts, every managed partition
projection, purge state, policy/epoch state, coverage read models, settings,
and the audit retention fence before it may record
`retention_storage_unchanged`. The cancellation step also holds one real
queued-state list response across the cancel response and releases it
afterward; the row must remain terminal, so an older poll cannot overwrite the
durable cancellation. The completion step likewise holds the cancelled-job
detail response while opening the completed job and then releases the older
response; the completed identity and rendered terminal evidence must remain
stable, covering both stale-detail flashes and out-of-order responses.

The owner requires a clean tracked worktree, builds a case-private backend
from that exact HEAD with network-disabled Go module resolution during
`prepare-case`, and records
the HEAD/source/toolchain/binary provenance. It fingerprints the backend
source tree, frontend runtime source/config tree, the complete installed pnpm
package store, case backend binary, DB lane, exact offline
Playwright CLI 0.1.18 tree, browser wrapper, command runtimes, selected
Chromium executable, and its complete app bundle. It rechecks the execution
handoff before consumption and the immutable runtime/source set again during
and after each scenario. Child services and Playwright
CLI subprocesses receive a minimal allowlisted environment; ambient
credentials, `DATABASE_URL`, PostgreSQL, proxy, Prism, and Vite overrides
cannot enter browser code or redirect a fixture away from its private config
and disposable clone. Ordinary execution rejects drift.
`cleanup-case` tolerates product/owner drift after a failed attempt but still
requires the recorded DB/support/browser cleanup fingerprints, strictly closes
the named session even if the pinned browser bundle was subsequently removed,
removes its exact private scratch tree, and verifies all owned ports are free
before dropping the exact clone. A stale process journal whose original leader
cannot still be identity-verified is retained for manual inspection; the owner
never signals a numeric process group on PGID alone.
Process journals are written before spawn (`launching`), immediately after a
returned PID/PGID (`spawned`), and after full command/start-time verification
(`started`). Reconciliation of an interrupted launch scans only the receipt's
exact command markers and bounded launch-time window, and stops a child only
when exactly one self-led process group matches; ambiguity remains journaled.

A passed attempt retains its private owner journal for audit. After the matrix
runner has durably recorded that result, invoke `cleanup-case` for that exact
attempt before opening the same case in the regression cycle; cleanup archives
the old journal, so the next cycle receives a freshly built backend, clone, and
browser session. Failed attempts use the same command only after diagnosis and
before their replacement attempt.

## Internal fixture-bound WFL-003..WFL-009 helpers

These commands document the lower-level protocol used by the formal owner.
They are not an alternative formal execution path; `prepare-case` creates the
private manifest and `run-case` drives these helper operations.

Before opening a case, create a mode-0600 JSON manifest below
`artifacts/evidence/20260813T204518Z/private/`:

```json
{
  "schema_version": 1,
  "run_id": "20260813T204518Z",
  "case_id": "WFL-003",
  "fixture_scope": "case",
  "disposable": true,
  "database_clone": "prism_matrix_20260813t204518z_case_wfl_003",
  "database_clone_identity": "<64 lowercase hex characters>",
  "frontend_origin": "http://127.0.0.1:15203",
  "backend_origin": "http://127.0.0.1:18203",
  "mock_origins": ["http://127.0.0.1:18303"]
}
```

WFL-003, WFL-004, WFL-006, and WFL-008 also require a mode-0600 private JSON
array containing every synthetic credential or leak-detection marker. Values
are sealed by the preparation receipt before allocation and cannot be appended
or replaced during the flow; they never enter helper state, CLI arguments, or
ordinary evidence. The browser owner installs them through a private file
bridge only for the action that needs them and fail-closed clears every live
page afterward. The redactor covers literal, JSON-escaped, URL-encoded, and
base64 forms.

Initialize a dedicated session without starting a trace:

```bash
python3 artifacts/tools/workflow_playwright.py case-init \
  --case-id WFL-003 \
  --case-dir "$ATTEMPT_DIR" \
  --fixture-manifest "$PRIVATE_FIXTURE_MANIFEST" \
  --private-values-file "$PRIVATE_VALUE_FILE"
python3 artifacts/tools/workflow_playwright.py trace-start --case-dir "$ATTEMPT_DIR"
python3 artifacts/tools/workflow_playwright.py goto \
  --case-dir "$ATTEMPT_DIR" --path /route/endpoints
python3 artifacts/tools/workflow_playwright.py snapshot \
  --case-dir "$ATTEMPT_DIR" --label endpoint-form
```

Build one of the required grouped snapshot files from named captures:

```bash
python3 artifacts/tools/workflow_playwright.py snapshot-index \
  --case-dir "$ATTEMPT_DIR" \
  --evidence-name form-snapshots.json \
  --label endpoint-form \
  --label pricing-form
```

For required plain-text snapshots, write the exact evidence name at capture
time:

```bash
python3 artifacts/tools/workflow_playwright.py snapshot \
  --case-dir "$ATTEMPT_DIR" \
  --label routing-health \
  --evidence-name routing-health.snapshot.txt
```

All raw Playwright files default below the run's private directory. A
nonblocking case lock prevents two commands from changing one case state at the
same time. Formal readonly and mutation owners additionally hold the runner
lock for the complete execution and final control recheck.

Record each case-specific checkpoint in the exact order printed by
`case-contract`:

```bash
python3 artifacts/tools/workflow_playwright.py case-checkpoint \
  --case-dir "$ATTEMPT_DIR" --name endpoint_created
python3 artifacts/tools/workflow_playwright.py case-status --case-dir "$ATTEMPT_DIR"
```

After an interruption, reconcile an orphan snapshot or an already-packaged
trace without overwriting either one:

```bash
python3 artifacts/tools/workflow_playwright.py case-resume --case-dir "$ATTEMPT_DIR"
```

Finish the trace and close only this named session:

```bash
python3 artifacts/tools/workflow_playwright.py trace-stop --case-dir "$ATTEMPT_DIR"
python3 artifacts/tools/workflow_playwright.py case-close --case-dir "$ATTEMPT_DIR"
```

`case-check` verifies every exact filename required by `matrix.json`, validates
nonempty JSON evidence and its exact `case_id`, scans text evidence for private
values and credential patterns, validates the sanitized trace archive, requires
all checkpoints, and requires the browser lifecycle to be closed.

## Secret cleanup and trace boundary

WFL-004 and WFL-008 may start tracing only after the one-time/password UI is
cleared. WFL-004 additionally requires its durable
`sensitive_ui_cleared` checkpoint:

```bash
python3 artifacts/tools/workflow_playwright.py trace-start \
  --case-dir "$ATTEMPT_DIR" \
  --sensitive-ui-cleared
```

Before deleting a private value file, close the case, finish all checkpoints,
and seal the hashes of the exact required evidence:

```bash
python3 artifacts/tools/workflow_playwright.py case-seal-redaction \
  --case-dir "$ATTEMPT_DIR"
```

Delete only that private file, then run the final check. A sealed case fails if
any evidence changes afterward:

```bash
python3 artifacts/tools/workflow_playwright.py case-check \
  --case-id WFL-003 --case-dir "$ATTEMPT_DIR"
```

For a trace created by the WFL-008 auth lane or a dedicated WFL-009 UI lane,
first export the raw zip below the run private directory, then sanitize it
without browser or service access:

```bash
python3 artifacts/tools/workflow_playwright.py sanitize-trace \
  --input "$PRIVATE_RAW_TRACE" \
  --output "$PRIVATE_SANITIZED_TRACE" \
  --private-values-file "$PRIVATE_VALUE_FILE"
```

## Evidence guidance for mutation workflows

- `WFL-003`: capture each form/save/refresh/cleanup state; author a bounded
  network transcript without request metadata fields that can carry secrets.
- `WFL-004`: trace after the secret boundary; capture Requests and Audit pages
  after runtime attribution is visible.
- `WFL-005`: capture routing-health list and event detail before reset, then
  author reset/recovery JSON from redacted response projections.
- `WFL-006`: index each settings mode snapshot; raw-download evidence must be a
  bounded redacted projection, not the downloaded body.
- `WFL-007`: capture pricing and usage details; calculate cost in JSON from
  visible/DB-safe numeric facts and restore the original currency.
- `WFL-008`: use the dedicated auth clone and two-page browser lane; capture
  only bounded transition, multi-tab, key-state, and storage projections.
- `WFL-009`: use the dedicated retention clone and a frontend proxy wired to
  that clone's backend; a synthesized text file or API-only retention run is
  not UI walk-through evidence.

Use `sanitize-json` only as a final validation/write gate for an already bounded
projection. It rejects ambiguous fields such as request metadata, credentials,
or raw bodies instead of silently stripping them.

## Validation

```bash
cd artifacts/tools
python3 -B -m unittest test_workflow_playwright.py test_run_workflow_cases.py
python3 -B run_workflow_cases.py self-test
ruff check --select E,F --ignore E501 \
  workflow_playwright.py run_workflow_cases.py \
  test_workflow_playwright.py test_run_workflow_cases.py
```
