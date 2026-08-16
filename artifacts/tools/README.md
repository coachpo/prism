# Recoverable matrix evidence runner

`matrix_runner.py` records a strict, crash-recoverable cursor for the local Prism
test matrix. It does not run commands or capture output. Test commands write only
deliberately redacted evidence; `record-result` stores relative references to it.
Runner-owned metadata is stored in `runner-manifest.json`; an existing full-run
`manifest.json` is never read, replaced, or otherwise modified.

The matrix is an ordered JSON object with `schema_version: 1` and a non-empty
`cases` array. Every case needs `id`, `title`, and `category` (or the equivalent
`phase` field); all five ordered phases
`SMK`, `FUN`, `INT`, `WFL`, and `RPL` must occur. Disabled cases, raw command/env/
header/body fields, and recognizable secret material are rejected.

Typical lifecycle:

```sh
python3 artifacts/tools/matrix_runner.py init \
  --run-dir artifacts/evidence/RUN_ID \
  --matrix /path/to/frozen-matrix.json \
  --fingerprint source_dump=sha256:HEX64 \
  --fingerprint branch_head=GIT_HEX \
  --fingerprint config=sha256:HEX64 \
  --fingerprint database_template=sha256:HEX64

python3 artifacts/tools/matrix_runner.py run-one \
  --run-dir artifacts/evidence/RUN_ID \
  --cycle primary \
  --database-clone sha256:HEX64

# Execute the returned case externally, redact its evidence, then finalize it.
python3 artifacts/tools/matrix_runner.py record-result \
  --run-dir artifacts/evidence/RUN_ID \
  --case-id SMK-001 \
  --status passed \
  --exit-code 0 \
  --assertions-total 3 \
  --assertions-passed 3 \
  --assertions-failed 0 \
  --evidence cases/SMK-001/primary-attempt-1/stdout.redacted.log

python3 artifacts/tools/matrix_runner.py resume-audit \
  --run-dir artifacts/evidence/RUN_ID

python3 artifacts/tools/matrix_runner.py summary \
  --run-dir artifacts/evidence/RUN_ID
```

If a completed result contains a transcription error in `defect.fix_commit` or
`fingerprints.branch_head`, preserve that `result.json` and every existing event.
Append an audited projection instead:

```sh
python3 artifacts/tools/matrix_runner.py record-correction \
  --run-dir artifacts/evidence/RUN_ID \
  --result-id RESULT_UUID \
  --field fingerprints.branch_head \
  --old WRONG_FULL_COMMIT \
  --new CORRECT_FULL_COMMIT \
  --reason "Correct manual transcription from retained attempt evidence."
```

The old value must match the current projected fact. The new full commit must
exist and be an ancestor of the worktree's current `HEAD`; use
`--git-work-tree /path/to/worktree` only when the run directory is outside that
worktree. The runner atomically writes a mode-0600 record below the mode-0700
`corrections/` directory, then appends a `correction_recorded` event and rebuilds
the checkpoint. Scans, checkpoints, and summaries use the corrected projection
while the summary retains the original result value and every correction step.
`resume-audit` reconciles a correction file if a crash occurs before its event
append.

`run-one` only permits the next matrix case. An unfinished `running` attempt is
converted to `interrupted` by `resume-audit`; the next `run-one` creates a new
attempt for the same case. Primary history may contain multiple fingerprint sets
when an in-cycle defect fix changes source: resume validates an explicitly
supplied fingerprint against the latest ordered attempt, and the next primary
attempt inherits that latest set by default. Regression cycles remain frozen to
exactly one fingerprint set and fail closed on drift. A primary failure can be retested in a new attempt and
closed as `passed_after_fix --defect-id BUG-N`. A regression failure invalidates
that cycle, so execution must begin again at `SMK-001` in the next consecutive
`regression-N`. A run is complete only when the latest regression cycle passes
every case without `passed_after_fix`. Every failed result requires a defect ID;
only the latest failed defect can be retried and closed, so a newly discovered
defect cannot be hidden by an older retry context.

