"""CLI for the live gateway-chain suite.

    python3 artifacts/tools/gateway_chain sync     # pull config+database from a live instance
    python3 artifacts/tools/gateway_chain up       # start.sh headless, wait for ready
    python3 artifacts/tools/gateway_chain run      # execute the case matrix
    python3 artifacts/tools/gateway_chain down     # stop the launcher and its stack
    python3 artifacts/tools/gateway_chain all      # up + run + down
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

if __package__ in (None, ""):  # allow `python3 artifacts/tools/gateway_chain`
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    __package__ = "gateway_chain"

from . import ALL_CASES  # noqa: E402
from .bootstrap import delete_proxy_key, run as bootstrap_run  # noqa: E402
from .env import EnvError, load_env, secret_values  # noqa: E402
from .harness import Harness, HarnessError  # noqa: E402
from .httpclient import Gateway  # noqa: E402
from .runner import CaseContext, ERRORED, FAILED, Runner  # noqa: E402
from .store import Store, StoreUnavailable  # noqa: E402


def _run_id() -> str:
    return datetime.now(timezone.utc).strftime("gateway-chain-%Y%m%dT%H%M%SZ")


def _command_sync(env, harness: Harness) -> int:
    workdir = env.state_dir / "sync"
    synced = harness.sync_from_remote(workdir=workdir)
    restored = harness.restore_database(workdir=workdir)
    print(json.dumps({"sync": synced, "restore": restored}, indent=2))
    return 0


def _command_up(env, harness: Harness) -> int:
    started = harness.start()
    print(json.dumps(started, indent=2))
    return 0


def _command_down(env, harness: Harness) -> int:
    print(json.dumps(harness.stop(), indent=2))
    return 0


def _command_run(env, harness: Harness, *, only: list[str] | None, keep_key: bool) -> int:
    store = Store(env=env)
    if not store.is_reachable():
        print("ERROR: the database is not reachable; refusing to report an empty run as a pass", file=sys.stderr)
        return 3
    gateway = Gateway(env.runtime_base, env.management_base)

    boot = bootstrap_run(store, gateway, env=env)
    for note in boot.notes:
        print(f"bootstrap: {note}", flush=True)

    state = {
        "live_model": boot.live_model,
        "dead_model": boot.dead_model,
        "failover_model": boot.failover_model,
        "proxy_key": boot.proxy_key,
        "upstream_api_key": env.upstream_api_key,
    }
    context = CaseContext(env=env, gateway=gateway, store=store, harness=harness, state=state)
    run_id = _run_id()
    evidence_dir = env.evidence_root / run_id
    def telemetry_sentinel() -> str | None:
        """A wedged outbox makes every recording assertion fail for one reason."""
        stuck = store.json_rows(
            "select id, core_attempt_count from runtime_telemetry_outbox "
            "where created_at < now() - interval '90 seconds' order by id limit 1"
        )
        if not stuck:
            return None
        return (
            f"runtime telemetry outbox is wedged: row {stuck[0].get('id')} has not materialized "
            f"(core_attempt_count={stuck[0].get('core_attempt_count')}); every later row is blocked "
            "behind it, so nothing is being recorded. See case L7-02."
        )

    runner = Runner(
        context,
        secrets=secret_values(env, extra=[boot.proxy_key] if boot.proxy_key else []),
        evidence_dir=evidence_dir,
        sentinel=telemetry_sentinel,
    )

    original_audit_mode = None
    try:
        current = gateway.management("GET", "/settings/audit")
        if current.status == 200:
            for policy in (current.json() or {}).get("policies", []):
                if policy.get("family") == "openai":
                    original_audit_mode = policy.get("mode")
        runner.run(ALL_CASES, only=only)
    finally:
        # Leave the instance the way it was found.
        if original_audit_mode:
            try:
                from .support import set_audit_mode

                set_audit_mode(context, openai_mode=original_audit_mode)
            except (StoreUnavailable, Exception):  # noqa: BLE001
                print("warning: could not restore the original audit policy", file=sys.stderr)
        if boot.proxy_key_name and not keep_key:
            delete_proxy_key(gateway, boot.proxy_key_name)

    report_path = runner.write_evidence(
        run_id,
        extra={
            "repo_root": str(env.repo_root),
            "compose_project": env.compose_project,
            "config_path": str(env.config_path),
            "backend_base": env.backend_base,
            "bootstrap": boot.to_json(),
            "original_audit_mode": original_audit_mode,
        },
    )
    summary = runner.summary()
    print("\n" + json.dumps(summary, indent=2))
    if report_path:
        print(f"evidence: {report_path}")
    return 1 if (summary.get(FAILED) or summary.get(ERRORED)) else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="gateway_chain", description=__doc__)
    parser.add_argument("command", choices=["sync", "up", "run", "down", "all"])
    parser.add_argument("--only", help="comma-separated case ids to run")
    parser.add_argument("--keep-proxy-key", action="store_true", help="do not delete the proxy key the run created")
    arguments = parser.parse_args(argv)

    try:
        env = load_env()
    except EnvError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2
    harness = Harness(env=env)
    only = [value.strip() for value in arguments.only.split(",")] if arguments.only else None

    try:
        if arguments.command == "sync":
            return _command_sync(env, harness)
        if arguments.command == "up":
            return _command_up(env, harness)
        if arguments.command == "down":
            return _command_down(env, harness)
        if arguments.command == "run":
            return _command_run(env, harness, only=only, keep_key=arguments.keep_proxy_key)
        _command_up(env, harness)
        try:
            return _command_run(env, harness, only=only, keep_key=arguments.keep_proxy_key)
        finally:
            _command_down(env, harness)
    except (HarnessError, StoreUnavailable) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 3


if __name__ == "__main__":
    raise SystemExit(main())
