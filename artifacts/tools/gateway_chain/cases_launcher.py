"""L0 — launcher contract, bootstrap config, and readiness honesty."""

from __future__ import annotations

from .env import LAUNCHER_DATABASE_URL
from .runner import Case, CaseContext
from .support import as_int


def _launcher_contract(context: CaseContext) -> None:
    settings = context.harness.effective_startup_settings()
    context.record("effective_settings", {key: value for key, value in settings.items() if key != "DATABASE_URL"})
    context.expect_eq(
        "bootstrap resolves the config path start.sh requires",
        str(context.env.config_path),
        settings.get("PRISM_CONFIG_PATH"),
    )
    context.expect(
        "bootstrap host is local",
        settings.get("SERVER_HOST") in ("0.0.0.0", "127.0.0.1", "localhost", "::", "[::]"),
        expected="one of 0.0.0.0/127.0.0.1/localhost/::",
        actual=settings.get("SERVER_HOST"),
    )
    context.expect_eq(
        "bootstrap port matches the port the suite talks to",
        context.env.backend_port,
        as_int(settings.get("SERVER_PORT")),
    )
    context.expect_eq(
        "bootstrap DSN is the launcher-local database",
        LAUNCHER_DATABASE_URL,
        settings.get("DATABASE_URL"),
    )


def _health_ready(context: CaseContext) -> None:
    response = context.gateway.health()
    payload = response.json() if response.status == 200 else None
    context.record("health", payload)
    context.expect_eq("/health answers 200", 200, response.status)
    context.expect_eq("status is ok", "ok", (payload or {}).get("status"))
    context.expect_eq("liveness is ok", "ok", (payload or {}).get("liveness"))
    context.expect_eq("readiness is ready", "ready", (payload or {}).get("readiness"))
    context.expect_eq("startup is complete", "complete", (payload or {}).get("startup"))
    context.expect(
        "version is reported",
        bool((payload or {}).get("version")),
        expected="non-empty",
        actual=(payload or {}).get("version"),
    )


def _migrations_preserved(context: CaseContext) -> None:
    """A database restored from another instance at the same revision must be
    adopted as-is, not re-migrated and not rejected."""
    rows = context.store.rows(
        "select version from prism_schema_migrations order by version", ["version"]
    )
    context.record("migration_count", len(rows))
    context.record("latest_migration", rows[-1]["version"] if rows else None)
    context.expect("migrations are present", len(rows) > 0, expected=">0", actual=len(rows))

    log = context.harness.launcher_log
    text = log.read_text(encoding="utf-8", errors="replace") if log.is_file() else ""
    if "startup sequence completed" in text:
        outcome_line = next(
            (line for line in text.splitlines() if "startup sequence completed" in line), ""
        )
        context.record("startup_outcome", outcome_line.strip()[-120:])
        context.expect(
            "startup adopted the restored schema without re-migrating",
            "migration_outcome=noop" in outcome_line,
            expected="migration_outcome=noop",
            actual=outcome_line.strip()[-80:],
        )
    else:
        context.note("launcher log unavailable; migration outcome not asserted from logs")
        context.expect("schema version table is readable", len(rows) > 0, expected=">0", actual=len(rows))


def _config_carries_secrets(context: CaseContext) -> None:
    """The synced bootstrap file must keep the encryption key, or every stored
    upstream credential would decrypt to garbage."""
    import json

    raw = json.loads(context.env.config_path.read_text(encoding="utf-8"))
    key = (raw.get("runtime") or {}).get("secretEncryptionKey") or ""
    context.expect(
        "runtime.secretEncryptionKey is present",
        len(key) > 0,
        expected="non-empty",
        actual=f"len={len(key)}",
    )
    context.expect_eq(
        "database.url was rewritten to the local launcher DSN",
        LAUNCHER_DATABASE_URL,
        (raw.get("database") or {}).get("url"),
    )
    context.record("cors_allowed_origins", (raw.get("http") or {}).get("corsAllowedOrigins"))


def _readiness_reflects_database(context: CaseContext) -> None:
    """Readiness must not claim ready while the datastore is unreachable.

    This case reads the current state only; it never takes the database down.
    It exists so that a run performed during a real outage records the
    contradiction instead of hiding it.
    """
    reachable = context.store.is_reachable()
    response = context.gateway.health()
    payload = response.json() or {}
    readiness = payload.get("readiness")
    context.record("database_reachable", reachable)
    context.record("readiness", readiness)
    if reachable:
        context.expect_eq("database reachable and readiness ready", "ready", readiness)
    else:
        context.expect(
            "readiness does not claim ready while the database is unreachable",
            readiness != "ready",
            expected="not 'ready'",
            actual=readiness,
        )


def _readiness_reacts_to_a_real_outage(context: CaseContext) -> None:
    """Take the datastore away and see whether readiness notices.

    L0-05 can only observe a contradiction that happens to exist. This case
    creates one: it stops the PostgreSQL container, confirms the gateway really
    cannot serve, asks /health, then restores the container and waits for the
    backend to reconnect. The stack is a dedicated local test instance and the
    container's volume is untouched, so the outage is fully reversible.
    """
    import time as _time

    if not context.store.is_reachable():
        context.block("the database is already unreachable; refusing to compound an existing outage")

    harness = context.harness
    stopped = harness.compose(["stop", "postgres"], timeout=120)
    if stopped.returncode != 0:
        context.block(f"could not stop postgres for the outage probe: {stopped.stderr.strip()[:200]}")
    try:
        # Wait until the gateway actually fails, so the probe is not racing the
        # connection pool's last good connection.
        deadline = _time.monotonic() + 60.0
        serving_status = None
        while _time.monotonic() < deadline:
            probe = context.gateway.runtime_raw(
                "POST", "/v1/chat/completions", body={"model": "probe", "messages": []}, timeout=15.0
            )
            serving_status = probe.status
            if probe.status >= 500:
                break
            _time.sleep(1.0)
        context.record("runtime_status_during_outage", serving_status)
        context.expect(
            "the gateway cannot serve while the datastore is down",
            serving_status is not None and serving_status >= 500,
            expected=">=500",
            actual=serving_status,
        )

        health = context.gateway.health()
        payload = health.json() or {}
        context.record("health_during_outage", payload)
        context.expect(
            "readiness does not claim ready during a total datastore outage",
            payload.get("readiness") != "ready",
            expected="not 'ready'",
            actual=payload.get("readiness"),
        )
        context.expect(
            "status does not claim ok during a total datastore outage",
            payload.get("status") != "ok",
            expected="not 'ok'",
            actual=payload.get("status"),
        )
    finally:
        harness.compose(["start", "postgres"], timeout=180)
        recovered = False
        deadline = _time.monotonic() + 120.0
        while _time.monotonic() < deadline:
            if context.store.is_reachable():
                recovered = True
                break
            _time.sleep(1.0)
        context.record("datastore_recovered", recovered)
        if not recovered:
            context.note("WARNING: the datastore did not come back; later cases will not be meaningful")


CASES = [
    Case("L0-01", "launcher", "start.sh bootstrap contract holds for the synced config", _launcher_contract),
    Case("L0-02", "launcher", "/health reports a fully ready backend", _health_ready),
    Case("L0-03", "launcher", "restored schema is adopted without re-migrating", _migrations_preserved),
    Case("L0-04", "launcher", "synced config keeps its secrets and only localises the DSN", _config_carries_secrets),
    Case("L0-05", "launcher", "readiness and datastore reachability agree", _readiness_reflects_database),
    Case("L0-06", "launcher", "readiness reacts to a real datastore outage", _readiness_reacts_to_a_real_outage),
]