For every attempt transition the durability order is: atomically replace
`result.json`, fsync and append `events.jsonl`, then atomically replace the derived
`checkpoint.json`. `resume-audit` reconciles a result whose event append was
interrupted. Fingerprints are hex digests only; pass the current frozen regression
fingerprints on its first `run-one`, and pass them again when independently
verifying that they did not change. The first case of every regression cycle
requires explicit fingerprints. Every finalized result requires an explicit exit
code, internally consistent assertion counts, every filename listed by the case's
`required_evidence`, and cycle-frozen `branch_head`, `config`, and
`database_template` fingerprints. Each `run-one` also requires the current
disposable `--database-clone` fingerprint; it is fixed for that attempt but may
change between cases and cycles. Evidence files must exist inside the run directory; the result
records their SHA-256 and size. Evidence must be a non-symlink file inside the
current exact attempt directory, cannot be reused across cases/cycles, and UTF-8
evidence is scanned for common credential/header/DSN/private-key patterns before
the result is accepted. A passing result requires at least one assertion. HTTP
cases may additionally use `--http-status`.

Run the self-tests with:

```sh
cd artifacts/tools && python3 -m unittest -v test_matrix_runner.py
```

## Authentication matrix and disposable browser lane

`run_auth_matrix.py` owns the local-only FUN-008 authentication lifecycle and
supplies recoverable browser/process/database primitives for WFL-008 and the
role-play owner's RPL-006 lane. It does not accept an RPL-006 formal attempt or
write its evidence, journal, result, or completion state. The helper accepts only those three case
IDs for its bounded primitives and maps them to fixed run-prefixed databases; gold, template, and
`prism_matrix_runtime` are never accepted as mutation targets.

Run FUN-008 only after the evidence runner has opened that exact attempt:

```sh
python3 artifacts/tools/run_auth_matrix.py run-fun-008 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/FUN-008/primary-attempt-1
```

The command writes exactly `auth-grid.redacted.json`,
`proxy-key-lifecycle.json`, `attribution-rows.json`, and `fault-results.json`.
One-time values never enter those files. The disposable clone is dropped and
its private fixture/config values are removed after the run. An interrupted
run can be recovered by invoking the same command for the runner's next
attempt; stale case state is safely reclaimed first.

Prepare or resume a real-browser lane without touching the default runtime:

```sh
python3 artifacts/tools/run_auth_matrix.py browser-prepare --case-id WFL-008
python3 artifacts/tools/run_auth_matrix.py browser-status --case-id WFL-008
python3 artifacts/tools/run_auth_matrix.py browser-open --case-id WFL-008
```

The safe status identifies the private fixture path and named Playwright
session without printing fixture values. Trace capture is intentionally not
automatic: login fields and one-time values must be hidden and acknowledged
before the explicit trace start.

```sh
python3 artifacts/tools/run_auth_matrix.py browser-trace-start \
  --case-id WFL-008 --sensitive-ui-cleared
python3 artifacts/tools/run_auth_matrix.py browser-close --case-id WFL-008 \
  --trace-output artifacts/evidence/20260813T204518Z/cases/WFL-008/primary-attempt-1/trace.zip
```

Trace export keeps the raw archive only below the run-private lane, forces it
to mode 0600, and delegates publication to the same text-only sanitizer used
by `workflow_playwright.py`. The sanitizer bounds and validates every ZIP
entry, rewrites textual local values and credential shapes, omits unverifiable
binary resources, emits `trace-redaction.json`, and rescans the public archive.
The raw archive is removed after a successful export. Do not start a trace
while a one-time value or password is visible.

Stop while retaining the clone for resumption, or explicitly remove only the
case clone and its private fixture after case cleanup:

```sh
python3 artifacts/tools/run_auth_matrix.py browser-cleanup --case-id WFL-008
python3 artifacts/tools/run_auth_matrix.py browser-cleanup --case-id WFL-008 \
  --drop-database --remove-private-values
```

Mutation-free harness validation:

```sh
python3 artifacts/tools/run_auth_matrix.py self-test
```

## FUN-007 routing harness

`run_fun007_routing_matrix.py` is the ignored, loopback-only executor for the
strategy/failover/Ban/admission/cancellation case. It creates only the exact
run-scoped `fun_007` clone through `db/db_lane.py`, seeds synthetic models and
Terminal Targets, starts the private backend binary on a free loopback port,
and drives the existing local mock. It writes exactly the six evidence files
declared by `FUN-007` and removes its mock scripts, temporary bootstrap,
backend process, and disposable database in `finally` cleanup.

