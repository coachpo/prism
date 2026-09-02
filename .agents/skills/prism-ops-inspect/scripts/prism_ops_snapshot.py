#!/usr/bin/env python3
"""Collect a secret-safe Prism repository and deployment snapshot."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

SCHEMA_VERSION = 1
DEFAULT_SERVICES = ("prism-a", "prism-b")
SECRET_PATTERN = re.compile(
    r"(?i)(password|passwd|token|secret|api[-_ ]?key|credential|authorization|cookie|database[_ .-]?url)"
)
REMOTE_PROGRAM = r"""
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys


def run(argv):
    result = subprocess.run(argv, text=True, capture_output=True)
    if result.returncode != 0:
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(argv[:3])}")
    return result.stdout.strip()


def compose_rows():
    raw = run(["docker", "compose", "ls", "--format", "json"])
    if not raw:
        return []
    value = json.loads(raw)
    return value if isinstance(value, list) else [value]


def container_id(project, service):
    raw = run([
        "docker", "ps", "-aq",
        "--filter", f"label=com.docker.compose.project={project}",
        "--filter", f"label=com.docker.compose.service={service}",
    ])
    ids = [line for line in raw.splitlines() if line]
    if len(ids) != 1:
        raise RuntimeError(f"expected one {project}/{service} container, found {len(ids)}")
    return ids[0]


def inspect(container):
    return json.loads(run(["docker", "inspect", container]))[0]


def file_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def psql(pg_container, sql):
    command = (
        'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" '
        '-d "$POSTGRES_DB" -AtF "|" -qc ' + json.dumps(sql)
    )
    return run(["docker", "exec", pg_container, "sh", "-lc", command])


def directory_size(path):
    total = 0
    for root, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [name for name in dirs if not (Path(root) / name).is_symlink()]
        for name in files:
            item = Path(root) / name
            if not item.is_symlink():
                try:
                    total += item.stat().st_size
                except FileNotFoundError:
                    pass
    return total


def backup_inventory(root, service):
    service_root = root / service
    items = []
    if service_root.is_dir():
        for child in sorted(service_root.iterdir()):
            if not child.is_dir() or child.is_symlink():
                continue
            item = {
                "name": child.name,
                "path": str(child),
                "size_bytes": directory_size(child),
                "managed": False,
                "status": "legacy_or_incomplete",
            }
            manifest = child / "manifest.json"
            if manifest.is_file() and not manifest.is_symlink() and manifest.stat().st_size <= 1024 * 1024:
                try:
                    data = json.loads(manifest.read_text(encoding="utf-8"))
                    item["managed"] = data.get("schema_version") == 1 and data.get("service") == service
                    item["status"] = str(data.get("status", "unknown"))
                    item["created_at"] = data.get("created_at")
                except (OSError, ValueError):
                    item["status"] = "invalid_manifest"
            items.append(item)
    capacity_target = root if root.exists() else root.parent
    usage = shutil.disk_usage(capacity_target)
    return {
        "root": str(service_root),
        "items": items,
        "filesystem": {"total_bytes": usage.total, "used_bytes": usage.used, "free_bytes": usage.free},
    }


def safe_container(info):
    state = info.get("State", {})
    health = state.get("Health") or {}
    mounts = [
        {"source": mount.get("Source"), "destination": mount.get("Destination"), "type": mount.get("Type")}
        for mount in info.get("Mounts", [])
        if mount.get("Destination") == "/app/config"
    ]
    ports = []
    for target, bindings in (info.get("NetworkSettings", {}).get("Ports") or {}).items():
        for binding in bindings or []:
            ports.append({"target": target, "host_ip": binding.get("HostIp"), "host_port": binding.get("HostPort")})
    return {
        "name": info.get("Name", "").lstrip("/"),
        "image_ref": info.get("Config", {}).get("Image"),
        "image_id": info.get("Image"),
        "status": state.get("Status"),
        "health": health.get("Status"),
        "restarts": info.get("RestartCount"),
        "started_at": state.get("StartedAt"),
        "ports": ports,
        "config_mounts": mounts,
    }


