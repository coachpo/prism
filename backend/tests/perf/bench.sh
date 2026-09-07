#!/usr/bin/env bash
# Ingress-chain read-path benchmark.
#
# CI never runs this: correctness suites use a handful of rows, and every
# bottleneck this harness exists to measure only appears once the retention
# window holds a realistic number of rows and partitions. Run it by hand
# before and after a read-path change and keep both numbers.
#
#   ./bench.sh up          start the benchmark PostgreSQL and migrate it
#   ./bench.sh load        generate and load the corpus, then rebuild coverage
#   ./bench.sh explain     capture auto_explain plans for the real API statements
#   ./bench.sh api         end-to-end API timings against the local backend
#   ./bench.sh all         up + load + explain + api
#   ./bench.sh down        remove the container and the work directory
#
# Environment knobs:
#   BENCH_DAYS      history days to generate                  (default 30)
#   BENCH_PER_DAY   ingresses per day                         (default 35000)
#   BENCH_END_DATE  last generated day, YYYY-MM-DD            (default today)
#   BENCH_WORK_DIR  scratch directory                         (default /tmp/prism-chain-bench)
#   BENCH_PORT      host port for the benchmark PostgreSQL    (default 15433)
#   BENCH_API_PORT  host port for the benchmark backend       (default 18000)
#   BENCH_JOBS      parallel loader workers                   (default 4)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

BENCH_DAYS="${BENCH_DAYS:-30}"
BENCH_PER_DAY="${BENCH_PER_DAY:-35000}"
BENCH_END_DATE="${BENCH_END_DATE:-$(date -u +%F)}"
BENCH_WORK_DIR="${BENCH_WORK_DIR:-/tmp/prism-chain-bench}"
BENCH_PORT="${BENCH_PORT:-15433}"
BENCH_API_PORT="${BENCH_API_PORT:-18000}"
BENCH_JOBS="${BENCH_JOBS:-4}"

CONTAINER=prism-chain-bench
DB_URL="postgres://bench:bench@127.0.0.1:${BENCH_PORT}/bench?sslmode=disable"
API_BASE="http://127.0.0.1:${BENCH_API_PORT}"
PSQL=(docker exec -i -e PGPASSWORD=bench "$CONTAINER" psql -U bench -d bench -X -q -v ON_ERROR_STOP=1)

log() { printf '\n== %s ==\n' "$*"; }

