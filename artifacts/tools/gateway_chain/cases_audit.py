"""L5 — the request-audit switch: does it change forwarding, and what is recorded.

Three policies exist per api family: disabled, metadata_only, body_capture.
Each case sets the policy explicitly, sends a real request, and asserts both
halves of the question the operator actually cares about — the caller's result
must be unchanged, and the record must match the policy exactly.
"""

from __future__ import annotations

from .runner import Case, CaseContext
from .support import (
    as_bool,
    as_int,
    audit_mode,
    live_model,
    marker,
    set_audit_mode,
    wait_for_rows,
)


def _content_of(payload: dict | None) -> str:
    text = ""
    for choice in (payload or {}).get("choices") or []:
        text += (choice.get("message") or {}).get("content") or ""
    return text


def _disabled_writes_nothing(context: CaseContext) -> None:
    model = live_model(context)
    set_audit_mode(context, openai_mode="disabled")
    context.expect_eq("policy reads back as disabled", "disabled", audit_mode(context))

    before_audit = context.store.max_id("audit_logs")
    before_logs = context.store.max_id("request_logs")
    token = marker("AUDITOFF")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
    context.record("status", response.status)
    context.expect_eq("forwarding still succeeds with audit off", 200, response.status)
    context.expect(
        "the model still answered",
        token in _content_of(response.json()),
        expected=f"content contains {token}",
        actual=_content_of(response.json())[:120],
    )

    logs = wait_for_rows(context, "request_logs", before_logs, expected=1)
    context.expect("the request is still recorded in request_logs", len(logs) >= 1, expected=">=1", actual=len(logs))
    if logs:
        context.expect_eq(
            "request_logs snapshots audit as disabled at request time",
            False,
            as_bool(logs[-1].get("audit_enabled_at_request")),
        )
    audit_rows = context.store.count("audit_logs", f"id > {before_audit}")
    context.record("audit_rows_written", audit_rows)
    context.expect_eq("no audit row is written at all when audit is disabled", 0, audit_rows)


def _metadata_only_captures_no_bodies(context: CaseContext) -> None:
    model = live_model(context)
    set_audit_mode(context, openai_mode="metadata_only")
    context.expect_eq("policy reads back as metadata_only", "metadata_only", audit_mode(context))

    before_audit = context.store.max_id("audit_logs")
    token = marker("AUDITMETA")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
    context.expect_eq("forwarding still succeeds under metadata_only", 200, response.status)

    rows = wait_for_rows(context, "audit_logs", before_audit, expected=1)
    context.record("audit_rows", len(rows))
    context.expect_eq("exactly one audit row is written", 1, len(rows))
    if not rows:
        return
    row = rows[0]
    context.record("capture_state", {key: row.get(key) for key in (
        "audit_enabled_at_request", "audit_capture_bodies_at_request",
        "request_body_capture_status", "response_body_capture_status",
        "request_headers_capture_status", "response_headers_capture_status",
    )})
    context.expect_eq("audit is snapshotted as enabled", True, as_bool(row.get("audit_enabled_at_request")))
    context.expect_eq(
        "body capture is snapshotted as off",
        False,
        as_bool(row.get("audit_capture_bodies_at_request")),
    )
    context.expect(
        "request headers are captured",
        bool(row.get("request_headers")),
        expected="non-empty",
        actual=row.get("request_headers"),
    )
    context.expect(
        "the request body is NOT stored",
        not row.get("request_body"),
        expected="empty request_body",
        actual=str(row.get("request_body"))[:80],
    )
    context.expect(
        "the response body is NOT stored",
        not row.get("response_body"),
        expected="empty response_body",
        actual=str(row.get("response_body"))[:80],
    )
    context.expect(
        "the prompt marker does not leak into a metadata-only row",
        token not in str(row.get("request_body") or ""),
        expected=f"{token} absent",
        actual="present" if token in str(row.get("request_body") or "") else "absent",
    )