def main():
    services = sys.argv[1:]
    rows = compose_rows()
    output = {"observed_at": dt.datetime.now(dt.timezone.utc).isoformat(), "services": {}, "limitations": []}
    for service in services:
        matches = [row for row in rows if row.get("Name") == service]
        if len(matches) != 1:
            output["services"][service] = {"error": f"expected one Compose project, found {len(matches)}"}
            continue
        config_files = [Path(value) for value in str(matches[0].get("ConfigFiles", "")).split(",") if value]
        if len(config_files) != 1:
            output["services"][service] = {"error": f"expected one Compose config file, found {len(config_files)}"}
            continue
        config_file = config_files[0]
        app_id = container_id(service, "prism")
        pg_id = container_id(service, "postgres")
        app_info = inspect(app_id)
        pg_info = inspect(pg_id)
        app = safe_container(app_info)
        postgres = safe_container(pg_info)
        config_paths = [Path(item["source"]) / "config.json" for item in app["config_mounts"] if item.get("source")]
        config_evidence = {"path": None, "sha256": None, "mode": None, "size_bytes": None}
        if len(config_paths) == 1 and config_paths[0].is_file():
            config_path = config_paths[0]
            stat = config_path.stat()
            config_evidence = {
                "path": str(config_path),
                "sha256": file_hash(config_path),
                "mode": oct(stat.st_mode & 0o777),
                "size_bytes": stat.st_size,
            }
        latest = psql(pg_id, "SELECT version FROM prism_schema_migrations ORDER BY version DESC LIMIT 1")
        counts = psql(
            pg_id,
            "SELECT (SELECT count(*) FROM model_configs),(SELECT count(*) FROM connections),"
            "(SELECT count(*) FROM model_access_targets),(SELECT count(*) FROM request_logs),"
            "(SELECT count(*) FROM usage_request_events)",
        ).split("|")
        database_size = int(psql(pg_id, "SELECT pg_database_size(current_database())"))
        postgres_version = psql(pg_id, "SHOW server_version")
        deploy_root = config_file.parent.parent
        output["services"][service] = {
            "compose": {"project": service, "config_file": str(config_file), "deploy_root": str(deploy_root)},
            "app": app,
            "postgres": postgres,
            "config": config_evidence,
            "database": {
                "server_version": postgres_version,
                "latest_migration": latest,
                "size_bytes": database_size,
                "counts": {
                    "models": int(counts[0]), "connections": int(counts[1]),
                    "access_targets": int(counts[2]), "request_logs": int(counts[3]),
                    "usage_request_events": int(counts[4]),
                },
            },
            "backups": backup_inventory(deploy_root / "backups", service),
        }
    print(json.dumps(output, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    main()
"""


class SnapshotError(RuntimeError):
    pass


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def run(
    argv: list[str], *, cwd: Path | None = None, input_text: str | None = None
) -> str:
    result = subprocess.run(
        argv, cwd=cwd, input=input_text, text=True, capture_output=True, check=False
    )
    if result.returncode != 0:
        detail = redact_text((result.stderr or result.stdout).strip())
        raise SnapshotError(
            f"command failed ({result.returncode}): {argv[0]}: {detail}"
        )
    return result.stdout.strip()


def redact_text(value: str) -> str:
    lines = []
    for line in value.splitlines():
        lines.append(
            "<redacted secret-bearing line>" if SECRET_PATTERN.search(line) else line
        )
    return "\n".join(lines)


def git_value(repo: Path, *args: str, optional: bool = False) -> str | None:
    try:
        return run(["git", "-C", str(repo), *args])
    except SnapshotError:
        if optional:
            return None
        raise


def parse_origin_slug(origin: str | None) -> str | None:
    if not origin:
        return None
    match = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$", origin)
    return f"{match.group(1)}/{match.group(2)}" if match else None


def sanitize_origin(origin: str | None) -> str | None:
    if not origin:
        return origin
    if "://" not in origin:
        return origin.split("@", 1)[1] if "@" in origin else origin
    parsed = urlsplit(origin)
    hostname = parsed.hostname or ""
    if parsed.port:
        hostname = f"{hostname}:{parsed.port}"
    # Git remotes do not need query/fragment evidence; both can carry opaque credentials.
    return urlunsplit((parsed.scheme, hostname, parsed.path, "", ""))


def version_surfaces(repo: Path) -> dict[str, str | None]:
    files = {
        "root": repo / "VERSION",
        "backend": repo / "backend" / "VERSION",
        "frontend": repo / "frontend" / "VERSION",
    }
    values = {
        name: path.read_text(encoding="utf-8").strip() if path.is_file() else None
        for name, path in files.items()
    }
    package = repo / "frontend" / "package.json"
    values["frontend_package"] = (
        json.loads(package.read_text(encoding="utf-8")).get("version")
        if package.is_file()
        else None
    )
    return values


def github_runs(
    slug: str | None, head_sha: str, limitations: list[str]
) -> list[dict[str, object]]:
    if not slug:
        limitations.append("origin is not a GitHub repository; CI evidence unavailable")
        return []
    request = urllib.request.Request(
        f"https://api.github.com/repos/{slug}/actions/runs?head_sha={head_sha}&per_page=10",
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "prism-ops-inspect",
        },
    )
    token = os.getenv("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            payload = json.load(response)
    except (OSError, urllib.error.HTTPError, ValueError) as exc:
        limitations.append(f"GitHub Actions evidence unavailable: {type(exc).__name__}")
        return []
    return [
        {
            "name": item.get("name"),
            "id": item.get("id"),
            "event": item.get("event"),
            "head_sha": item.get("head_sha"),
            "head_branch": item.get("head_branch"),
            "status": item.get("status"),
            "conclusion": item.get("conclusion"),
            "url": item.get("html_url"),
        }
        for item in payload.get("workflow_runs", [])
    ]


def remote_snapshot(host: str, services: list[str]) -> dict[str, object]:
    for service in services:
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", service):
            raise SnapshotError(f"invalid service name: {service}")
    command = [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "ConnectTimeout=10",
        host,
        "python3",
        "-",
        *services,
    ]
    raw = run(command, input_text=REMOTE_PROGRAM)
    try:
        return json.loads(raw)
    except ValueError as exc:
        raise SnapshotError("remote snapshot did not return JSON") from exc


def check_snapshot(
    snapshot: dict[str, object], requested_services: list[str]
) -> list[str]:
    failures: list[str] = []
    repository = snapshot.get("repository", {})
    if repository.get("dirty"):
        failures.append("repository worktree is dirty")
    versions = [
        value for value in repository.get("versions", {}).values() if value is not None
    ]
    if versions and len(set(versions)) != 1:
        failures.append("repository version surfaces are not aligned")
    remote = snapshot.get("remote", {})
    services = remote.get("services", {}) if isinstance(remote, dict) else {}
    for service in requested_services:
        item = services.get(service)
        if not isinstance(item, dict) or item.get("error"):
            failures.append(f"{service}: discovery failed")
            continue
        if item.get("app", {}).get("health") != "healthy":
            failures.append(f"{service}: app is not healthy")
        if item.get("postgres", {}).get("health") != "healthy":
            failures.append(f"{service}: postgres is not healthy")
        if not item.get("config", {}).get("sha256"):
            failures.append(f"{service}: config hash unavailable")
        if not item.get("database", {}).get("latest_migration"):
            failures.append(f"{service}: migration history unavailable")
        if item.get("app", {}).get("restarts", 0) != 0:
            failures.append(f"{service}: app restart count is nonzero")
    capy_mode = snapshot.get("deployment_host") == "capy" or any(
        service in {"prism-a", "prism-b"} for service in requested_services
    )
    if capy_mode:
        expected_ports = {"prism-a": "8087", "prism-b": "8088"}
        for service, expected_port in expected_ports.items():
            if service not in requested_services or service not in services:
                continue
            ports = services[service].get("app", {}).get("ports", [])
            if not any(
                item.get("target") == "8080/tcp"
                and item.get("host_port") == expected_port
                for item in ports
            ):
                failures.append(
                    f"{service}: expected public port {expected_port} is missing"
                )
        if "prism-b" in requested_services and "prism-b" in services:
            postgres_ports = services["prism-b"].get("postgres", {}).get("ports", [])
            if not any(
                item.get("target") == "5432/tcp" and item.get("host_port") == "8432"
                for item in postgres_ports
            ):
                failures.append("prism-b: expected PostgreSQL port 8432 is missing")
        if "prism-a" in requested_services and "prism-a" in services:
            postgres_ports = services["prism-a"].get("postgres", {}).get("ports", [])
            if any(item.get("target") == "5432/tcp" for item in postgres_ports):
                failures.append("prism-a: PostgreSQL must remain internal-only")
        comparable = [services.get(name) for name in ("prism-a", "prism-b")]
        if all(isinstance(item, dict) and not item.get("error") for item in comparable):
            image_refs = {item.get("app", {}).get("image_ref") for item in comparable}
            image_ids = {item.get("app", {}).get("image_id") for item in comparable}
            migrations = {
                item.get("database", {}).get("latest_migration") for item in comparable
            }
            if len(image_refs) != 1 or len(image_ids) != 1:
                failures.append("capy: prism-a and prism-b image identity differs")
            if len(migrations) != 1:
                failures.append("capy: prism-a and prism-b migration identity differs")
    return failures


def atomic_write(path: Path, payload: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(payload, encoding="utf-8")
    os.replace(temporary, path)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="capy", help="SSH deployment host")
    parser.add_argument(
        "--service",
        action="append",
        dest="services",
        help="Compose project; repeatable",
    )
    parser.add_argument(
        "--repo-root", type=Path, default=Path.cwd(), help="Prism repository root"
    )
    parser.add_argument(
        "--check", action="store_true", help="exit nonzero when core invariants fail"
    )
    parser.add_argument(
        "--output", type=Path, help="optional retained JSON evidence path"
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    repo = args.repo_root.resolve()
    services = args.services or list(DEFAULT_SERVICES)
    limitations: list[str] = []
    origin = git_value(repo, "remote", "get-url", "origin", optional=True)
    safe_origin = sanitize_origin(origin)
    head = git_value(repo, "rev-parse", "HEAD")
    repository = {
        "root": str(repo),
        "head": head,
        "branch": git_value(repo, "branch", "--show-current", optional=True),
        "dirty": bool(git_value(repo, "status", "--porcelain")),
        "upstream": git_value(
            repo,
            "rev-parse",
            "--abbrev-ref",
            "--symbolic-full-name",
            "@{upstream}",
            optional=True,
        ),
        "origin": safe_origin,
        "tags_at_head": (
            git_value(repo, "tag", "--points-at", "HEAD", optional=True) or ""
        ).splitlines(),
        "versions": version_surfaces(repo),
    }
    snapshot: dict[str, object] = {
        "schema_version": SCHEMA_VERSION,
        "observed_at": utc_now(),
        "deployment_host": args.host,
        "repository": repository,
        "github_runs": github_runs(
            parse_origin_slug(safe_origin), str(head), limitations
        ),
        "remote": remote_snapshot(args.host, services),
        "limitations": limitations,
    }
    snapshot["checks"] = check_snapshot(snapshot, services)
    serialized = (
        json.dumps(snapshot, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    )
    if SECRET_PATTERN.search(serialized):
        # Field names such as limitations are safe; reject value-like secret markers only.
        for line in serialized.splitlines():
            if SECRET_PATTERN.search(line) and not re.search(
                r'"(limitations|checks|config|database)"', line
            ):
                raise SnapshotError("snapshot may contain secret-bearing output")
    if args.output:
        atomic_write(args.output.resolve(), serialized)
    sys.stdout.write(serialized)
    return 1 if args.check and snapshot["checks"] else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SnapshotError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
