#!/usr/bin/env python3
"""Plan or execute a staged Prism rollout from a published release manifest."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
from collections.abc import Callable, Iterable
from pathlib import Path
from typing import cast

SKILLS_ROOT = Path(__file__).resolve().parents[2]
BACKUP_SCRIPTS = SKILLS_ROOT / "prism-backup-restore" / "scripts"
sys.path.insert(0, str(BACKUP_SCRIPTS))
from prism_backup_common import (
    REMOTE_COMMON,
    OpsError,
    ssh_python,
    validate_service,
)


class RolloutError(RuntimeError):
    pass


REMOTE_DEPLOY = (
    REMOTE_COMMON
    + r"""

def deploy_main():
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    if safe_state(inspect(topology["pg_id"]))["health"] != "healthy":
        raise RuntimeError("postgres is not healthy before deploy")
    if safe_state(inspect(topology["app_id"]))["status"] == "running":
        raise RuntimeError("verified backup must leave the app stopped before deploy")
    deploy_image(topology, args["image_ref"])
    health = wait_health(topology, expected_version=args["version"])
    post = database_evidence(topology)
    pre = args["preflight_database"]
    if not set(pre["schema_versions"]).issubset(post["schema_versions"]):
        raise RuntimeError("migration history lost a preflight version")
    for key in ("models", "connections", "access_targets", "request_logs", "usage_request_events"):
        if post["counts"][key] < pre["counts"][key]:
            raise RuntimeError(f"entity count decreased after migration: {key}")
    for key, previous in pre.get("historical_evidence", {}).items():
        current = post.get("historical_evidence", {}).get(key)
        if previous is None and current not in (None, 0):
            raise RuntimeError(f"migration fabricated historical evidence: {key}")
        if previous is not None and (current is None or current < previous):
            raise RuntimeError(f"historical evidence regressed: {key}")
    if file_sha(topology["config_path"]) != args["config_sha256"]:
        raise RuntimeError("config hash changed during deploy")
    duplicates = psql(topology, "SELECT count(*) FROM (SELECT target_connection_id FROM model_access_targets WHERE target_connection_id IS NOT NULL GROUP BY target_connection_id HAVING count(*) > 1) d")
    orphans = psql(topology, "SELECT count(*) FROM connections c LEFT JOIN model_access_targets m ON m.target_connection_id=c.id WHERE m.id IS NULL")
    if duplicates != "0" or orphans != "0":
        raise RuntimeError("owner/orphan invariant failed after deploy")
    has_upstream = psql(topology, "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='connections' AND column_name='upstream_model_id'")
    if has_upstream == "1":
        invalid = psql(topology, "SELECT count(*) FROM model_access_targets m JOIN model_configs o ON o.id=m.source_model_config_id JOIN connections c ON c.id=m.target_connection_id WHERE m.target_connection_id IS NOT NULL AND (c.upstream_model_id IS NULL OR btrim(c.upstream_model_id)='' OR c.upstream_model_id IS DISTINCT FROM o.model_id)")
        if invalid != "0":
            raise RuntimeError("owner-backed upstream model id invariant failed")
    app = safe_state(inspect(topology["app_id"]))
    print(json.dumps({
        "service": topology["service"], "app_container": topology["app_id"],
        "postgres_container": topology["pg_id"], "health": health,
        "app": app, "database": post, "config_sha256": args["config_sha256"],
    }, sort_keys=True))


try:
    deploy_main()
except Exception as exc:
    print(json.dumps({"error": str(exc)}), file=sys.stderr)
    raise SystemExit(1)
"""
)


REMOTE_STOP = (
    REMOTE_COMMON
    + r"""
try:
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    stop_app(topology)
    print(json.dumps({"service": topology["service"], "status": "stopped"}))
except Exception as exc:
    print(json.dumps({"error": str(exc)}), file=sys.stderr)
    raise SystemExit(1)
"""
)


REMOTE_TELEMETRY = (
    REMOTE_COMMON
    + r"""
def literal(value):
    return "'" + value.replace("'", "''") + "'"