def _body_capture_stores_both_bodies(context: CaseContext) -> None:
    model = live_model(context)
    set_audit_mode(context, openai_mode="body_capture")
    context.expect_eq("policy reads back as body_capture", "body_capture", audit_mode(context))

    before_audit = context.store.max_id("audit_logs")
    token = marker("AUDITBODY")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
    context.expect_eq("forwarding still succeeds under body_capture", 200, response.status)

    rows = wait_for_rows(context, "audit_logs", before_audit, expected=1)
    context.expect_eq("exactly one audit row is written", 1, len(rows))
    if not rows:
        return
    row = rows[0]
    request_body = _decode_bytea(row.get("request_body"))
    response_body = _decode_bytea(row.get("response_body"))
    context.record("request_body_bytes", len(request_body))
    context.record("response_body_bytes", len(response_body))
    context.record("capture_state", {key: row.get(key) for key in (
        "request_body_capture_status", "request_body_capture_end_state", "request_body_truncated",
        "response_body_capture_status", "response_body_capture_end_state", "response_body_truncated",
        "request_body_encoding", "response_body_encoding",
    )})

    context.expect_eq("body capture is snapshotted as on", True, as_bool(row.get("audit_capture_bodies_at_request")))
    context.expect_eq("request body capture status is captured", "captured", row.get("request_body_capture_status"))
    context.expect_eq("response body capture status is captured", "captured", row.get("response_body_capture_status"))
    context.expect_eq("request body capture completed", "complete", row.get("request_body_capture_end_state"))
    context.expect_eq("response body capture completed", "complete", row.get("response_body_capture_end_state"))
    context.expect(
        "the captured request body contains the prompt marker",
        token in request_body,
        expected=f"request body contains {token}",
        actual=request_body[:160],
    )
    context.expect(
        "the captured response body contains the model's answer",
        token in response_body,
        expected=f"response body contains {token}",
        actual=response_body[-160:],
    )
    headers = str(row.get("request_headers") or "")
    context.expect(
        "the Authorization header is redacted in the audit record",
        "[REDACTED]" in headers,
        expected="authorization value replaced with [REDACTED]",
        actual=headers[:200],
    )
    proxy_key = context.state.get("proxy_key") or ""
    if proxy_key:
        context.expect(
            "the caller's proxy key never appears in the audit record",
            proxy_key not in headers and proxy_key not in request_body,
            expected="proxy key absent",
            actual="present" if (proxy_key in headers or proxy_key in request_body) else "absent",
        )
    upstream_key = context.state.get("upstream_api_key") or ""
    if upstream_key:
        context.expect(
            "the upstream credential never appears in the audit record",
            upstream_key not in headers and upstream_key not in request_body,
            expected="upstream key absent",
            actual="present" if (upstream_key in headers or upstream_key in request_body) else "absent",
        )


def _body_capture_of_a_stream(context: CaseContext) -> None:
    """A streamed response has no single body; the audit must accumulate the
    whole event stream and say plainly whether it got all of it."""
    model = live_model(context)
    set_audit_mode(context, openai_mode="body_capture")
    before_audit = context.store.max_id("audit_logs")
    token = marker("AUDITSTREAM")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32, streaming=True)
    context.expect_eq("streamed forwarding still succeeds under body_capture", 200, response.status)

    rows = wait_for_rows(context, "audit_logs", before_audit, expected=1)
    context.expect_eq("exactly one audit row is written for the stream", 1, len(rows))
    if not rows:
        return
    row = rows[0]
    response_body = _decode_bytea(row.get("response_body"))
    context.record("captured_stream_bytes", len(response_body))
    context.record("capture_end_state", row.get("response_body_capture_end_state"))
    context.expect_eq("the row is marked as a stream", True, as_bool(row.get("is_stream")))
    context.expect_eq("stream capture completed", "complete", row.get("response_body_capture_end_state"))
    context.expect_eq("stream capture status is captured", "captured", row.get("response_body_capture_status"))
    context.expect(
        "the captured stream holds the terminal [DONE] frame",
        "[DONE]" in response_body,
        expected="[DONE] present",
        actual=response_body[-120:],
    )
    context.expect(
        "the captured stream holds more than one SSE frame",
        response_body.count("data:") >= 2,
        expected=">=2 data: frames",
        actual=response_body.count("data:"),
    )
    context.expect_eq(
        "truncation is reported as false when the whole stream was captured",
        False,
        as_bool(row.get("response_body_truncated")),
    )