Run its non-mutating safety checks first:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_fun007_routing_matrix.py --self-test
```

After the matrix runner has allocated the exact attempt directory, execute:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_fun007_routing_matrix.py \
  --attempt-dir artifacts/evidence/RUN_ID/cases/FUN-007/CYCLE-attempt-N
```

The full command intentionally performs run-scoped database creation and
deletion, so it requires the same local Docker authorization as `db_lane.py`.
The source private bootstrap is read in memory and copied to an OS-private
temporary file; neither it nor backend logs enter normal evidence.

## FUN-009 disposable observability fixture

`run_fun009_observability_fixture.py` owns the formal case lifecycle. It binds
to the runner's exact `running` attempt and frozen matrix, clones the template
as `prism_matrix_20260813t204518z_case_fun_009`, builds the clean attempt HEAD,
starts that binary on loopback port `18109`, and creates success, failover,
stream, failure, and Client Rule negative-control traffic against namespaced
scripts on the shared local mock. After telemetry/outbox/coverage quiescence it
invokes `run_stats_observability_case.py` as the read-only collector:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_fun009_observability_fixture.py \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/FUN-009/CYCLE-attempt-N
```

The executable in the command is `run_fun009_observability_fixture.py` (the
collector is not called directly by an operator). The wrapper always stops its
exact backend, deletes only its mock-script namespace, and drops the exact
clone. Exit `1` means trustworthy product assertions failed; exit `2` means an
environment, fixture, cleanup, or evidence failure.

The six emitted files match `FUN-009.required_evidence`. The harness builds a
Default-profile, bounded-window database oracle and compares exact public list,
detail, chain, audit, filter, aggregate, activity, Terminal Target, and cost
segment projections. It exports every selected ingress, verifies server CSV
metadata plus normalized JSON parity, paginates capped endpoints, and recomputes
trusted price components and FX with the persisted snapshots. Raw bodies,
headers, URLs, opaque query-context capabilities, and endpoint labels are not
written; endpoint labels are represented only by a short SHA-256 digest. The
attempt directory must otherwise be empty; the runner-owned `result.json` is
the only pre-existing file accepted.

## Disposable retention helper

`run_fun010_retention_fixture.py` is the sole formal owner for `FUN-010`.
It fences the exact runner-allocated attempt, frozen matrix/config/template/
source/clone fingerprints, clean branch HEAD, and ignored harness hashes before
building a case-private backend with the pinned offline Go toolchain. The exact
hash inventory covers the wrapper, retention helper, local support, database
lane, matrix runner, and `mock_provider.py`. Before and after the attempt the
wrapper also binds the listener on `127.0.0.1:18081` to that exact resolved mock
source path, source digest, Python command shape, and fixed host/port arguments
without recording its raw command or environment. The tail
owner `run_int_tail_cases.py` separately invokes `run_retention_case.py` for
`INT-006` after building its current-HEAD backend. Direct `FUN-010` and
`INT-006` helper execution is rejected so those provenance checks cannot be
bypassed.
Its reusable `RetentionHarness` also supplies only the disposable
retention-copy portion of `RPL-005` to the roleplay owner. Each run creates one exact
`run_id`-qualified database from the frozen template and starts a private
loopback backend. The helper inserts rows only into partitions that already
exist. Only Prism's retention worker drops partitions or deletes retention
scope data; cleanup validates and removes only that disposable clone.

```sh
# Before matrix allocation, create and fingerprint the exact disposable clone.
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_fun010_retention_fixture.py --prepare-clone

# Copy the returned digest and all four returned fingerprints into allocation.
python3 artifacts/tools/matrix_runner.py run-one \
  --run-dir artifacts/evidence/20260813T204518Z \
  --case-id FUN-010 \
  --database-clone CLONE_HEX64 \
  --fingerprint branch_head=PREPARED_HEAD \
  --fingerprint config=sha256:CONFIG_HEX64 \
  --fingerprint database_template=TEMPLATE_HEX \
  --fingerprint source_dump=sha256:SOURCE_HEX64

PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_fun010_retention_fixture.py \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/FUN-010/CYCLE-attempt-N
```

The final command must consume the exact `attempt_dir` returned by `run-one`;
do not reconstruct or reuse an attempt path. After a crash or exit `2`, first
run `matrix_runner.py resume-audit`, then run `--prepare-clone` again so stale
process, receipt, and clone state is reconciled before allocating a new attempt.

Preparation runs only while the runner cursor is at FUN-010 with no active
attempt. It records a mode-0600 journal containing the clone's cluster/name/OID/
flag identity plus an initial public schema/data integrity digest captured
immediately after the exact frozen-template clone operation. Content capture
refuses another database session and uses bounded server and process-group
timeouts. The runner allocation, journal, and live physical/content
fingerprints must agree before any fixture mutation; the helper rechecks both
identities immediately before seeding.
The wrapper exclusively reconciles a recorded prior FUN-010 process and adopts
that exact receipt-owned clone under the runner and case locks; it never passes `--recreate` or a
keep flag. It reserves cleanup time, independently proves the private backend
identity is absent, port `18110` is free, and the exact clone is gone, then
counts those facts plus current-HEAD binary provenance in
`retention-job-events.json`. Exit `1` retains trustworthy product failures;
provenance, deadline, cleanup, secret-scan, or evidence failures exit `2`.
Valid-request HTTP status and malformed response-contract failures use a narrow
phase-specific product-error allowlist and emit all five assertion-shaped,
secret-safe evidence documents before returning `1`; transport, readiness,
setup, database-oracle, process-identity, and cleanup failures remain exit `2`.
`FUN-010` performs a complete draft/chunk/seal/preview/commit currency cutover,
binds every priced row to the exact template revision and effective time,
proves prior telemetry is immutable, and restores the baseline currency and
prices before retention. A clone-local one-shot guard makes the first queued
cancel deterministic. The helper then requires an orderly restart of the same
binary/config/port, re-reads the same cancelled job, and only then runs the four
managed datasets to completion with exact boundary-partition names and bounds.
Each managed table includes a canary whose `created_at` equals the cutoff, and
the final state must retain all four canaries. Restart equality remains strict
for rows, partitions, policy, settings, coverage cuts/revisions/hashes/sources,
and every other coverage field; only startup-refreshed coverage
`materialized_at` and `updated_at` timestamps are normalized for that comparison.

## FUN-011 frontend regression owner

`run_frontend_regression_case.py` is the sole formal owner for `FUN-011`. It
accepts only the exact runner-allocated, latest, unique `running` attempt at the
campaign cursor. Before writing evidence it validates the strict `result.json`
schema, matching `case_started` event, rebuilt checkpoint, frozen matrix and
manifest fingerprints, current clean branch HEAD, non-ignored and ignored
source-impact gates, and an attempt containing only the immutable runner-owned
`result.json`. The case and runner locks remain held through final evidence
sealing; the initial result digest and all runner control-file digests are
rechecked before return.

Run only the pure self-tests and non-browser collection check before allocation:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_frontend_regression_case.py --self-test

cd artifacts/tools && PYTHONDONTWRITEBYTECODE=1 \
  python3 -m unittest -v test_frontend_regression_harness.py

PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_frontend_regression_case.py --static-check
```

After `FUN-010` has been recorded and the checkpoint names `FUN-011`, allocate
the frontend-only lane with its fixed no-database fingerprint, then pass the
returned attempt path unchanged to the owner:

```sh
python3 artifacts/tools/matrix_runner.py run-one \
  --run-dir artifacts/evidence/20260813T204518Z \
  --case-id FUN-011 \
  --database-clone e50ad2b12417062f63c4ebfc1412bb7c28eedb478d4f5eaf6c8540291b85e389

PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_frontend_regression_case.py \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/FUN-011/CYCLE-attempt-N
```

The formal owner uses an exact child-process allowlist and freezes/rechecks the
ignored harness, `run_code_gates.py`, `matrix_runner.py`, shared workflow
sanitizer, package and checked/installed lockfiles, complete frontend test
inventory, installed test runner inventory, Node, pnpm, and the selected browser
binary. Every spawned test or server group receives a mode-0600 receipt bound to
the exact result, process identity, planned arguments digest, binary digest, and
owned port where applicable. SIGINT/SIGTERM and timeouts use verified
process-group cleanup. The 1800-second contract reserves 45 seconds for cleanup
and 15 seconds for evidence finalization.

