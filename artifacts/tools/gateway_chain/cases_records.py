"""L4 — what the request record itself must contain.

These cases assert the durable observability contract: one row per attempt in
request_logs, one row per caller request in usage_request_events, the routing
evidence beside them, and the daily partitions that hold them.
"""

from __future__ import annotations

import time
from datetime import datetime, timezone

from .runner import Case, CaseContext
from .support import as_bool, as_int, live_model, marker, wait_for_rows


def _record_identity_fields(context: CaseContext) -> None:
    """The identifying columns a Requests page needs must all be populated."""
    model = live_model(context)
    before = context.store.max_id("request_logs")
    context.gateway.chat(model, marker("IDENT"), max_tokens=8)
    rows = wait_for_rows(context, "request_logs", before, expected=1)
    if not rows:
        context.expect("a request_log row was written", False, expected=">=1", actual=0)
        return
    row = rows[-1]
    context.record("row", {key: row.get(key) for key in (
        "operation_name", "api_family", "model_id", "resolved_target_model_id",
        "connection_id", "endpoint_id", "endpoint_description", "row_kind",
        "proxy_api_key_name_snapshot", "caller_user_agent", "request_path",
    )})
    for column, expected in (
        ("operation_name", "openai.chat_completions"),
        ("api_family", "openai"),
        ("model_id", model),
        ("row_kind", "upstream"),
        ("request_path", "/v1/chat/completions"),
    ):
        context.expect_eq(f"{column} is recorded", expected, row.get(column))
    for column in ("resolved_target_model_id", "connection_id", "endpoint_id", "endpoint_description", "ingress_request_id"):
        context.expect(
            f"{column} is populated",
            bool(row.get(column)),
            expected="non-empty",
            actual=row.get(column),
        )
    context.expect(
        "caller user agent is captured",
        bool(row.get("caller_user_agent")),
        expected="non-empty",
        actual=row.get("caller_user_agent"),
    )


def _usage_event_pairs_with_attempts(context: CaseContext) -> None:
    """Exactly one usage event per caller request, and its declared attempt
    count must equal the attempt rows actually written."""
    model = live_model(context)
    before_logs = context.store.max_id("request_logs")
    before_usage = context.store.max_id("usage_request_events")
    context.gateway.chat(model, marker("USAGE"), max_tokens=8)

    logs = wait_for_rows(context, "request_logs", before_logs, expected=1)
    events = wait_for_rows(context, "usage_request_events", before_usage, expected=1)
    context.expect_eq("one usage event was written", 1, len(events))
    if not events:
        return
    event = events[0]
    context.record("usage_event", {key: event.get(key) for key in (
        "model_id", "status_code", "success_flag", "attempt_count",
        "expected_request_log_row_count", "endpoint_label_snapshot",
        "proxy_api_key_name_snapshot", "routing_evidence_complete",
    )})
    context.expect_eq("attempt count matches the rows written", len(logs), as_int(event.get("attempt_count")))
    context.expect_eq(
        "expected row count matches the rows written",
        len(logs),
        as_int(event.get("expected_request_log_row_count")),
    )
    context.expect_eq("usage event carries the caller model", model, event.get("model_id"))
    context.expect(
        "endpoint_label_snapshot is retained on the usage event",
        bool(event.get("endpoint_label_snapshot")),
        expected="non-empty",
        actual=event.get("endpoint_label_snapshot"),
    )
    context.expect_eq("routing evidence is complete", True, as_bool(event.get("routing_evidence_complete")))
    context.expect(
        "attempt rows share the usage event's ingress request id",
        all(row.get("ingress_request_id") == event.get("ingress_request_id") for row in logs),
        expected=event.get("ingress_request_id"),
        actual=[row.get("ingress_request_id") for row in logs],
    )


def _loadbalance_events_track_transitions(context: CaseContext) -> None:
    """loadbalance_events records routing state transitions, not traffic.

    A clean single-attempt success must therefore leave none, while a request
    that has to retry another peer must leave a retry_scheduled event. Both
    halves are asserted, because "always writes" and "never writes" would each
    be wrong.
    """
    model = live_model(context)
    before_success = context.store.max_id("loadbalance_events")
    context.gateway.chat(model, marker("LBOK"), max_tokens=8)
    time.sleep(3.0)
    after_success = context.store.count("loadbalance_events", f"id > {before_success}")
    context.record("events_after_clean_success", after_success)
    context.expect_eq("a clean single-attempt success schedules no retry", 0, after_success)

    failing = context.state.get("failover_model")
    if not failing:
        context.note("no multi-target model available; the transition half of this case was not exercised")
        return
    before_failover = context.store.max_id("loadbalance_events")
    context.gateway.chat(failing, marker("LBRETRY"), max_tokens=8, timeout=180.0)
    rows = wait_for_rows(context, "loadbalance_events", before_failover, expected=1)
    kinds = sorted({row.get("event_type") for row in rows})
    context.record("events_after_failover", len(rows))
    context.record("event_types", kinds)
    context.expect("a failing peer produces a routing event", len(rows) >= 1, expected=">=1", actual=len(rows))
    context.expect(
        "the routing event is a scheduled retry",
        "retry_scheduled" in kinds,
        expected="retry_scheduled among the event types",
        actual=kinds,
    )
    for row in rows:
        context.expect(
            "the event names the connection it applies to",
            bool(row.get("connection_id")),
            expected="non-empty connection_id",
            actual=row.get("connection_id"),
        )


def _daily_partition_exists(context: CaseContext) -> None:
    """The runtime creates the day's partitions; a missing one would make the
    write fail rather than silently vanish, so assert the partition is there."""
    today = datetime.now(timezone.utc).strftime("%Y%m%d")
    expected = {
        f"request_logs_p{today}",
        f"usage_request_events_p{today}",
        f"audit_logs_p{today}",
        f"loadbalance_events_p{today}",
    }
    rows = context.store.rows(
        "select tablename from pg_tables where schemaname='public' and tablename like '%\\_p2%'",
        ["tablename"],
    )
    present = {row["tablename"] for row in rows}
    context.record("today", today)
    context.record("missing_partitions", sorted(expected - present))
    context.expect_eq("today's partitions all exist", set(), expected - present)


def _no_secret_material_in_records(context: CaseContext) -> None:
    """The non-audit record path must not persist credentials."""
    key = context.state.get("upstream_api_key")
    proxy_key = context.state.get("proxy_key")
    candidates = [value for value in (key, proxy_key) if value]
    if not candidates:
        context.block("no credential known to the suite, so leakage cannot be checked")
    for secret in candidates:
        literal = secret.replace("'", "''")
        hits = context.store.scalar(
            "select count(*) as value from request_logs where "
            f"coalesce(error_detail,'') like '%{literal}%' "
            f"or coalesce(endpoint_base_url,'') like '%{literal}%' "
            f"or coalesce(request_path,'') like '%{literal}%' "
            f"or coalesce(upstream_request_path,'') like '%{literal}%'"
        )
        context.expect_eq("no credential material in request_logs text columns", 0, as_int(hits))


CASES = [
    Case("L4-01", "records", "request_logs carries every identifying field", _record_identity_fields, requires_live_upstream=True),
    Case("L4-02", "records", "usage event and attempt rows agree", _usage_event_pairs_with_attempts, requires_live_upstream=True),
    Case("L4-03", "records", "loadbalance events track routing transitions, not traffic", _loadbalance_events_track_transitions, requires_live_upstream=True),
    Case("L4-04", "records", "today's log partitions exist", _daily_partition_exists),
    Case("L4-05", "records", "no credential material is persisted on the record path", _no_secret_material_in_records),
]