def _toggle_takes_effect_immediately(context: CaseContext) -> None:
    """A policy change must reach the runtime path without a restart. The proof
    is the per-request snapshot on the next request, not the settings read."""
    model = live_model(context)

    set_audit_mode(context, openai_mode="disabled")
    before_off = context.store.max_id("request_logs")
    context.gateway.chat(model, marker("TOGGLEOFF"), max_tokens=8)
    off_rows = wait_for_rows(context, "request_logs", before_off, expected=1)
    off_snapshot = as_bool(off_rows[-1].get("audit_enabled_at_request")) if off_rows else None

    set_audit_mode(context, openai_mode="body_capture")
    before_on = context.store.max_id("request_logs")
    context.gateway.chat(model, marker("TOGGLEON"), max_tokens=8)
    on_rows = wait_for_rows(context, "request_logs", before_on, expected=1)
    on_snapshot = as_bool(on_rows[-1].get("audit_enabled_at_request")) if on_rows else None

    context.record("snapshot_after_disable", off_snapshot)
    context.record("snapshot_after_enable", on_snapshot)
    context.expect_eq("the request right after disabling records audit off", False, off_snapshot)
    context.expect_eq("the request right after enabling records audit on", True, on_snapshot)
    context.expect(
        "the change was live on the very next request, with no restart",
        off_snapshot is False and on_snapshot is True,
        expected="False then True",
        actual=f"{off_snapshot} then {on_snapshot}",
    )


def _switch_does_not_change_forwarding(context: CaseContext) -> None:
    """The operator's core question: does auditing alter what the caller gets?"""
    model = live_model(context)
    token = marker("PARITY")
    observed = {}
    for mode in ("disabled", "metadata_only", "body_capture"):
        set_audit_mode(context, openai_mode=mode)
        response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
        observed[mode] = {
            "status": response.status,
            "content_has_marker": token in _content_of(response.json()),
            "content_type": response.header("content-type"),
        }
    context.record("per_mode", observed)
    statuses = {mode: data["status"] for mode, data in observed.items()}
    context.expect_eq("every mode returns 200", {mode: 200 for mode in observed}, statuses)
    context.expect(
        "the model's answer is unaffected by the audit policy",
        all(data["content_has_marker"] for data in observed.values()),
        expected="marker present in all three modes",
        actual={mode: data["content_has_marker"] for mode, data in observed.items()},
    )
    context.expect(
        "the response content type is unaffected by the audit policy",
        len({data["content_type"] for data in observed.values()}) == 1,
        expected="one distinct content-type",
        actual={mode: data["content_type"] for mode, data in observed.items()},
    )


def _failed_request_is_audited(context: CaseContext) -> None:
    """Auditing must not be a success-only path."""
    model = context.state.get("dead_model")
    if not model:
        context.block("no model with a rejecting upstream was discovered")
    set_audit_mode(context, openai_mode="body_capture")
    before_audit = context.store.max_id("audit_logs")
    response = context.gateway.chat(model, marker("AUDITFAIL"), max_tokens=8, timeout=90.0)
    context.record("status", response.status)

    rows = wait_for_rows(context, "audit_logs", before_audit, expected=1)
    context.record("audit_rows", len(rows))
    context.expect("the failed attempt is audited too", len(rows) >= 1, expected=">=1", actual=len(rows))
    for row in rows:
        context.expect(
            "the audit row records the upstream's failing status",
            as_int(row.get("upstream_status_code")) not in (None, 200),
            expected="a non-200 upstream status",
            actual=row.get("upstream_status_code"),
        )
    # The response body is attached to the final attempt only, so asserting it
    # on every row would fail for the earlier peers by design.
    if rows:
        final = rows[-1]
        body = _decode_bytea(final.get("response_body"))
        context.record("final_attempt_response_bytes", len(body))
        context.expect(
            "the upstream's error body is captured on the final attempt",
            len(body) > 0,
            expected="non-empty response body",
            actual=len(body),
        )


def _decode_bytea(value: object) -> str:
    """psql renders bytea as \\x<hex>; json_agg gives the same text."""
    if value is None:
        return ""
    text = str(value)
    if text.startswith("\\x"):
        try:
            return bytes.fromhex(text[2:]).decode("utf-8", errors="replace")
        except ValueError:
            return ""
    return text


CASES = [
    Case("L5-01", "audit", "audit disabled writes no audit row and forwards normally", _disabled_writes_nothing, requires_live_upstream=True),
    Case("L5-02", "audit", "metadata_only records the request without its bodies", _metadata_only_captures_no_bodies, requires_live_upstream=True),
    Case("L5-03", "audit", "body_capture stores both bodies with credentials redacted", _body_capture_stores_both_bodies, requires_live_upstream=True),
    Case("L5-04", "audit", "body_capture accumulates a whole SSE stream", _body_capture_of_a_stream, requires_live_upstream=True),
    Case("L5-05", "audit", "toggling audit is live on the next request", _toggle_takes_effect_immediately, requires_live_upstream=True),
    Case("L5-06", "audit", "the audit policy does not change what the caller receives", _switch_does_not_change_forwarding, requires_live_upstream=True),
    Case("L5-07", "audit", "a failing upstream request is audited as well", _failed_request_is_audited),
]