Playwright runs with a private working directory so the narrow accessibility
spec's relative screenshots, runner output, report, traces, and console log can
only land below the mode-0700 run-private lane. Public traces are new,
mode-0600 ZIPs containing redacted UTF-8 entries plus a sanitizer manifest;
binary trace resources and standalone screenshots/videos are omitted and only
hash-bound in `playwright-traces-index.json`. The raw staging directory is
deleted after sealing. Before/after inventories prove `frontend/artifacts` was
absent and every sibling attempt stayed unchanged. Each of the five required
evidence files embeds the complete assertion ledger and before/after provenance.

After a crash, do not reuse an attempt containing partial evidence. Reconcile
only receipt-owned processes and private staging, run the matrix resume audit,
then allocate a new attempt:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_frontend_regression_case.py --reconcile-only
```

Exit `0` means all regressions and trust assertions passed, `1` means sealed
product-test failure evidence, `2` means an allocation, provenance, cleanup, or
evidence-integrity failure, and `130` means an interrupted formal case.

Formal WFL-001..WFL-010 execution first runs
`run_workflow_cases.py prepare-case --case-id WFL-NNN` while that case is the
matrix checkpoint. The returned physical `database_clone` fingerprint is then
passed unchanged to the external `matrix_runner.py run-one` allocation together
with every returned `runner_fingerprints` entry (`branch_head`, `config`,
`database_template`, and `source_dump`). Only after allocation may the exact
returned attempt path be passed to
`run_workflow_cases.py run-case`. The owner never allocates or records an
attempt. Preparation writes its receipt before building, can reconcile only
receipt-owned orphan artifacts, and binds the prepared clone to the formal
attempt before services start. Immediately before fixture or service/browser
consumption it revalidates the exact mode-0600 generated artifacts and their
semantic bindings, physical clone identity, full DB-lane hash of all public
rows/schema objects, and pinned runtime/source set. The owner holds the runner lock through the
entire scenario, frozen evidence sealing, cleanup, and final control recheck.
WFL-001, WFL-002, and WFL-010 have no direct readonly CLI path: this owner
journals their private mock, backend, Vite, clone, named browser, trace, close,
and scratch purge just like the mutation lanes. One absolute matrix deadline
reserves 90 seconds for cleanup and still bounds cleanup plus the formal
postcheck.
An assertion failure returns `1` only after all required evidence is sealed;
infrastructure or finalization rejection returns `2`.
Child-process receipts persist `launching`, `spawned`, and verified `started`
phases. Crash recovery never treats a free listener as proof that spawn did not
happen: it scans the exact command markers inside a bounded launch window and
signals only one uniquely matched self-led process group, retaining ambiguous
receipts for manual inspection.

`WFL-009` is owned by the browser workflow runner because its preflight,
confirmation, cancellation, restart, completion, and trace must all be bound
to the same Playwright case/attempt/job. Direct `WFL-009` and `RPL-005`
retention-helper invocations are rejected rather than emitting partial evidence.
`run_workflow_cases.py run-case --case-id WFL-009 --attempt-dir ...` is the
sole WFL-009 executable owner after the exact clone has been prepared: it seeds
only that disposable clone,
temporarily defers the first clone-local manual request-log job so the real UI
queued-cancellation branch is deterministic. A clone-local transactional guard
makes this independent of retained terminal job history, while inherited
nonterminal retention jobs are rejected before startup. The 720-second defer
outlives the enforced frozen 600-second whole-case deadline. It then runs the second real UI job
to completion and verifies the refreshed UI plus DB/partition controls. It
never invokes the direct retention helper or uses its artifacts as WFL
evidence. Preflight timestamps must be UTC, exactly five minutes apart, and
fresh at confirmation; UTC-day consistency is rechecked at each destructive
boundary. Queued cancellation must leave the four managed datasets, all
partition projections, purge/policy/epoch/coverage state, retention settings,
and audit fence unchanged. A deliberately held queued-state poll is released
after cancellation and must not overwrite the terminal row. An older cancelled-job
detail response is also released only after the completed-job dialog renders;
it must not replace the selected job. From a clean tracked worktree the owner
builds a network-disabled case-private backend and records its exact
HEAD/source/toolchain/binary provenance. It pins the selected Chromium app bundle,
complete installed frontend package store, source/config tree, offline CLI,
and cleanup dispatchers, and child services cannot inherit an ambient
`DATABASE_URL` or proxy/Prism/Vite override. The trace
sanitizer utility requires the complete
`trace.trace`/`trace.network` archive shape, bounds every entry, rewrites
preflight capabilities and recognizable auth/cookie/key/DSN material, and
normalizes untrusted ZIP metadata while rejecting symlinks and sensitive entry
names. It rescans every decompressed entry before publishing the mode-0600 archive.
Raw trace/network/resource components are removed after packaging, and a
post-publication validation failure removes the rejected public archive.
Without a valid safe trace the owner reports a code-only failure and cannot pass.
Once the runner durably records a passed WFL attempt, call the owner's
`cleanup-case` for that exact attempt before the regression cycle; this
archives the completed private journal and frees the fixed case lane for a
fresh backend build, clone, and browser session.
The roleplay helper acts only on an extra retention copy; it does not mutate the
main roleplay database. Retention fixtures use existing partitions; only the
product retention worker may delete rows or drop partitions. The attempt
directory accepts only the runner-owned `result.json` before execution.

Run the non-destructive harness self-tests with:

```sh
cd artifacts/tools && PYTHONDONTWRITEBYTECODE=1 \
  python3 -m unittest -v \
  test_stats_retention_harness.py test_fun010_retention_fixture.py test_db_lane.py
