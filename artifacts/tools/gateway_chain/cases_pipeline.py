"""L7 — health of the durable telemetry pipeline itself.

A wedged outbox is the worst observability failure this system can have: the
gateway keeps serving traffic and /health keeps reporting ready, while nothing
is recorded any more. These cases assert that the pipeline is draining, and
that a row it can never insert is quarantined instead of retried forever.

Added after a live run reproduced exactly that: one usage event carrying
proxy_api_key_attribution_state='identified' with a NULL proxy_api_key_id_snapshot
violated ck_usage_request_events_proxy_key_snapshot_consistent, was retried
several times per second indefinitely without its attempt counter advancing,
and head-of-line blocked 20 healthy rows behind it.
"""

from __future__ import annotations

import time

from .runner import Case, CaseContext
from .support import as_int, live_model, marker

DRAIN_TIMEOUT_S = 60.0

# Port 9 (discard) is reserved and closed on a normal host, so a connection
# there fails at the transport layer rather than returning an HTTP status. That
# is what forces the gateway-side total-failure path instead of a relayed 4xx.
UNROUTABLE_BASE_URL = "http://127.0.0.1:9"
PROBE_PREFIX = "chain-probe"


def _outbox_drains(context: CaseContext) -> None:
    model = live_model(context)
    context.gateway.chat(model, marker("DRAIN"), max_tokens=8)

    deadline = time.monotonic() + DRAIN_TIMEOUT_S
    depth = context.store.count("runtime_telemetry_outbox")
    while time.monotonic() < deadline and depth > 0:
        time.sleep(1.0)
        depth = context.store.count("runtime_telemetry_outbox")
    context.record("outbox_depth_after_drain", depth)
    context.expect_eq("the telemetry outbox drains to empty", 0, depth)


def _no_row_is_retried_without_accounting(context: CaseContext) -> None:
    """A row that keeps failing must at least count its attempts, or it will be
    retried forever and block everything behind it."""
    rows = context.store.json_rows(
        "select id, core_state, core_attempt_count, core_last_safe_error_code, created_at "
        "from runtime_telemetry_outbox where created_at < now() - interval '60 seconds' order by id"
    )
    context.record("stale_outbox_rows", len(rows))
    context.record(
        "stale_rows_detail",
        [{key: row.get(key) for key in ("id", "core_state", "core_attempt_count", "core_last_safe_error_code")} for row in rows[:5]],
    )
    context.expect_eq("no outbox row is older than a minute", 0, len(rows))
    unaccounted = [row for row in rows if as_int(row.get("core_attempt_count")) in (None, 0)]
    context.expect_eq(
        "no stale row has been retried without its attempt counter advancing",
        0,
        len(unaccounted),
    )


def _permanent_failures_are_quarantined(context: CaseContext) -> None:
    """Whatever cannot ever be inserted belongs in quarantine, where an operator
    can see it — not in an endless retry loop that is invisible outside the log."""
    stuck = context.store.json_rows(
        "select id, core_attempt_count from runtime_telemetry_outbox "
        "where created_at < now() - interval '120 seconds'"
    )
    quarantined = context.store.count("runtime_telemetry_quarantine")
    context.record("stuck_rows", len(stuck))
    context.record("quarantined_rows", quarantined)
    if not stuck:
        context.expect("no row is stuck, so nothing needs quarantining", True)
        return
    context.expect(
        "a row that cannot be materialized has been routed to quarantine",
        quarantined > 0,
        expected=">0 quarantined rows",
        actual=quarantined,
    )


def _recording_keeps_up_with_traffic(context: CaseContext) -> None:
    """The end-to-end promise: traffic served must equal traffic recorded."""
    model = live_model(context)
    before = context.store.max_id("request_logs")
    served = 0
    for index in range(3):
        response = context.gateway.chat(model, marker(f"KEEPUP{index}"), max_tokens=8)
        if response.status == 200:
            served += 1

    deadline = time.monotonic() + DRAIN_TIMEOUT_S
    recorded = 0
    while time.monotonic() < deadline:
        recorded = context.store.count("request_logs", f"id > {before}")
        if recorded >= served:
            break
        time.sleep(1.0)
    context.record("served", served)
    context.record("recorded", recorded)
    context.expect_eq("every served request was recorded", served, recorded)


def _provision_dead_upstream(context: CaseContext) -> dict:
    """Author a caller model whose only target cannot be reached at all.

    Returns the ids to tear down. Every name carries PROBE_PREFIX so an
    interrupted run leaves something obviously sweepable behind.
    """
    endpoint = context.gateway.management(
        "POST",
        "/endpoints",
        body={
            "name": f"{PROBE_PREFIX}-dead-endpoint",
            "base_url": UNROUTABLE_BASE_URL,
            "api_key": f"{PROBE_PREFIX}-not-a-real-key",
        },
    )
    if endpoint.status != 201:
        raise RuntimeError(f"could not create the probe endpoint: HTTP {endpoint.status} {endpoint.text[:200]}")
    endpoint_id = (endpoint.json() or {}).get("id")

    strategies = context.gateway.management("GET", "/loadbalance/strategies")
    listed = strategies.json() or []
    default_strategy = next(
        (item.get("id") for item in listed if item.get("is_default")),
        listed[0].get("id") if listed else None,
    )

    model = context.gateway.management(
        "POST",
        "/models",
        body={
            "api_family": "openai",
            "model_id": f"{PROBE_PREFIX}-dead-model",
            "display_name": "gateway-chain probe (unreachable upstream)",
            "openai_accepted_format": "chat_completions_only",
            "loadbalance_strategy_id": default_strategy,
            "initial_terminal_target": {
                "endpoint_id": endpoint_id,
                "name": f"{PROBE_PREFIX}-dead-target",
                "is_active": True,
                "openai_text_capability": "chat_completions_only",
            },
        },
    )
    if model.status not in (200, 201):
        context.gateway.management("DELETE", f"/endpoints/{endpoint_id}")
        raise RuntimeError(f"could not create the probe model: HTTP {model.status} {model.text[:200]}")
    return {"endpoint_id": endpoint_id, "model_id": f"{PROBE_PREFIX}-dead-model"}


