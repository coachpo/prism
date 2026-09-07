# Ingress-chain read-path benchmark

CI does not run this directory. The correctness suites work with a handful of
rows, and every bottleneck this harness exists to measure only appears once a
retention window holds a realistic number of rows *and* partitions: partition
pruning, per-page recomputation, `work_mem` spills, and the JIT compilation
cliff are all invisible at a few thousand rows.

Run it by hand before and after any change to the ingress-chain read path and
keep both sets of numbers.

## Running it

```bash
cd backend/tests/perf
./bench.sh up      # benchmark PostgreSQL + migrations (takes ~1 min)
./bench.sh load    # generate and load the corpus (30d x 35k takes ~10 min)
./bench.sh api     # end-to-end API timings
./bench.sh explain # auto_explain plans for the same statements
./bench.sh down    # tear everything down
```

`./bench.sh all` chains them. Size and shape are environment knobs — see the
header of `bench.sh`. A quick smoke run:

```bash
BENCH_DAYS=3 BENCH_PER_DAY=2000 ./bench.sh all
```

Requirements: `docker`, `go`, `python3`, `curl`.

## What it builds

`generate_load.py` synthesizes the corpus from the template rows in
`templates.jsonl` using `json_populate_record(template, overrides)`. The
templates are complete, schema-shaped rows, so the generator inherits every
column the schema currently has and a new NOT NULL column or CHECK constraint
surfaces as a load failure instead of a silently different corpus. Only
per-row identity is overridden: id, ingress ID, timestamps, and the attempt
shape. The values in the templates are synthetic.

The chain mix mirrors what a live instance shows: ~98% single-row ingresses,
~1.2% three-row failover chains, ~1.5% failures, one finalized usage event per
ingress. A flatter mix would understate per-ingress work.

Two load-path details matter and are handled by `bench.sh`:

- The retained-history triggers are disabled during the load. The
  retention-coverage append trigger upserts one shared row per statement, so
  parallel loaders would otherwise queue on that single tuple.
- After the load the triggers come back, the coverage projection is marked
  dirty, and the backend is restarted so its startup owner recomputes
  coverage. Without that restart the API clamps every requested window to the
  pre-load `latest_retained_at` and measures nothing.

## Reading the output

`./bench.sh api` reports the first sample separately from the median: a cold
plan cache, planning time across many partitions, and per-execution JIT
compilation all show up in the spread rather than the mean. Deep-pagination
rows exist to expose per-page recomputation, which page 1 hides by
construction.

`./bench.sh explain` captures plans with `auto_explain` over the real API
surface instead of a hand-copied SQL suite: a copy drifts from the Go query
builders as soon as either changes, and a drifted plan is worse than none.

## Reference numbers

Measured on 30 days x 35k ingresses (1,075,260 `request_logs` rows across 45
partitions, 1,050,000 usage events), stock `shared_buffers=128MB` /
`work_mem=4MB`, tmpfs storage, WSL2. Median of six samples per case; "before"
is v1.1.10 and "after" is the ingress-chain read-path change, both run
back to back against the same corpus. These are shape indicators, not
thresholds — absolute numbers depend on the host.

| Case | Before | After |
| --- | --- | --- |
| 24h window, page 1 | 859 ms | 92 ms |
| 7d window, page 1 | 498 ms | 304 ms |
| 30d window, page 1 | 1836 ms | 1559 ms |
| 30d window, page 5 | 2169 ms | 1558 ms |
| 7d + 5xx row filter | 596 ms | 317 ms |
| 7d + 2xx row filter | 4256 ms | 543 ms |
| 7d + final_result filter | 633 ms | 354 ms |
| exact ingress, no window | 63 ms | 85 ms |
| attempts view, 7d, limit 100 | 16 ms | 15 ms |

What the numbers say:

- Paging is now flat. Before, page 5 cost more than page 1 because the outer
  set was re-derived from a whole-window aggregate on every page; the keyset
  walk reads only the rows it returns.
- The remaining cost on a wide window is the full-cohort totals, an exact
  aggregate over every retained row in the window. At 30 days that is about
  1.5 s of the 1.56 s, and it is the same on every page. Measured
  alternatives on this corpus: the current group-then-sum takes 1.92 s,
  `COUNT(DISTINCT ingress_request_id)` 1.87 s, a flat row aggregate 195 ms,
  and the ingress count read from `usage_request_events` 152 ms. Only the last
  pair is cheap, and it stops counting chains that have no usage event, so the
  totals would no longer match the item list.
- The exact-ingress read costs about 20 ms more than before: it now probes
  `usage_request_events` and `request_logs` instead of only `request_logs`,
  once per partition. The cost grows with the partition count, not the row
  count.