```

## INT-006 through INT-008 local tail runner

`run_int_tail_cases.py` binds each invocation to the exact runner-allocated
attempt, frozen matrix SHA-256, isolated worktree/branch, and exact
`required_evidence` list. Every JSON evidence document carries the frozen
`run_id`; a mismatched or missing provenance field fails closed. It delegates `INT-006` to the
disposable retention helper, where the first accepted completion job is proven
non-terminal after an exact isolated-backend SIGKILL and then reaches
`succeeded` under the same job ID after the loopback backend restarts. Clone
identity evidence is finalized only after drop-and-absence verification. It
runs `INT-007` as the complete
`./tests/priority/...` package tree, and runs the six exact `INT-008` commands
into the five frozen backend/frontend log artifacts. The integration scratch
schema is routed to the private run tree so `TestDumpMigratedSchema` runs
instead of becoming an unapproved skip; inherited fixture-update flags are
removed, Go module writes are read-only, and the two fixed `/tmp` debug files
owned by the runtime suite must be absent before execution and are removed
afterward. The code-gate cases require the already-cached Go, pnpm,
modules, `node_modules`, and `postgres:16-alpine` image; dependency and image
downloads are disabled and fail closed.
Before INT-006 starts its clone, the tail owner builds a case-private backend
from the current clean campaign HEAD with Go 1.26.5, `-buildvcs=true`, read-only
modules, and offline dependency resolution. The embedded VCS revision and
`vcs.modified=false` must match the pre-case HEAD; its SHA-256 and current-HEAD
assertion are retained in `clone-identity.json`. The build and retention flow
are each bounded; the product retention flow retains the matrix-frozen
600-second deadline exactly.

Run only the mutation-free contract checks before the formal cases:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_int_tail_cases.py --self-test

cd artifacts/tools && PYTHONDONTWRITEBYTECODE=1 \
  python3 -m unittest -v test_int_tail_harness.py
```

After `matrix_runner.py run-one` allocates the exact attempt directory, execute
one case at a time:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_int_tail_cases.py \
  --case-id INT-006 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/INT-006/CYCLE-attempt-N

PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_int_tail_cases.py \
  --case-id INT-007 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/INT-007/CYCLE-attempt-N

PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_int_tail_cases.py \
  --case-id INT-008 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/INT-008/CYCLE-attempt-N \
  --main-workspace /Users/qingli/Documents/proj/prism