def _teardown_dead_upstream(context: CaseContext, provisioned: dict) -> None:
    listing = context.gateway.management("GET", "/models")
    for item in listing.json() or []:
        if item.get("model_id") == provisioned.get("model_id"):
            context.gateway.management("DELETE", f"/models/{item['id']}")
    if provisioned.get("endpoint_id"):
        context.gateway.management("DELETE", f"/endpoints/{provisioned['endpoint_id']}")


def _unreachable_upstream_does_not_wedge_the_pipeline(context: CaseContext) -> None:
    """The reproducer for the incident this whole group exists for.

    A keyed request whose every target is unreachable takes the gateway-side
    total-failure path. That path must still produce a usage event the database
    will accept, and the outbox must drain afterwards. If it does not, this
    instance has stopped recording every request that follows.
    """
    provisioned = _provision_dead_upstream(context)
    before_outbox = context.store.max_id("runtime_telemetry_outbox")
    try:
        before_logs = context.store.max_id("request_logs")
        before_usage = context.store.max_id("usage_request_events")
        response = context.gateway.chat(
            provisioned["model_id"], marker("DEADUPSTREAM"), max_tokens=4, timeout=120.0
        )
        context.record("status", response.status)
        context.record("body", response.text[:240])
        context.expect_eq("an unreachable upstream fails the request 502", 502, response.status)
        context.expect(
            "the failure is reported as a transport failure, not an upstream status",
            "connect" in response.text.lower() or "transport" in response.text.lower(),
            expected="a transport-level failure reason",
            actual=response.text[:160],
        )

        deadline = time.monotonic() + DRAIN_TIMEOUT_S
        depth = context.store.count("runtime_telemetry_outbox")
        while time.monotonic() < deadline and depth > 0:
            time.sleep(1.0)
            depth = context.store.count("runtime_telemetry_outbox")
        context.record("outbox_depth", depth)
        context.expect_eq("the outbox drains after a total upstream failure", 0, depth)

        logs = context.store.count("request_logs", f"id > {before_logs}")
        usage = context.store.json_rows(
            f"select * from usage_request_events where id > {before_usage} order by id"
        )
        context.record("attempt_rows", logs)
        context.record("usage_rows", len(usage))
        context.expect("the failed attempt is recorded", logs >= 1, expected=">=1", actual=logs)
        context.expect_eq("a usage event is recorded for the caller request", 1, len(usage))
        if usage:
            event = usage[0]
            context.record(
                "attribution",
                {
                    key: event.get(key)
                    for key in (
                        "proxy_api_key_attribution_state",
                        "proxy_api_key_id_snapshot",
                        "proxy_api_key_name_snapshot",
                        "status_code",
                    )
                },
            )
            state = event.get("proxy_api_key_attribution_state")
            has_id = event.get("proxy_api_key_id_snapshot") is not None
            has_name = event.get("proxy_api_key_name_snapshot") is not None
            context.expect(
                "the proxy-key attribution triple is internally consistent",
                (state == "identified" and has_id and has_name)
                or (state == "none" and not has_id and not has_name)
                or (state == "unknown" and not has_id),
                expected="a legal (state, id, name) combination",
                actual=(state, has_id, has_name),
            )
            context.expect_eq(
                "a keyed request is attributed to its key even when the upstream is unreachable",
                "identified",
                state,
            )
    finally:
        _teardown_dead_upstream(context, provisioned)
        # If this case just wedged the pipeline, unwedge it. Leaving the row in
        # place would block every later case and bury this finding under
        # unrelated failures — but the cleanup is recorded, never silent.
        stranded = context.store.count("runtime_telemetry_outbox", f"id > {before_outbox}")
        if stranded:
            discarded = context.store.discard_outbox_rows_after(before_outbox)
            context.record("stranded_outbox_rows_discarded", discarded)
            context.note(
                f"discarded {discarded} outbox row(s) this case wedged, so the rest of the run stays "
                "meaningful; on a healthy build there is nothing to discard"
            )


CASES = [
    Case("L7-05", "pipeline", "an unreachable upstream does not wedge the telemetry pipeline", _unreachable_upstream_does_not_wedge_the_pipeline, requires_live_upstream=False),
    Case("L7-01", "pipeline", "the telemetry outbox drains after traffic", _outbox_drains, requires_live_upstream=True),
    Case("L7-02", "pipeline", "no outbox row is retried without attempt accounting", _no_row_is_retried_without_accounting),
    Case("L7-03", "pipeline", "a permanently un-insertable row is quarantined", _permanent_failures_are_quarantined),
    Case("L7-04", "pipeline", "recording keeps up with served traffic", _recording_keeps_up_with_traffic, requires_live_upstream=True),
]