try:
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    models = ",".join(literal(value) for value in args["models"])
    start = literal(args["start"])
    query = (
        "SELECT count(DISTINCT ingress_request_id),"
        "count(*) FILTER (WHERE row_kind='upstream' AND success_flag IS TRUE AND output_tokens>0),"
        "count(*) FILTER (WHERE row_kind='upstream' AND upstream_model_id IS NULL) "
        f"FROM request_logs WHERE created_at >= {start}::timestamptz AND model_id IN ({models})"
    )
    request_counts = [int(value) for value in psql(topology, query).split("|")]
    usage_query = (
        "SELECT count(*),count(*) FILTER (WHERE upstream_model_id IS NULL),"
        "count(*) FILTER (WHERE success_flag IS TRUE AND output_tokens>0) "
        f"FROM usage_request_events WHERE created_at >= {start}::timestamptz AND model_id IN ({models})"
    )
    usage_counts = [int(value) for value in psql(topology, usage_query).split("|")]
    if request_counts[0] < 4 or request_counts[1] < 4 or request_counts[2] != 0:
        raise RuntimeError(f"request attribution gate failed: {request_counts}")
    if usage_counts[0] < 4 or usage_counts[1] != 0 or usage_counts[2] < 4:
        raise RuntimeError(f"usage attribution gate failed: {usage_counts}")
    print(json.dumps({"request_counts": request_counts, "usage_counts": usage_counts}, sort_keys=True))
except Exception as exc:
    print(json.dumps({"error": str(exc)}), file=sys.stderr)
    raise SystemExit(1)
"""
)


REMOTE_OBSERVE = (
    REMOTE_COMMON
    + r"""
try:
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    initial_restarts = safe_state(inspect(topology["app_id"]))["restarts"]
    deadline = time.monotonic() + args["seconds"]
    samples = 0
    while True:
        state = safe_state(inspect(topology["app_id"]))
        if state["status"] != "running" or state["health"] != "healthy":
            raise RuntimeError("app became unhealthy during observation")
        if state["restarts"] != initial_restarts or state["image_ref"] != args["image_ref"]:
            raise RuntimeError("restart count or image reference changed during observation")
        wait_health(topology, expected_version=args["version"], attempts=1, interval=0)
        samples += 1
        if time.monotonic() >= deadline:
            break
        time.sleep(min(10, max(0, deadline - time.monotonic())))
    print(json.dumps({"samples": samples, "restarts": initial_restarts, "status": "stable"}, sort_keys=True))
except Exception as exc:
    print(json.dumps({"error": str(exc)}), file=sys.stderr)
    raise SystemExit(1)
"""
)


REMOTE_READ_JSON = r"""
import base64
import json
from pathlib import Path
import sys

args = json.loads(base64.urlsafe_b64decode(sys.argv[1].encode("ascii")))
path = Path(args["path"])
if path.is_symlink() or not path.is_file():
    raise SystemExit("JSON evidence path is not a regular file")
value = json.loads(path.read_text(encoding="utf-8"))
if not isinstance(value, dict):
    raise SystemExit("JSON evidence is not an object")