```

The runner holds a campaign-local lock, stops at the first failed gate, writes
truthful placeholders for later required artifacts, kills the full child
process group on timeout, and removes only newly observed branch-scoped
PostgreSQL test containers. A pre-existing scoped container blocks the case;
it is never silently adopted or deleted. Exit `0` means the case and evidence
passed, `1` means a test gate failed, `2` means the environment, cleanup, or
evidence contract failed, and `130` means the case was interrupted.
The Go JSON parser retains complete package and skip inventories outside the
bounded human log; an incomplete skip inventory, unexpected package in
`INT-007`, skipped package, or non-launcher skipped test in `INT-008` fails the
case. INT-006 additionally proves the tracked worktree stayed clean and on the
frozen campaign branch before and after the destructive clone-only flow.

After `matrix_runner.py resume-audit` marks a hard-interrupted predecessor and
`run-one` allocates its next attempt, restart INT-006 with `--recreate` so only
its exact run-qualified clone is replaced. Restart INT-007 or INT-008 with
`--reconcile-interrupted-resources`; that flag is accepted only when the
immediately preceding same-cycle attempt is durably `interrupted`, and removes
only exact branch-scoped `postgres:16-alpine` test containers before taking a
fresh baseline. Ordinary failed retries do not accept this recovery authority.

## Shared role-play lane

`run_roleplay_matrix.py` is the sole formal owner of `RPL-001` through
`RPL-006`. The cases run in matrix order on the exact disposable `rpl_006`
clone created through the authentication helper's bounded primitives, so the
final `RPL-006` security actor continues from the same retained
history and private application-key fixture. Gold, template, and active runtime
databases are rejected as mutation targets. Every upstream URL is the fixed
loopback mock; the mock's outbound-disabled health contract is asserted.

The role-play state and append-only hash-chained timeline live below
`artifacts/evidence/20260813T204518Z/private/roleplay/`. A retry uses a fresh
matrix attempt directory and resumes the first role-play case not durably marked
passed. It never overwrites evidence from a prior attempt. `RPL-005` delegates
retention deletion to an additional disposable copy and removes that copy after
projecting its result, preserving the shared role-play clone. Its currency proof
uses the real draft/preview/commit workflow, changes code and epoch while
applying a deterministic factor-two fixture price conversion, proves the
source-aligned `DEFAULT_1_TO_1` runtime snapshot on the migrated revision, then
uses a factor-one-half cutover to restore the original code and price inventory.

Every dynamic case has a process-wide monotonic/SIGALRM execution deadline.
Successful cases re-arm to the absolute frozen timeout for cleanup and evidence;
failure paths receive an independently active 60-second cleanup/evidence bound.

Run the mutation-free static and journal self-test first:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_roleplay_matrix.py --self-test
```

After `matrix_runner.py run-one` has allocated the exact case attempt, run one
case without putting private values in argv:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_roleplay_matrix.py \
  --case-id RPL-001 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/RPL-001/primary-attempt-1
```

The command emits exactly the filenames frozen in that case's
`required_evidence`. Browser traces are captured only for `RPL-001`, `RPL-003`,
`RPL-004`, and `RPL-006`; raw snapshots/traces are checked inside the private lane, then
the existing Playwright redaction is packaged into private staging and moved
atomically into evidence only after a second scan. Do not run a later role-play
case until the runner has recorded its predecessor as passed. `RPL-006` owns
the auth transition, key rotation/revocation, two-page logout/open-mode proof,
secret scan, private-value cleanup, and the final retained clone handoff.

If the process is interrupted after the attempt-bound finalization receipt is
prepared, resume only that still-running RPL-006 attempt through the explicit
recovery path. It revalidates the runner controls, HEAD, clone identity/content,
receipt, private cleanup state, exact evidence, journal ordering, and marker
before restoring `ready_for_record`:

```sh
PYTHONDONTWRITEBYTECODE=1 \
  python3 artifacts/tools/run_roleplay_matrix.py \
  --case-id RPL-006 \
  --attempt-dir artifacts/evidence/20260813T204518Z/cases/RPL-006/primary-attempt-1 \
  --cycle primary \
  --recover-finalization
```

After the matrix runner durably records the result, use
`--reconcile-case RPL-006 --cycle primary` to advance the role-play state from
`ready_for_record` to `passed`.
