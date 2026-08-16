"""L1 — runtime ingress admission: what is rejected, and with what side effects.

The runtime contract is operation-registered: an unregistered path or a wrong
method must reject before provider transport, telemetry, audit, or any durable
side effect. These cases assert both halves — the response AND the absence of
rows.
"""

from __future__ import annotations

from .runner import Case, CaseContext
from .support import assert_no_new_rows, as_bool, live_model, marker, wait_for_rows


def _unregistered_path(context: CaseContext) -> None:
    watermarks = context.store.watermarks()
    response = context.gateway.runtime_raw("POST", "/v1/definitely-not-an-operation", body={"model": "x"})
    context.record("status", response.status)
    context.record("body", response.text[:300])
    context.expect_eq("unregistered runtime path is rejected 404", 404, response.status)
    context.expect(
        "rejection body names the runtime operation registry",
        "operation" in response.text.lower(),
        expected="mentions 'operation'",
        actual=response.text[:120],
    )
    assert_no_new_rows(context, watermarks)


def _wrong_method(context: CaseContext) -> None:
    watermarks = context.store.watermarks()
    response = context.gateway.runtime_raw("GET", "/v1/chat/completions")
    context.record("status", response.status)
    context.record("body", response.text[:300])
    context.expect_eq("registered path with the wrong method is rejected 405", 405, response.status)
    assert_no_new_rows(context, watermarks)


def _unknown_model(context: CaseContext) -> None:
    """An unknown model is rejected during planning, before any upstream call."""
    watermarks = context.store.watermarks()
    response = context.gateway.chat("prism-chain-no-such-model", "ping", max_tokens=8, timeout=60.0)
    context.record("status", response.status)
    context.record("body", response.text[:300])
    context.expect_eq("unknown model is rejected 404", 404, response.status)
    context.expect(
        "rejection names the model",
        "prism-chain-no-such-model" in response.text,
        expected="body contains the requested model id",
        actual=response.text[:160],
    )
    assert_no_new_rows(context, watermarks)


def _proxy_key_enforcement(context: CaseContext) -> None:
    """Proxy-key enforcement follows the instance auth setting, and whatever it
    does must be recorded truthfully rather than silently."""
    model = live_model(context)
    auth_enabled = as_bool(context.store.scalar("select auth_enabled as value from app_auth_settings"))
    context.record("app_auth_enabled", auth_enabled)

    before = context.store.max_id("request_logs")
    missing = context.gateway.chat(model, marker("NOKEY"), max_tokens=8, authorize=False)
    context.record("missing_key_status", missing.status)

    if auth_enabled:
        context.expect_eq("request without a proxy key is rejected 401", 401, missing.status)
        invalid = context.gateway.chat(
            model, marker("BADKEY"), max_tokens=8, override_key="pm-00000000000000000000000000"
        )
        context.record("invalid_key_status", invalid.status)
        context.expect_eq("request with an invalid proxy key is rejected 401", 401, invalid.status)
        return

    # Auth disabled is the synced production posture. Assert the honest
    # consequence instead of pretending the request was rejected.
    context.note(
        "app_auth_settings.auth_enabled is false, so runtime proxy-key auth is NOT enforced; "
        "unauthenticated callers reach real upstreams"
    )
    context.expect(
        "with auth disabled the unauthenticated request is forwarded, not rejected",
        missing.status not in (401, 403),
        expected="not 401/403",
        actual=missing.status,
    )
    rows = wait_for_rows(context, "request_logs", before, expected=1)
    context.record("rows_written", len(rows))
    context.expect("the unauthenticated attempt is still logged", len(rows) >= 1, expected=">=1", actual=len(rows))
    if rows:
        row = rows[-1]
        context.expect_eq(
            "attribution records that no proxy key was identified",
            "none",
            row.get("proxy_api_key_attribution_state"),
        )
        context.expect_eq(
            "the log records that auth was not enforced for this request",
            False,
            as_bool(row.get("proxy_api_key_auth_enforced_at_request")),
        )


def _ingress_correlation_header(context: CaseContext) -> None:
    """The response carries a correlation header; record whether it can actually
    be used to find the request's own row."""
    model = live_model(context)
    before = context.store.max_id("request_logs")
    response = context.gateway.chat(model, marker("CORR"), max_tokens=8)
    header = response.header("x-prism-ingress-request-id")
    context.record("x_prism_ingress_request_id", header)
    context.expect("runtime response carries the ingress correlation header", bool(header), expected="non-empty", actual=header)

    rows = wait_for_rows(context, "request_logs", before, expected=1)
    if not rows:
        context.expect("a request_log row was written for the call", False, expected=">=1 row", actual=0)
        return
    stored = str(rows[-1].get("ingress_request_id") or "")
    dashless = stored.replace("-", "")
    context.record("request_logs_ingress_request_id", stored)
    matches = bool(header) and header == dashless
    context.expect(
        "correlation header can be used to locate the caller's own request_log row",
        matches,
        expected=f"header == request_logs.ingress_request_id ({dashless})",
        actual=header,
    )
    if not matches:
        context.note(
            "GAP: X-Prism-Ingress-Request-Id is a transport-level id generated by middleware and is not "
            "the persisted request_logs.ingress_request_id, so a caller cannot look up its own request"
        )


CASES = [
    Case("L1-01", "ingress", "unregistered runtime path rejects with no side effects", _unregistered_path),
    Case("L1-02", "ingress", "wrong method on a registered path rejects with no side effects", _wrong_method),
    Case("L1-03", "ingress", "unknown model rejects during planning with no side effects", _unknown_model, requires_live_upstream=False),
    Case("L1-04", "ingress", "proxy-key enforcement matches the instance auth setting", _proxy_key_enforcement, requires_live_upstream=True),
    Case("L1-05", "ingress", "response correlation header maps to the persisted request id", _ingress_correlation_header, requires_live_upstream=True),
]