require() {
	command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

# The corpus lives on a tmpfs so a load is I/O-bound on nothing but PostgreSQL
# itself. shared_buffers/work_mem are pinned to the stock values the shipped
# compose file runs with: raising them here would hide the exact planner
# behaviour the benchmark is meant to expose. The shared-memory segment is
# raised because Docker's 64 MB default is too small for parallel workers on
# this corpus, and a query that fails on dynamic shared memory measures
# nothing.
cmd_up() {
	require docker
	require python3
	log "starting benchmark postgres on ${BENCH_PORT}"
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	docker run --rm -d --name "$CONTAINER" \
		-e POSTGRES_DB=bench -e POSTGRES_USER=bench -e POSTGRES_PASSWORD=bench \
		-p "${BENCH_PORT}:5432" \
		--tmpfs /var/lib/postgresql/data:rw,size=8g \
		--shm-size=1g \
		postgres:16-alpine \
		-c shared_buffers=128MB -c work_mem=4MB -c max_connections=100 \
		-c autovacuum_naptime=10s -c checkpoint_timeout=30min -c max_wal_size=4GB >/dev/null

	for _ in $(seq 1 60); do
		if docker exec "$CONTAINER" pg_isready -U bench -d bench >/dev/null 2>&1; then break; fi
		sleep 1
	done
	docker exec "$CONTAINER" pg_isready -U bench -d bench >/dev/null

	mkdir -p "$BENCH_WORK_DIR"
	log "building prism-backend"
	(cd "$BACKEND_DIR" && go build -o "$BENCH_WORK_DIR/prism-backend" ./cmd/prism-backend)
	render_config
	log "running migrations and partition provisioning through the backend"
	start_backend
	stop_backend
	echo "benchmark database ready: $DB_URL"
}

render_config() {
	sed -e "s#@DATABASE_URL@#${DB_URL}#" -e "s#@PORT@#${BENCH_API_PORT}#" \
		"$SCRIPT_DIR/config.template.json" > "$BENCH_WORK_DIR/config.json"
}

start_backend() {
	PRISM_CONFIG_PATH="$BENCH_WORK_DIR/config.json" "$BENCH_WORK_DIR/prism-backend" \
		>"$BENCH_WORK_DIR/backend.log" 2>&1 &
	echo $! > "$BENCH_WORK_DIR/backend.pid"
	for _ in $(seq 1 120); do
		if curl -fsS "$API_BASE/health" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	echo "backend did not become healthy; see $BENCH_WORK_DIR/backend.log" >&2
	tail -40 "$BENCH_WORK_DIR/backend.log" >&2
	exit 1
}

stop_backend() {
	[ -f "$BENCH_WORK_DIR/backend.pid" ] || return 0
	kill "$(cat "$BENCH_WORK_DIR/backend.pid")" 2>/dev/null || true
	wait "$(cat "$BENCH_WORK_DIR/backend.pid")" 2>/dev/null || true
	rm -f "$BENCH_WORK_DIR/backend.pid"
}

disable_auto_explain() {
	"${PSQL[@]}" \
		-c "ALTER SYSTEM RESET session_preload_libraries" \
		-c "ALTER SYSTEM RESET auto_explain.log_min_duration" \
		-c "ALTER SYSTEM RESET auto_explain.log_analyze" \
		-c "ALTER SYSTEM RESET auto_explain.log_buffers" \
		-c "ALTER SYSTEM RESET auto_explain.log_nested_statements" \
		-c "SELECT pg_reload_conf()" >/dev/null 2>&1 || true
}

cmd_load() {
	require docker
	require python3
	mkdir -p "$BENCH_WORK_DIR"
	log "generating corpus SQL (${BENCH_DAYS}d x ${BENCH_PER_DAY})"
	python3 "$SCRIPT_DIR/generate_load.py" --days "$BENCH_DAYS" --per-day "$BENCH_PER_DAY" \
		--end-date "$BENCH_END_DATE" --out "$BENCH_WORK_DIR/corpus.sql"

	# The retention-coverage trigger writes one shared row per statement, so
	# parallel loaders would still contend on that tuple. The projection is
	# rebuilt from scratch after the load anyway.
	log "disabling retained-history triggers for the load"
	"${PSQL[@]}" -c "ALTER TABLE request_logs DISABLE TRIGGER USER" \
		-c "ALTER TABLE usage_request_events DISABLE TRIGGER USER"

	log "creating history partitions"
	split_corpus
	"${PSQL[@]}" -f - < "$BENCH_WORK_DIR/corpus_partitions.sql"

	log "loading corpus with ${BENCH_JOBS} workers"
	local started
	started=$(date +%s)
	local pids=()
	for worker in $(seq 0 $((BENCH_JOBS - 1))); do
		( "${PSQL[@]}" -f - < "$BENCH_WORK_DIR/corpus_w${worker}.sql" > "$BENCH_WORK_DIR/worker_${worker}.log" 2>&1 ) &
		pids+=($!)
	done
	local failed=0
	for pid in "${pids[@]}"; do wait "$pid" || failed=1; done
	if [ "$failed" -ne 0 ]; then
		echo "corpus load failed; see $BENCH_WORK_DIR/worker_*.log" >&2
		exit 1
	fi
	echo "loaded in $(( $(date +%s) - started ))s"

	log "re-enabling triggers and rebuilding the coverage projection"
	"${PSQL[@]}" -c "ALTER TABLE request_logs ENABLE TRIGGER USER" \
		-c "ALTER TABLE usage_request_events ENABLE TRIGGER USER" \
		-c "UPDATE retention_coverage_read_models SET dirty = true, freshness = 'stale'"
	# Coverage is owner-computed at startup. Without this restart the API
	# clamps every window to the pre-load latest timestamp and measures
	# nothing.
	start_backend
	stop_backend

	log "vacuum analyze"
	"${PSQL[@]}" -c "VACUUM ANALYZE request_logs" -c "VACUUM ANALYZE usage_request_events" -c "CHECKPOINT"
	cmd_size
}

# Partition DDL runs once; the per-session preamble is replayed by every
# worker, and days are dealt round-robin so each worker touches a comparable
# spread of partitions.
split_corpus() {
	python3 - "$BENCH_WORK_DIR" "$BENCH_JOBS" <<'SPLIT'
import sys

work_dir, jobs = sys.argv[1], int(sys.argv[2])
with open(work_dir + "/corpus.sql", encoding="utf-8") as handle:
    text = handle.read()

sections = {"preamble": [], "partitions": [], "day": []}
current = None
for line in text.splitlines():
    if line.startswith("-- @@PRISM_BENCH_SECTION "):
        current = line.split(" ")[-1]
        if current == "day":
            sections["day"].append([])
        continue
    if current == "day":
        sections["day"][-1].append(line)
    elif current:
        sections[current].append(line)

preamble = "\n".join(sections["preamble"])
with open(work_dir + "/corpus_partitions.sql", "w", encoding="utf-8") as handle:
    handle.write("\n".join(sections["partitions"]) + "\n")

buckets = [[] for _ in range(jobs)]
for index, block in enumerate(sections["day"]):
    buckets[index % jobs].append("\n".join(block))
for worker, blocks in enumerate(buckets):
    with open(f"{work_dir}/corpus_w{worker}.sql", "w", encoding="utf-8") as handle:
        handle.write(preamble + "\n" + "\n".join(blocks) + "\n")
print(f"split {len(sections['day'])} day blocks across {jobs} workers")
SPLIT
}

cmd_size() {
	"${PSQL[@]}" -c "SELECT parent.relname AS table, count(*) AS partitions,
			pg_size_pretty(sum(pg_total_relation_size(child.oid))) AS total,
			pg_size_pretty(sum(pg_relation_size(child.oid))) AS heap,
			sum(child.reltuples)::bigint AS estimated_rows
		FROM pg_inherits
		JOIN pg_class parent ON parent.oid = inhparent
		JOIN pg_class child ON child.oid = inhrelid
		WHERE parent.relname IN ('request_logs','usage_request_events')
		GROUP BY 1 ORDER BY 1"
}

# Plans come from auto_explain over the real API surface rather than from a
# hand-copied SQL suite: a copy drifts away from the Go query builders the
# moment either changes, and a drifted plan is worse than no plan.
cmd_explain() {
	require docker
	require curl
	mkdir -p "$BENCH_WORK_DIR"
	log "enabling auto_explain"
	"${PSQL[@]}" \
		-c "LOAD 'auto_explain'" \
		-c "ALTER SYSTEM SET session_preload_libraries = 'auto_explain'" \
		-c "ALTER SYSTEM SET auto_explain.log_min_duration = 0" \
		-c "ALTER SYSTEM SET auto_explain.log_analyze = on" \
		-c "ALTER SYSTEM SET auto_explain.log_buffers = on" \
		-c "ALTER SYSTEM SET auto_explain.log_nested_statements = on" \
		-c "SELECT pg_reload_conf()" >/dev/null

	local since
	# docker reads a timestamp without a zone as daemon-local time, so stamp
	# the zone explicitly or the filter silently keeps older plans.
	since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	render_config
	start_backend
	trap 'stop_backend; disable_auto_explain' EXIT
	BENCH_WORK_DIR="$BENCH_WORK_DIR" BENCH_END_DATE="$BENCH_END_DATE" API_BASE="$API_BASE" \
		BENCH_RUNS=1 bash "$SCRIPT_DIR/api_suite.sh" >/dev/null
	stop_backend
	disable_auto_explain
	trap - EXIT

	docker logs --since "$since" "$CONTAINER" 2>&1 | grep -v '^\s*$' > "$BENCH_WORK_DIR/explain.out"
	echo "plans written to $BENCH_WORK_DIR/explain.out"
	grep -E 'duration: .* plan:' "$BENCH_WORK_DIR/explain.out" |
		sed -E 's/.*duration: ([0-9.]+) ms.*/\1/' |
		sort -gr | head -15 |
		awk '{ printf "  slowest statement %2d: %s ms\n", NR, $1 }'
}

cmd_api() {
	require curl
	require python3
	mkdir -p "$BENCH_WORK_DIR"
	render_config
	start_backend
	trap stop_backend EXIT
	BENCH_WORK_DIR="$BENCH_WORK_DIR" BENCH_END_DATE="$BENCH_END_DATE" API_BASE="$API_BASE" \
		bash "$SCRIPT_DIR/api_suite.sh"
	stop_backend
	trap - EXIT
}

cmd_down() {
	stop_backend
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	rm -rf "$BENCH_WORK_DIR"
	echo "benchmark torn down"
}

case "${1:-}" in
up) cmd_up ;;
load) cmd_load ;;
explain) cmd_explain ;;
api) cmd_api ;;
size) cmd_size ;;
all) cmd_up; cmd_load; cmd_explain; cmd_api ;;
down) cmd_down ;;
*) sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac
