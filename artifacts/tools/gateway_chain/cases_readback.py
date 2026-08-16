"""L6 — reading the records back through the management API.

A record that exists in PostgreSQL but cannot be read through the API is not
observable. These cases go through the same surfaces the dashboard uses, and
they check the four-state honesty contract: zero, missing, failed, and
truncated must stay distinguishable on every read.
"""

from __future__ import annotations

from .runner import Case, CaseContext
from .support import audit_window, export_window, live_model, marker, set_audit_mode, wait_for_rows


def _requests_list_and_detail(context: CaseContext) -> None:
    model = live_model(context)
    before = context.store.max_id("request_logs")
    context.gateway.chat(model, marker("READLIST"), max_tokens=8)
    rows = wait_for_rows(context, "request_logs", before, expected=1)
    if not rows:
        context.expect("a request was recorded to read back", False, expected=">=1", actual=0)
        return
    request_id = rows[-1].get("id")

    listing = context.gateway.management("GET", "/stats/requests")
    context.record("list_status", listing.status)
    context.expect_eq("requests list answers 200", 200, listing.status)
    payload = listing.json()
    context.expect(
        "the list response is an object with items",
        isinstance(payload, dict),
        expected="object",
        actual=type(payload).__name__,
    )
    if isinstance(payload, dict):
        context.record("list_keys", sorted(payload.keys()))
        items = payload.get("items") or []
        context.expect("the list is non-empty after a real request", len(items) > 0, expected=">0", actual=len(items))
        # The list projects the attempt row under request_log_id, not id.
        listed_ids = {str(item.get("request_log_id")) for item in items if isinstance(item, dict)}
        context.expect(
            "the request just made appears in the list",
            str(request_id) in listed_ids,
            expected=str(request_id),
            actual=sorted(listed_ids)[:10],
        )

    detail = context.gateway.management("GET", f"/stats/requests/{request_id}")
    context.record("detail_status", detail.status)
    context.expect_eq("request detail answers 200", 200, detail.status)
    body = detail.json()
    if isinstance(body, dict):
        context.record("detail_keys", sorted(body.keys())[:40])
        context.expect(
            "detail carries the caller model",
            model in detail.text,
            expected=f"detail mentions {model}",
            actual=detail.text[:200],
        )


def _requests_list_rejects_unknown_filters(context: CaseContext) -> None:
    """Strict query validation is what keeps a silently-ignored filter from
    producing a wrong answer that looks right."""
    response = context.gateway.management("GET", "/stats/requests", query={"definitely_not_a_filter": "1"})
    context.record("status", response.status)
    context.record("body", response.text[:200])
    context.expect_eq("an unknown query key is rejected", 422, response.status)
    context.expect_eq("the rejection is coded unknown_query_key", "unknown_query_key", (response.json() or {}).get("code"))
    context.expect(
        "the rejection names the offending key",
        "definitely_not_a_filter" in response.text,
        expected="body names the key",
        actual=response.text[:160],
    )


def _requests_csv_export(context: CaseContext) -> None:
    # An unbounded export is refused rather than silently capped, so the range
    # is required. Assert the refusal first, then the successful export.
    unbounded = context.gateway.management("GET", "/stats/requests/export", timeout=60.0)
    context.record("unbounded_status", unbounded.status)
    context.expect_eq("an export without a time range is refused", 422, unbounded.status)
    context.expect_eq(
        "the refusal names the missing range",
        "export_range_required",
        (unbounded.json() or {}).get("code"),
    )

    response = context.gateway.management(
        "GET", "/stats/requests/export", query=export_window(), timeout=180.0
    )
    context.record("status", response.status)
    context.record("content_type", response.header("content-type"))
    context.expect_eq("CSV export answers 200", 200, response.status)
    context.expect(
        "the export is delivered as CSV",
        "csv" in (response.header("content-type") or "").lower(),
        expected="a csv content type",
        actual=response.header("content-type"),
    )
    lines = response.text.splitlines()
    context.record("csv_lines", len(lines))
    context.expect("the export has a header row", len(lines) >= 1, expected=">=1", actual=len(lines))
    if lines:
        context.record("csv_header", lines[0][:300])


def _audit_list_requires_a_window(context: CaseContext) -> None:
    """An unbounded audit scan is refused rather than silently truncated."""
    response = context.gateway.management("GET", "/audit/logs")
    context.record("status", response.status)
    context.record("body", response.text[:200])
    context.expect_eq("audit list without a window is rejected", 400, response.status)
    context.expect(
        "the rejection says a window is required",
        "window" in response.text.lower() or "from" in response.text.lower(),
        expected="body explains the window requirement",
        actual=response.text[:160],
    )


def _audit_read_back(context: CaseContext) -> None:
    model = live_model(context)
    set_audit_mode(context, openai_mode="body_capture")
    token = marker("READAUDIT")
    before = context.store.max_id("audit_logs")
    context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
    rows = wait_for_rows(context, "audit_logs", before, expected=1)
    if not rows:
        context.expect("an audit row was written to read back", False, expected=">=1", actual=0)
        return
    audit_id = rows[-1].get("id")

    listing = context.gateway.management("GET", "/audit/logs", query=audit_window())
    context.record("list_status", listing.status)
    context.expect_eq("audit list answers 200 with a window", 200, listing.status)
    payload = listing.json() if listing.status == 200 else {}
    items = (payload or {}).get("items") or []
    context.record("audit_items", len(items))
    context.expect(
        "the audit entry just written is listed",
        any(str(item.get("id")) == str(audit_id) for item in items),
        expected=str(audit_id),
        actual=[item.get("id") for item in items][:10],
    )
    if items:
        first = next((item for item in items if str(item.get("id")) == str(audit_id)), items[0])
        context.record("list_item_capture_fields", {key: first.get(key) for key in (
            "request_body_stored", "response_body_stored", "request_body_capture_status",
            "request_body_capture_end_state", "request_body_truncated",
            "request_body_preview_unavailable_reason", "request_body_bytes_observed",
            "request_body_bytes_stored",
        )})
        context.expect_eq(
            "the list reports the request body as stored",
            True,
            first.get("request_body_stored"),
        )
        context.expect(
            "observed and stored byte counts are both reported",
            first.get("request_body_bytes_observed") is not None
            and first.get("request_body_bytes_stored") is not None,
            expected="both byte counts present",
            actual=(first.get("request_body_bytes_observed"), first.get("request_body_bytes_stored")),
        )

    detail = context.gateway.management("GET", f"/audit/logs/{audit_id}")
    context.record("detail_status", detail.status)
    context.expect_eq("audit detail answers 200", 200, detail.status)

    request_body = context.gateway.management("GET", f"/audit/logs/{audit_id}/body/request", timeout=60.0)
    response_body = context.gateway.management("GET", f"/audit/logs/{audit_id}/body/response", timeout=60.0)
    context.record("request_body_status", request_body.status)
    context.record("response_body_status", response_body.status)
    context.expect_eq("raw request body reads 200", 200, request_body.status)
    context.expect_eq("raw response body reads 200", 200, response_body.status)
    context.expect(
        "the raw request body still holds the prompt marker",
        token in request_body.text,
        expected=f"contains {token}",
        actual=request_body.text[:200],
    )
    context.expect(
        "the raw response body still holds the model's answer",
        token in response_body.text,
        expected=f"contains {token}",
        actual=response_body.text[-200:],
    )
    proxy_key = context.state.get("proxy_key") or ""
    if proxy_key:
        context.expect(
            "the raw body read does not expose the caller's proxy key",
            proxy_key not in request_body.text,
            expected="proxy key absent",
            actual="present" if proxy_key in request_body.text else "absent",
        )