print(json.dumps(value, sort_keys=True))
"""


def load_manifest(path: Path) -> dict[str, object]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise RolloutError(f"cannot read release manifest: {path}") from exc
    if value.get("schema_version") != 1 or value.get("status") != "published":
        raise RolloutError("rollout requires a published schema-version-1 manifest")
    image = value.get("image")
    if not isinstance(image, dict):
        raise RolloutError("release manifest lacks image evidence")
    digest = str(image.get("manifest_digest", ""))
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise RolloutError("release manifest has an invalid image digest")
    repository = str(image.get("repository", ""))
    repository_slug = str(value.get("repository", ""))
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository_slug):
        raise RolloutError("release manifest has an invalid repository slug")
    owner, repository_name = repository_slug.split("/", 1)
    if owner in {".", ".."} or repository_name in {".", ".."}:
        raise RolloutError("release manifest repository contains a dot path component")
    expected_repository = f"ghcr.io/{repository_slug.lower()}"
    if repository != expected_repository:
        raise RolloutError("release manifest repository and image repository differ")
    expected_ref = f"{repository}:{value.get('tag')}@{digest}"
    if not repository or image.get("ref") != expected_ref:
        raise RolloutError("release manifest lacks an immutable image reference")
    if image.get("revision") != value.get("release_sha") or image.get(
        "version"
    ) != value.get("version"):
        raise RolloutError("release manifest OCI identity is inconsistent")
    return value


def canonical_service_order(services: list[str]) -> list[str]:
    ordered = list(dict.fromkeys(services))
    if len(ordered) != len(services):
        raise RolloutError("duplicate rollout services are not allowed")
    if "prism-b" in ordered and "prism-a" not in ordered:
        raise RolloutError("prism-b rollout requires prism-a in the same staged run")
    if "prism-a" in ordered and "prism-b" in ordered:
        ordered.remove("prism-a")
        ordered.remove("prism-b")
        ordered = ["prism-a", "prism-b", *ordered]
    return ordered


def rollout_token(manifest: dict[str, object]) -> str:
    return f"{manifest['tag']}@{str(manifest['release_sha'])[:12]}"


def validate_image_identity(
    manifest: dict[str, object], inspected: dict[str, object], host_architecture: str
) -> None:
    expected = cast("dict[str, object]", manifest["image"])
    if inspected.get("manifest_digest") != expected.get("manifest_digest"):
        raise RolloutError("published image digest differs from the release manifest")
    if inspected.get("revision") != manifest.get("release_sha"):
        raise RolloutError("published image revision differs from the release manifest")
    if inspected.get("version") != manifest.get("version"):
        raise RolloutError("published image version differs from the release manifest")
    if (
        inspected.get("os") != "linux"
        or inspected.get("architecture") != host_architecture
    ):
        raise RolloutError("published image platform differs from the deployment host")


def inspect_remote_image(host: str, manifest: dict[str, object]) -> dict[str, object]:
    image_ref = str(cast("dict[str, object]", manifest["image"])["ref"])
    manifest_result = subprocess.run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            host,
            "docker",
            "buildx",
            "imagetools",
            "inspect",
            image_ref,
            "--format",
            "{{json .Manifest}}",
        ],
        text=True,
        capture_output=True,
        check=False,
    )
    image_result = subprocess.run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            host,
            "docker",
            "buildx",
            "imagetools",
            "inspect",
            image_ref,
            "--format",
            "{{json .Image}}",
        ],
        text=True,
        capture_output=True,
        check=False,
    )
    arch_result = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", host, "uname", "-m"],
        text=True,
        capture_output=True,
        check=False,
    )
    if manifest_result.returncode or image_result.returncode or arch_result.returncode:
        raise RolloutError("cannot revalidate the published image before rollout")
    try:
        manifest_value = json.loads(manifest_result.stdout)
        image_value = json.loads(image_result.stdout)
    except ValueError as exc:
        raise RolloutError("published image inspection returned invalid JSON") from exc
    labels = image_value.get("config", {}).get("Labels", {})
    inspected = {
        "manifest_digest": manifest_value.get("digest"),
        "revision": labels.get("org.opencontainers.image.revision"),
        "version": labels.get("org.opencontainers.image.version"),
        "os": image_value.get("os"),
        "architecture": image_value.get("architecture"),
    }
    host_arch = {"aarch64": "arm64", "x86_64": "amd64"}.get(
        arch_result.stdout.strip(), arch_result.stdout.strip()
    )
    validate_image_identity(manifest, inspected, host_arch)
    return inspected


def run(
    argv: list[str], *, input_bytes: bytes | None = None
) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run(argv, input=input_bytes, capture_output=True, check=False)


def parse_http_output(raw: bytes) -> tuple[int, bytes]:
    marker = b"\n__PRISM_STATUS__:"
    if marker not in raw:
        raise RolloutError("provider smoke response omitted HTTP status marker")
    body, status_raw = raw.rsplit(marker, 1)
    try:
        return int(status_raw.strip()), body
    except ValueError as exc:
        raise RolloutError("provider smoke returned an invalid HTTP status") from exc


def should_retry_status(status: int) -> bool:
    return status == 429 or 500 <= status <= 599


def remote_http(
    host: str, container: str, path: str, payload: dict[str, object]
) -> tuple[int, bytes]:
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    result = run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            host,
            "docker",
            "exec",
            "-i",
            container,
            "curl",
            "-sS",
            "-N",
            "--connect-timeout",
            "10",
            "--max-time",
            "180",
            "-H",
            "Content-Type: application/json",
            "--data-binary",
            "@-",
            "-w",
            "\n__PRISM_STATUS__:%{http_code}",
            f"http://127.0.0.1:8080{path}",
        ],
        input_bytes=body,
    )
    if result.returncode != 0:
        raise RolloutError("provider smoke transport failed")
    return parse_http_output(result.stdout)


def request_once_with_retry(
    fetch: Callable[[], tuple[int, bytes]],
    *,
    sleep: Callable[[float], None] = time.sleep,
) -> bytes:
    for attempt in range(2):
        status, body = fetch()
        if 200 <= status <= 299:
            return body
        if attempt == 0 and should_retry_status(status):
            sleep(10)
            continue
        raise RolloutError(f"provider smoke failed with HTTP {status}")
    raise RolloutError("provider smoke retry exhausted")


def require_chat_nonstream(body: bytes) -> None:
    try:
        payload = json.loads(body)
        content = payload["choices"][0]["message"]["content"]
    except (ValueError, KeyError, IndexError, TypeError) as exc:
        raise RolloutError("chat non-stream response is invalid") from exc
    if not isinstance(content, str) or not content:
        raise RolloutError("chat non-stream response has no visible output")


def sse_data(body: bytes) -> list[dict[str, object] | str]:
    values: list[dict[str, object] | str] = []
    for raw in body.decode("utf-8", errors="replace").splitlines():
        if not raw.startswith("data:"):
            continue
        data = raw[5:].strip()
        if data == "[DONE]":
            values.append(data)
            continue
        try:
            value = json.loads(data)
        except ValueError:
            continue
        if isinstance(value, dict):
            values.append(value)
    return values


def require_chat_stream(body: bytes) -> None:
    values = sse_data(body)
    visible = []
    for value in values:
        if not isinstance(value, dict):
            continue
        for choice in cast("list[object]", value.get("choices", [])):
            content = cast("dict[str, object]", choice).get("delta", {}).get("content")
            if isinstance(content, str):
                visible.append(content)
    if "[DONE]" not in values or not "".join(visible):
        raise RolloutError("chat stream has no visible output or terminal marker")


def require_responses_nonstream(body: bytes) -> None:
    try:
        payload = json.loads(body)
    except ValueError as exc:
        raise RolloutError("Responses non-stream body is invalid JSON") from exc
    visible = []
    for item in payload.get("output", []):
        for content in item.get("content", []):
            if isinstance(content.get("text"), str):
                visible.append(content["text"])
    if payload.get("status") != "completed" or not "".join(visible):
        raise RolloutError("Responses non-stream has no completed visible output")


def require_responses_stream(body: bytes) -> None:
    values = [value for value in sse_data(body) if isinstance(value, dict)]
    visible = [
        value.get("delta", "")
        for value in values
        if value.get("type") == "response.output_text.delta"
    ]
    completed = any(value.get("type") == "response.completed" for value in values)
    if not completed or not "".join(
        value for value in visible if isinstance(value, str)
    ):
        raise RolloutError("Responses stream has no visible output or completed event")


def run_provider_smoke(
    host: str,
    container: str,
    chat_model: str,
    chat_tokens: int,
    responses_model: str,
    responses_tokens: int,
) -> str:
    start = dt.datetime.now(dt.timezone.utc).isoformat()
    suffix = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    calls: list[tuple[str, dict[str, object], Callable[[bytes], None]]] = [
        (
            "/v1/chat/completions",
            {
                "model": chat_model,
                "messages": [
                    {
                        "role": "user",
                        "content": f"Reply exactly: PRISM_SMOKE_CHAT_NONSTREAM_{suffix}",
                    }
                ],
                "stream": False,
                "max_tokens": chat_tokens,
            },
            require_chat_nonstream,
        ),
        (
            "/v1/chat/completions",
            {
                "model": chat_model,
                "messages": [
                    {
                        "role": "user",
                        "content": f"Reply exactly: PRISM_SMOKE_CHAT_STREAM_{suffix}",
                    }
                ],
                "stream": True,
                "max_tokens": chat_tokens,
            },
            require_chat_stream,
        ),
        (
            "/v1/responses",
            {
                "model": responses_model,
                "input": f"Reply exactly: PRISM_SMOKE_RESPONSES_NONSTREAM_{suffix}",
                "stream": False,
                "max_output_tokens": responses_tokens,
            },
            require_responses_nonstream,
        ),
        (
            "/v1/responses",
            {
                "model": responses_model,
                "input": f"Reply exactly: PRISM_SMOKE_RESPONSES_STREAM_{suffix}",
                "stream": True,
                "max_output_tokens": responses_tokens,
            },
            require_responses_stream,
        ),
    ]
    for path, payload, validator in calls:
        body = request_once_with_retry(
            lambda path=path, payload=payload: remote_http(
                host, container, path, payload
            )
        )
        validator(body)
    return start


def run_services(
    services: Iterable[str], handler: Callable[[str], object]
) -> list[object]:
    results = []
    for service in services:
        results.append(handler(service))
    return results


def write_new(path: Path, value: dict[str, object]) -> None:
    if path.exists():
        raise RolloutError(f"refusing to overwrite rollout evidence: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(
        json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    os.replace(temporary, path)


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    subparsers = result.add_subparsers(dest="action", required=True)
    for action in ("plan", "execute"):
        command = subparsers.add_parser(action)
        command.add_argument("--manifest", type=Path, required=True)
        command.add_argument("--host", default="capy")
        command.add_argument("--service", action="append", dest="services")
        command.add_argument("--backup-root")
        command.add_argument(
            "--backup-compression", type=int, choices=range(10), default=6
        )
        command.add_argument("--confirm-rollout")
        command.add_argument("--confirm-prune", action="append", default=[])
        command.add_argument("--allow-provider-smoke", action="store_true")
        command.add_argument("--chat-model", default="deepseek-v4-flash")
        command.add_argument("--chat-max-tokens", type=int, default=256)
        command.add_argument("--responses-model", default="codex/gpt-5.5")
        command.add_argument("--responses-max-output-tokens", type=int, default=32)
        command.add_argument("--observe-seconds", type=int, default=300)
        command.add_argument("--evidence", type=Path)
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    manifest_path = args.manifest.resolve()
    manifest = load_manifest(manifest_path)
    services = canonical_service_order(
        [validate_service(value) for value in (args.services or ["prism-a", "prism-b"])]
    )
    token = rollout_token(manifest)
    required_prune = {f"{service}:keep-3" for service in services}
    plan = {
        "action": args.action,
        "manifest": str(manifest_path),
        "release_sha": manifest["release_sha"],
        "tag": manifest["tag"],
        "image_ref": manifest["image"]["ref"],
        "services": services,
        "confirm_rollout": token,
        "required_prune_confirmations": sorted(required_prune),
        "provider_smoke": args.allow_provider_smoke,
        "observe_seconds": args.observe_seconds,
    }
    if args.action == "plan":
        print(json.dumps(plan, ensure_ascii=False, indent=2, sort_keys=True))
        return 0
    if args.confirm_rollout != token:
        raise RolloutError(f"execute requires --confirm-rollout {token}")
    if set(args.confirm_prune) != required_prune:
        raise RolloutError(
            f"execute requires prune confirmations: {sorted(required_prune)}"
        )
    image_inspection = inspect_remote_image(args.host, manifest)
    backup_script = BACKUP_SCRIPTS / "prism_backup.py"
    prune_script = BACKUP_SCRIPTS / "prism_prune_backups.py"
    completed: list[dict[str, object]] = []

    def deploy_service(service: str) -> dict[str, object]:
        backup_command = [
            sys.executable,
            str(backup_script),
            "execute",
            "--host",
            args.host,
            "--service",
            service,
            "--mode",
            "quiesced",
            "--compression",
            str(args.backup_compression),
            "--confirm-backup",
            service,
        ]
        if args.backup_root:
            backup_command += ["--backup-root", args.backup_root]
        backup_result = subprocess.run(
            backup_command, text=True, capture_output=True, check=False
        )
        if backup_result.returncode != 0:
            raise RolloutError(f"{service}: backup failed")
        backup = json.loads(backup_result.stdout)
        remote_manifest_path = backup["manifest"]
        # The backup result exposes the preflight values needed by the deploy gate.
        backup_manifest_data = ssh_python(
            args.host,
            REMOTE_READ_JSON,
            {"path": remote_manifest_path},
        )
        try:
            post = ssh_python(
                args.host,
                REMOTE_DEPLOY,
                {
                    "service": service,
                    "backup_root": args.backup_root,
                    "image_ref": manifest["image"]["ref"],
                    "version": manifest["version"],
                    "preflight_database": backup_manifest_data["database"],
                    "config_sha256": backup_manifest_data["config_sha256"],
                },
                timeout=None,
            )
            smoke = None
            if args.allow_provider_smoke:
                smoke_start = run_provider_smoke(
                    args.host,
                    str(post["app_container"]),
                    args.chat_model,
                    args.chat_max_tokens,
                    args.responses_model,
                    args.responses_max_output_tokens,
                )
                smoke = ssh_python(
                    args.host,
                    REMOTE_TELEMETRY,
                    {
                        "service": service,
                        "backup_root": args.backup_root,
                        "start": smoke_start,
                        "models": [args.chat_model, args.responses_model],
                    },
                    timeout=180,
                )
            observation = ssh_python(
                args.host,
                REMOTE_OBSERVE,
                {
                    "service": service,
                    "backup_root": args.backup_root,
                    "seconds": args.observe_seconds,
                    "image_ref": manifest["image"]["ref"],
                    "version": manifest["version"],
                },
                timeout=max(60, args.observe_seconds + 60),
            )
        except Exception:
            try:
                ssh_python(
                    args.host,
                    REMOTE_STOP,
                    {"service": service, "backup_root": args.backup_root},
                )
            except OpsError as stop_error:
                print(
                    f"warning: {service}: failed to enforce stop gate: {stop_error}",
                    file=sys.stderr,
                )
            raise
        prune_command = [
            sys.executable,
            str(prune_script),
            "execute",
            "--host",
            args.host,
            "--service",
            service,
            "--keep",
            "3",
            "--confirm-prune",
            f"{service}:keep-3",
            "--protect",
            str(Path(str(remote_manifest_path)).parent),
        ]
        if args.backup_root:
            prune_command += ["--backup-root", args.backup_root]
        prune_result = subprocess.run(
            prune_command, text=True, capture_output=True, check=False
        )
        if prune_result.returncode != 0:
            raise RolloutError(f"{service}: post-rollout retention failed")
        result = {
            "service": service,
            "backup": backup,
            "post_deploy": post,
            "smoke": smoke,
            "observation": observation,
            "retention": json.loads(prune_result.stdout),
        }
        completed.append(result)
        return result

    run_services(services, deploy_service)
    evidence = {
        "schema_version": 1,
        "status": "complete",
        "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "release_manifest": str(manifest_path),
        "release_sha": manifest["release_sha"],
        "image_ref": manifest["image"]["ref"],
        "image_inspection": image_inspection,
        "services": completed,
    }
    evidence_path = args.evidence
    if evidence_path is None:
        stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        repo = Path.cwd().resolve()
        evidence_path = (
            repo
            / "artifacts"
            / "evidence"
            / "prism-ops"
            / "rollouts"
            / f"{stamp}-{manifest['tag']}.json"
        )
    write_new(evidence_path.resolve(), evidence)
    print(
        json.dumps(
            {"evidence": str(evidence_path.resolve()), **evidence},
            ensure_ascii=False,
            indent=2,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OpsError, RolloutError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
