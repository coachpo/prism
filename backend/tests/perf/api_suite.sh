#!/usr/bin/env bash
# End-to-end ingress-chain API timings. Driven by bench.sh, which owns the
# backend process; API_BASE, BENCH_WORK_DIR and BENCH_END_DATE come from there.
set -euo pipefail

API_BASE="${API_BASE:-http://127.0.0.1:18000}"
BENCH_WORK_DIR="${BENCH_WORK_DIR:-/tmp/prism-chain-bench}"
BENCH_END_DATE="${BENCH_END_DATE:-$(date -u +%F)}"
LAST="$BENCH_WORK_DIR/last.json"

DAY_START="${BENCH_END_DATE}T00:00:00Z"
DAY_END="$(date -u -d "$BENCH_END_DATE + 1 day" +%F)T00:00:00Z"
WEEK_START="$(date -u -d "$BENCH_END_DATE - 6 days" +%F)T00:00:00Z"
MONTH_START="$(date -u -d "$BENCH_END_DATE - 29 days" +%F)T00:00:00Z"

# Every case reports first (cold plan cache) and median separately: the JIT and
# planning costs this benchmark hunts show up in the spread, not the mean.
measure() {
	local label="$1" query="$2" runs="${BENCH_RUNS:-${3:-5}}"
	local index
	for index in $(seq 1 "$runs"); do
		curl -s -o "$LAST" -w "%{time_total}\n" "$API_BASE/api/stats/requests?$query"
	done | LABEL="$label" LAST="$LAST" python3 -c '
import json, os, sys

raw = [float(value) * 1000 for value in sys.stdin.read().split()]
samples = sorted(raw)
document = json.load(open(os.environ["LAST"]))
print("%-38s first=%7.0fms median=%7.0fms max=%7.0fms items=%-4s ingresses=%s" % (
    os.environ["LABEL"], raw[0], samples[len(samples) // 2], samples[-1],
    len(document.get("items", [])), document.get("retained_ingress_total")))
'
}

echo "== API timings against $API_BASE =="
measure "24h window" "view=ingress_chains&time_range=custom&from_time=$DAY_START&to_time=$DAY_END&chain_limit=20&sort_order=desc" 6
measure "7d window" "view=ingress_chains&time_range=custom&from_time=$WEEK_START&to_time=$DAY_END&chain_limit=20&sort_order=desc" 6
measure "30d window" "view=ingress_chains&time_range=custom&from_time=$MONTH_START&to_time=$DAY_END&chain_limit=20&sort_order=desc" 6
measure "7d + 5xx row filter" "view=ingress_chains&time_range=custom&from_time=$WEEK_START&to_time=$DAY_END&chain_limit=20&sort_order=desc&status_family=5xx" 4
measure "7d + 2xx row filter" "view=ingress_chains&time_range=custom&from_time=$WEEK_START&to_time=$DAY_END&chain_limit=20&sort_order=desc&status_family=2xx" 4
measure "7d + final_result filter" "view=ingress_chains&time_range=custom&from_time=$WEEK_START&to_time=$DAY_END&chain_limit=20&sort_order=desc&ingress_final_result=completed" 4
measure "attempts view 7d limit=100" "view=attempts&time_range=custom&from_time=$WEEK_START&to_time=$DAY_END&limit=100&offset=0&sort_by=created_at&sort_order=desc" 4

# Deep pagination: a per-page recompute is invisible on page 1 and obvious here.
BASE_QUERY="view=ingress_chains&time_range=custom&from_time=$MONTH_START&to_time=$DAY_END&chain_limit=20&sort_order=desc"
cursor=$(curl -s "$API_BASE/api/stats/requests?$BASE_QUERY" |
	python3 -c 'import json,sys; print(json.load(sys.stdin).get("next_chain_cursor") or "")')
for page in 2 3 4 5; do
	[ -n "$cursor" ] || break
	encoded=$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1], safe=""))' "$cursor")
	elapsed=$(curl -s -o "$LAST" -w "%{time_total}" "$API_BASE/api/stats/requests?$BASE_QUERY&chain_cursor=$encoded")
	cursor=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("next_chain_cursor") or "")' "$LAST")
	printf '%-38s %7.0fms\n' "30d page $page" "$(python3 -c "print(float('$elapsed') * 1000)")"
done

ingress=$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(d["items"][0]["ingress_request_id"] if d.get("items") else "")' "$LAST")
if [ -n "$ingress" ]; then
	measure "exact ingress (no window)" "view=ingress_chains&ingress_request_id=$ingress&chain_limit=20&sort_order=desc" 4
fi
echo "== done =="
