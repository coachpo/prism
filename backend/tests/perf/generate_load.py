#!/usr/bin/env python3
"""Render the ingress-chain benchmark corpus as SQL.

The corpus is synthesized from checked-in template rows rather than written
column by column: `json_populate_record(template, overrides)` inherits every
column the schema currently has, so a new NOT NULL column or CHECK constraint
does not silently produce an unloadable generator. Only the per-row identity
(id, ingress_request_id, timestamps, attempt shape) is overridden.

Usage: generate_load.py --days 30 --per-day 35000 --end-date 2026-09-06
"""

import argparse
import datetime
import json
import os
import sys

TEMPLATES_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "templates.jsonl")

SECTION_MARKER = "-- @@PRISM_BENCH_SECTION "

# Chain mix, matching the distribution measured on a live instance: almost
# every ingress is a single retained row, a small tail retries across targets,
# and a small tail fails. A flatter mix would understate the per-ingress work.
SINGLE_ROW_CHAIN_MODULUS = 83  # every Nth ingress is a three-row failover chain
FAILED_CHAIN_MODULUS = 67  # every Nth ingress is a 5xx single-row chain


def load_templates():
    templates = {}
    with open(TEMPLATES_PATH, encoding="utf-8") as handle:
        for line in handle:
            line = line.rstrip("\n")
            if not line:
                continue
            kind, document = line.split(" ", 1)
            templates[kind] = json.loads(document)
    return templates


def sql_literal(document):
    return "$j$" + json.dumps(document, ensure_ascii=False) + "$j$"


def render(days, per_day, end_date):
    templates = load_templates()
    chain_rows = templates["chain3"]
    start = end_date - datetime.timedelta(days=days - 1)
    # Section markers let the loader replay the per-session preamble in every
    # worker while the partition DDL runs exactly once: concurrent
    # `CREATE TABLE IF NOT EXISTS ... PARTITION OF` races on the parent.
    out = [SECTION_MARKER + "preamble", "SET client_min_messages = warning;"]

    out.append("CREATE TEMP TABLE tpl(kind text PRIMARY KEY, doc json);")
    for kind in ("rl_ok", "rl_5xx", "ue_ok", "ue_fail"):
        out.append(f"INSERT INTO tpl VALUES ('{kind}', {sql_literal(templates[kind])});")
    for index, row in enumerate(chain_rows):
        out.append(f"INSERT INTO tpl VALUES ('rl_c{index}', {sql_literal(row)});")

    # Typed template records: one json_populate_record per template, not per
    # generated row.
    out.append(
        "CREATE TEMP TABLE tpl_rl AS SELECT kind, json_populate_record(NULL::request_logs, doc) AS rec "
        "FROM tpl WHERE kind LIKE 'rl_%';"
    )
    out.append(
        "CREATE TEMP TABLE tpl_ue AS SELECT kind, json_populate_record(NULL::usage_request_events, doc) AS rec "
        "FROM tpl WHERE kind LIKE 'ue_%';"
    )

    # History partitions. The running backend only creates today and the
    # forward horizon, so the benchmark owns the backfilled days.
    out.append(SECTION_MARKER + "partitions")
    for offset in range(days + 1):
        day = start + datetime.timedelta(days=offset)
        following = day + datetime.timedelta(days=1)
        for table in ("request_logs", "usage_request_events"):
            out.append(
                f"CREATE TABLE IF NOT EXISTS public.{table}_p{day:%Y%m%d} PARTITION OF public.{table} "
                f"FOR VALUES FROM ('{day} 00:00:00+00') TO ('{following} 00:00:00+00') "
                "WITH (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 10000, "
                "toast.autovacuum_vacuum_scale_factor = 0.02, toast.autovacuum_vacuum_threshold = 10000);"
            )

    for offset in range(days):
        day = start + datetime.timedelta(days=offset)
        # Each day owns a disjoint id block so parallel loaders never collide.
        id_base = (offset + 1) * 10_000_000
        out.append(SECTION_MARKER + "day")
        out.append(
            f"""BEGIN;
CREATE TEMP TABLE g ON COMMIT DROP AS
SELECT n, {id_base} + n * 4 AS id0, gen_random_uuid()::text AS iid,
       '{day} 00:00:00+00'::timestamptz + (random() * 86399) * interval '1 second' AS ts,
       CASE WHEN n % {SINGLE_ROW_CHAIN_MODULUS} = 0 THEN 'c3'
            WHEN n % {FAILED_CHAIN_MODULUS} = 0 THEN '5xx'
            ELSE 'ok' END AS kind
FROM generate_series(1, {per_day}) n;
-- Single-row chains.
INSERT INTO request_logs
SELECT (json_populate_record(t.rec, json_build_object('id', g.id0, 'ingress_request_id', g.iid, 'created_at', g.ts, 'request_started_at', g.ts))).*
FROM g JOIN tpl_rl t ON t.kind = CASE g.kind WHEN 'ok' THEN 'rl_ok' WHEN '5xx' THEN 'rl_5xx' END
WHERE g.kind <> 'c3';
-- Three-row failover chains (429 -> 503 -> 200).
INSERT INTO request_logs
SELECT (json_populate_record(t.rec, json_build_object('id', g.id0 + k.i, 'ingress_request_id', g.iid, 'created_at', g.ts + k.off, 'request_started_at', g.ts + k.off))).*
FROM g JOIN (VALUES (0, interval '0'), (1, interval '0.3 second'), (2, interval '9 second')) k(i, off) ON TRUE
JOIN tpl_rl t ON t.kind = 'rl_c' || k.i
WHERE g.kind = 'c3';
-- One finalized usage event per ingress.
INSERT INTO usage_request_events
SELECT (json_populate_record(t.rec, json_build_object('id', g.id0, 'ingress_request_id', g.iid, 'created_at', g.ts,
        'ingress_started_at', g.ts, 'ingress_completed_at', g.ts + CASE WHEN g.kind = 'c3' THEN interval '11 second' ELSE interval '2 second' END,
        'attempt_count', CASE WHEN g.kind = 'c3' THEN 3 ELSE 1 END,
        'final_attempt_number', CASE WHEN g.kind = 'c3' THEN 3 ELSE 1 END,
        'expected_request_log_row_count', CASE WHEN g.kind = 'c3' THEN 3 ELSE 1 END))).*
FROM g JOIN tpl_ue t ON t.kind = CASE g.kind WHEN '5xx' THEN 'ue_fail' ELSE 'ue_ok' END;
COMMIT;"""
        )
    return "\n".join(out) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--days", type=int, default=30)
    parser.add_argument("--per-day", type=int, default=35000)
    parser.add_argument("--end-date", required=True, help="last generated day, YYYY-MM-DD")
    parser.add_argument("--out", default="-", help="output SQL path, or - for stdout")
    arguments = parser.parse_args()

    end_date = datetime.date.fromisoformat(arguments.end_date)
    sql = render(arguments.days, arguments.per_day, end_date)
    if arguments.out == "-":
        sys.stdout.write(sql)
    else:
        with open(arguments.out, "w", encoding="utf-8") as handle:
            handle.write(sql)
    start = end_date - datetime.timedelta(days=arguments.days - 1)
    print(
        f"generated {arguments.days} days x {arguments.per_day} ingresses ({start} .. {end_date})",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
