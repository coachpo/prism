# Direct-request entry reclassification (operator runbook)

This tracked operator bundle is the one-off Default-profile data change that
follows the generic `000032_model_direct_request_enabled.sql` migration. It is
shipped for explicit operator use but is never invoked by startup. The release
migration intentionally keeps every retained model directly requestable; this
runbook applies the operator-approved 12/4 classification without turning the
instance IDs into a generic migration rule.

The executable companion is
`artifacts/plans/direct-request-entry-reclassification.sql`. It defaults to a
read-only, serializable preview. Apply mode is impossible unless the operator
supplies the exact apply, verified-backup, quiesce, and database-name tokens.
This task does not execute either mode against the home-LAN instance.

The runbook is bound to the reviewed SQL payload:

```text
SQL SHA-256: d4afecb631dc70fa974d96cf0a877958ee3187c64675092260c6729b0fd396d0
```

The canonical disposable acceptance runner is
`artifacts/plans/direct-request-entry-reclassification-test.py`. It refuses to
start PostgreSQL unless this runbook, the SQL, and the runner are all visible to
`git ls-files`, the SQL hash matches the value above, and `STATUS.md` describes
the bundle as shipped rather than ignored or unshipped.

## Accepted identity set

Direct entries (exact and case-sensitive):

```text
codex/codex-auto-review
codex/gpt-5.6-terra
deepseek-v4-flash
deepseek-v4-pro
glm-5.3-flash
codex/gpt-image-2
codex/gpt-5.4-mini
codex/gpt-5.5
codex/gpt-5.6-luna
gpt-5.6-luna
muse-spark-1.2-contributor
qwen3.8-flash
```

The approved spelling map is recorded as non-entry identity → direct entry
identity:

```text
DeepSeek-V4-Flash              → deepseek-v4-flash
deepseek/deepseek-v4-flash-0731 → deepseek-v4-flash
deepseek/deepseek-v4-pro        → deepseek-v4-pro
z-ai/glm-5.3-flash              → glm-5.3-flash
```

Model Target edges use the entry/parent as `source_model_config_id` and the
non-entry spelling as `target_model_config_id`:

```text
deepseek-v4-flash --Model Target--> DeepSeek-V4-Flash
deepseek-v4-flash --Model Target--> deepseek/deepseek-v4-flash-0731
deepseek-v4-pro   --Model Target--> deepseek/deepseek-v4-pro
glm-5.3-flash     --Model Target--> z-ai/glm-5.3-flash
```

The four right-hand IDs become `direct_request_enabled=false`. Existing edges
are retained byte-for-byte, including ID, position, and enabled state. A
missing edge is appended enabled at the end of its parent’s mixed target list,
with the two Flash children using the order shown above. No model or connection
`is_enabled`, Terminal Target, connection ownership, upstream identity, log,
usage event, pricing row, or directory binding is changed.

## Safety boundary

Apply requires all of the following:

1. Deploy the schema/code upgrade and confirm migration 000032 succeeded.
2. Create and validate a current Prism PostgreSQL plus plaintext-config backup.
3. Stop the Prism application while leaving PostgreSQL available. Keep it
   stopped for the whole transaction; do not permit concurrent management
   writes.
4. Resolve the exact target database name and use an operator connection with
   access only to that database.
5. Save preview output and compare it with the approved 16 IDs and four edges.

The SQL takes stable row locks in apply mode and validates every condition
before its first persistent write: the exact 16-row set, Default profile,
000032 schema and migration-history stamp, parent/child family and both OpenAI
dimensions, edge uniqueness and enabled state, dense affected-parent positions,
prospective acyclicity, and the three enabled Flash logical/upstream identities.
Any mismatch aborts the serializable transaction with zero persistent writes.

The application’s HTTP middleware is normally the sole generation writer.
Because this is a quiesced offline batch, the SQL performs the equivalent four
planning-generation bumps and one route-witness-generation bump in the same
transaction, but only when at least one model bit or edge actually changes.
Restarting Prism after commit discards every old in-memory snapshot. A true
second run changes neither business rows nor generations.

## Preview

With Prism still running, preview is read-only and requires no confirmation
tokens:

```bash
psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -f artifacts/plans/direct-request-entry-reclassification.sql
```

The preview rolls back. It prints all 16 current/desired entry bits, all four
correct parent-to-child edges with existing ID/position/state or `will_append`,
and the exact Flash Terminal Target identities. Stop on any SQL exception or
unexpected row.

## Apply

After the backup is verified and Prism is stopped, substitute the exact
database name shown by `SELECT current_database()`:

```bash
psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v apply_token=APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1 \
  -v backup_token=BACKUP_VERIFIED \
  -v quiesce_token=PRISM_STOPPED \
  -v expected_database=prism \
  -f artifacts/plans/direct-request-entry-reclassification.sql
```

The transaction updates only mismatched qualification bits and inserts only
missing approved edges. It then proves `is_enabled` and every pre-existing
target row stayed unchanged, proves the exact qualification/edge postcondition,
bumps generations if and only if business rows changed, and commits. The final
summary is instance-dependent: it reports one `model_qualification` row for
each bit that actually changes and one `model_target_append` row for each
missing edge. This worktree has not read or written a running instance, so it
makes no claim about which of the four edges already exist.

## Post-commit verification

1. Run the preview again before restart. Every `will_update` and `will_append`
   value must be false; apply mode would report no change rows and would not
   advance generations.
2. Restart Prism only after that no-op preview passes.
3. Read `/api/models`: the direct set must be exactly the 12 IDs above and the
   other four rows false; compare all model `is_enabled` values with the saved
   preview.
4. Read `/v1/models` and Pi export source: neither may include the four
   non-entry IDs.
5. Read model/setup readiness: only direct roots count, while the parent model
   diagnostics still traverse each non-entry child.
6. Confirm `deepseek-v4-flash` retains its own Terminal Target plus the two
   Model Targets and that the three exact upstream identities remain
   `deepseek-v4-flash`, `DeepSeek-V4-Flash`, and
   `deepseek/deepseek-v4-flash-0731`.
7. Do not send a real provider request merely to validate this migration.

Save preview/apply/no-op output and before/after inventory checks under
`artifacts/evidence/`. If any postcondition fails, leave Prism stopped and use
the verified backup/restore procedure; do not improvise another SQL repair.

## Disposable acceptance runner

The canonical runner exercises this SQL without reading the home-LAN database
or opening port 8088. It starts an unexposed disposable
`postgres:16-alpine` container, verifies preview, existing-edge and missing-edge
success, verifies a second apply is a true no-op, and proves every precondition,
inventory, compatibility, edge-state, position, cycle, and DeepSeek-identity
failure leaves both business rows and generation rows unchanged:

```bash
python3 -B artifacts/plans/direct-request-entry-reclassification-test.py
```

When `CLOSED_LOOP_EVIDENCE_DIR` is set, the runner writes the concise
`c7-summary.json` evidence file there. The runner is local acceptance support,
not a production migration command.