def _audit_states_are_distinguishable(context: CaseContext) -> None:
    """Zero, missing, failed and truncated must not collapse into one another.

    A metadata_only row and a body_capture row are read through the same API;
    the reason a body is absent must be visible, not inferred from emptiness.
    """
    model = live_model(context)

    set_audit_mode(context, openai_mode="metadata_only")
    before_meta = context.store.max_id("audit_logs")
    context.gateway.chat(model, marker("STATEMETA"), max_tokens=8)
    meta_rows = wait_for_rows(context, "audit_logs", before_meta, expected=1)

    set_audit_mode(context, openai_mode="body_capture")
    before_full = context.store.max_id("audit_logs")
    context.gateway.chat(model, marker("STATEFULL"), max_tokens=8)
    full_rows = wait_for_rows(context, "audit_logs", before_full, expected=1)

    if not (meta_rows and full_rows):
        context.expect(
            "both a metadata-only and a body-capture row were produced",
            False,
            expected="one of each",
            actual=(len(meta_rows), len(full_rows)),
        )
        return

    window = audit_window()
    listing = context.gateway.management("GET", "/audit/logs", query=window)
    items = {str(item.get("id")): item for item in ((listing.json() or {}).get("items") or [])}
    meta_item = items.get(str(meta_rows[-1].get("id")))
    full_item = items.get(str(full_rows[-1].get("id")))
    context.record("metadata_only_projection", meta_item and {key: meta_item.get(key) for key in (
        "request_body_stored", "request_body_capture_status", "request_body_preview",
        "request_body_preview_unavailable_reason", "request_body_bytes_observed",
    )})
    context.record("body_capture_projection", full_item and {key: full_item.get(key) for key in (
        "request_body_stored", "request_body_capture_status", "request_body_preview_truncated",
        "request_body_preview_unavailable_reason", "request_body_bytes_observed",
    )})

    context.expect(
        "both rows are readable through the list API",
        meta_item is not None and full_item is not None,
        expected="both present",
        actual=(meta_item is not None, full_item is not None),
    )
    if meta_item and full_item:
        context.expect(
            "an absent body under metadata_only is reported as not stored, not as an empty body",
            meta_item.get("request_body_stored") is False,
            expected="request_body_stored == False",
            actual=meta_item.get("request_body_stored"),
        )
        context.expect(
            "a captured body is reported as stored",
            full_item.get("request_body_stored") is True,
            expected="request_body_stored == True",
            actual=full_item.get("request_body_stored"),
        )
        context.expect(
            "capture status distinguishes the two rows",
            meta_item.get("request_body_capture_status") != full_item.get("request_body_capture_status"),
            expected="different capture statuses",
            actual=(meta_item.get("request_body_capture_status"), full_item.get("request_body_capture_status")),
        )
        context.expect(
            "truncation is reported explicitly rather than inferred",
            "request_body_preview_truncated" in full_item,
            expected="a truncation field on the projection",
            actual=sorted(full_item.keys())[:20],
        )

    missing = context.gateway.management("GET", "/audit/logs/999999999")
    context.record("missing_detail_status", missing.status)
    context.expect(
        "a missing audit id is reported as missing, not as an empty record",
        missing.status in (400, 404),
        expected="404 (or 400)",
        actual=missing.status,
    )


def _audit_absence_has_a_reason(context: CaseContext) -> None:
    """Asking for the audit of a request that was never audited must say why.

    Returning an empty list here would be indistinguishable from "audited, but
    nothing was captured". The API answers 409 instead.
    """
    model = live_model(context)
    set_audit_mode(context, openai_mode="disabled")
    before = context.store.max_id("request_logs")
    context.gateway.chat(model, marker("NOAUDIT"), max_tokens=8)
    rows = wait_for_rows(context, "request_logs", before, expected=1)
    if not rows:
        context.expect("an unaudited request was recorded", False, expected=">=1", actual=0)
        return
    request_log_id = rows[-1].get("id")

    query = dict(audit_window())
    query["request_log_id"] = str(request_log_id)
    response = context.gateway.management("GET", "/audit/logs", query=query)
    context.record("status", response.status)
    context.record("body", response.text[:300])
    context.expect(
        "an unaudited request is reported as unaudited, not as an empty result",
        response.status != 200 or (response.json() or {}).get("items") is None,
        expected="a status that says 'not audited' (409), never 200 with an empty list",
        actual=f"HTTP {response.status} {response.text[:120]}",
    )
    context.expect_eq("the API answers 409 for a request whose audit was disabled", 409, response.status)


CASES = [
    Case("L6-01", "readback", "requests list and detail return the recorded request", _requests_list_and_detail, requires_live_upstream=True),
    Case("L6-02", "readback", "requests list rejects an unknown filter", _requests_list_rejects_unknown_filters),
    Case("L6-03", "readback", "requests CSV export is served server-side", _requests_csv_export),
    Case("L6-04", "readback", "audit list refuses an unbounded window", _audit_list_requires_a_window),
    Case("L6-05", "readback", "audit list, detail and raw bodies read back", _audit_read_back, requires_live_upstream=True),
    Case("L6-06", "readback", "zero, missing, failed and truncated stay distinguishable", _audit_states_are_distinguishable, requires_live_upstream=True),
    Case("L6-07", "readback", "an unaudited request reads back as unaudited, not as empty", _audit_absence_has_a_reason, requires_live_upstream=True),
]
