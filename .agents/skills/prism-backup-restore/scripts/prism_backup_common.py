#!/usr/bin/env python3
"""Shared local and remote helpers for Prism backup automation."""

from __future__ import annotations

import base64
import hashlib
import json
import re
import subprocess
from pathlib import Path

SERVICE_PATTERN = re.compile(r"[a-z0-9][a-z0-9-]{0,62}")
DATABASE_PATTERN = re.compile(r"[a-z][a-z0-9_]{0,62}")
SECRET_PATTERN = re.compile(
    r"(?i)(password|passwd|token|secret|api[-_ ]?key|credential|authorization|cookie|database[_ .-]?url)"
)


class OpsError(RuntimeError):
    pass


def redact(value: str) -> str:
    return "\n".join(
        "<redacted secret-bearing line>" if SECRET_PATTERN.search(line) else line
        for line in value.splitlines()
    )


def validate_service(service: str) -> str:
    if not SERVICE_PATTERN.fullmatch(service):
        raise OpsError(f"invalid service name: {service}")
    return service


def validate_database_name(value: str) -> str:
    if not DATABASE_PATTERN.fullmatch(value):
        raise OpsError(f"invalid target database name: {value}")
    return value


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def manifest_file_sha(path: Path) -> str:
    return sha256_bytes(path.read_bytes())


def encode_payload(payload: dict[str, object]) -> str:
    raw = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return base64.urlsafe_b64encode(raw).decode("ascii")


def ssh_python(
    host: str, program: str, payload: dict[str, object], timeout: int | None = None
) -> dict[str, object]:
    encoded = encode_payload(payload)
    result = subprocess.run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=10",
            host,
            "python3",
            "-",
            encoded,
        ],
        input=program,
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    if result.returncode != 0:
        detail = redact((result.stderr or result.stdout).strip())
        raise OpsError(f"remote operation failed ({result.returncode}): {detail}")
    if SECRET_PATTERN.search(result.stdout):
        raise OpsError("remote operation returned possible secret-bearing output")
    try:
        value = json.loads(result.stdout)
    except ValueError as exc:
        raise OpsError("remote operation did not return JSON") from exc
    if not isinstance(value, dict):
        raise OpsError("remote operation returned a non-object result")
    return value


REMOTE_COMMON = r"""
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
from urllib.parse import quote, urlsplit, urlunsplit


def payload():
    return json.loads(base64.urlsafe_b64decode(sys.argv[1].encode("ascii")))


def run(argv, *, input_stream=None, output_stream=None, env=None):
    result = subprocess.run(
        argv,
        stdin=input_stream,
        stdout=output_stream if output_stream is not None else subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=False,
        env=env,
    )
    if result.returncode != 0:
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(str(item) for item in argv[:4])}")
    if output_stream is not None:
        return ""
    return (result.stdout or b"").decode("utf-8", errors="replace").strip()


def compose_rows():
    raw = run(["docker", "compose", "ls", "--format", "json"])
    if not raw:
        return []
    value = json.loads(raw)
    return value if isinstance(value, list) else [value]


def one_container(project, service):
    raw = run([
        "docker", "ps", "-aq",
        "--filter", f"label=com.docker.compose.project={project}",
        "--filter", f"label=com.docker.compose.service={service}",
    ])
    values = [line for line in raw.splitlines() if line]
    if len(values) != 1:
        raise RuntimeError(f"expected one {project}/{service} container, found {len(values)}")
    return values[0]


def inspect(container):
    return json.loads(run(["docker", "inspect", container]))[0]


def discover(service, backup_root_override=None):
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", service):
        raise RuntimeError("invalid service name")
    matches = [row for row in compose_rows() if row.get("Name") == service]
    if len(matches) != 1:
        raise RuntimeError(f"expected one Compose project {service}, found {len(matches)}")
    configs = [Path(value).resolve() for value in str(matches[0].get("ConfigFiles", "")).split(",") if value]
    if len(configs) != 1 or not configs[0].is_file():
        raise RuntimeError("expected one readable Compose config file")
    config_file = configs[0]
    service_dir = config_file.parent
    deploy_root = service_dir.parent
    env_file = next((path for path in (service_dir / "backend.env", service_dir / ".env") if path.is_file()), None)
    app_id = one_container(service, "prism")
    pg_id = one_container(service, "postgres")
    app_info = inspect(app_id)
    pg_info = inspect(pg_id)
    config_mounts = [item for item in app_info.get("Mounts", []) if item.get("Destination") == "/app/config"]
    if len(config_mounts) != 1:
        raise RuntimeError("expected one /app/config mount")
    config_path = Path(config_mounts[0]["Source"]).resolve() / "config.json"
    if not config_path.is_file():
        raise RuntimeError("Prism config.json is not a regular file")
    if backup_root_override:
        backup_root_value = Path(backup_root_override)
        if not backup_root_value.is_absolute():
            raise RuntimeError("backup root must be absolute")
        backup_root = backup_root_value.resolve()
    else:
        backup_root = (deploy_root / "backups").resolve()
    version_var = re.sub(r"[^A-Z0-9]", "_", service.upper()) + "_VERSION"
    return {
        "service": service,
        "config_file": config_file,
        "service_dir": service_dir,
        "deploy_root": deploy_root,
        "env_file": env_file,
        "app_id": app_id,
        "pg_id": pg_id,
        "app_info": app_info,
        "pg_info": pg_info,
        "config_path": config_path,
        "backup_root": backup_root,
        "version_var": version_var,
    }


def compose_argv(topology, *tail):
    argv = ["docker", "compose"]
    if topology["env_file"]:
        argv += ["--env-file", str(topology["env_file"])]
    argv += ["-f", str(topology["config_file"])]
    argv += list(tail)
    return argv


def psql(topology, sql, *, database=None):
    db_expr = json.dumps(database) if database else '"$POSTGRES_DB"'
    command = (
        'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" '
        f'-d {db_expr} -AtF "|" -qc ' + json.dumps(sql)
    )
    return run(["docker", "exec", topology["pg_id"], "sh", "-lc", command])


def file_sha(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(4 * 1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_atomic(path, data, mode=0o600):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    with temporary.open("w", encoding="utf-8") as stream:
        json.dump(data, stream, ensure_ascii=False, indent=2, sort_keys=True)
        stream.write("\n")
    os.chmod(temporary, mode)
    os.replace(temporary, path)


def safe_state(info):
    state = info.get("State", {})
    health = state.get("Health") or {}
    return {
        "image_ref": info.get("Config", {}).get("Image"),
        "image_id": info.get("Image"),
        "status": state.get("Status"),
        "health": health.get("Status"),
        "restarts": info.get("RestartCount"),
        "started_at": state.get("StartedAt"),
    }


def immutable_image_ref(info):
    configured = info.get("Config", {}).get("Image")
    if not isinstance(configured, str) or not configured:
        raise RuntimeError("container has no configured image reference")
    if "@sha256:" in configured:
        return configured
    image_id = info.get("Image")
    image_info = json.loads(run(["docker", "image", "inspect", image_id]))[0]
    repo_digests = image_info.get("RepoDigests") or []
    repository = configured.rsplit(":", 1)[0] if ":" in configured.rsplit("/", 1)[-1] else configured
    matches = [value for value in repo_digests if value.startswith(repository + "@sha256:")]
    if len(matches) != 1:
        raise RuntimeError("cannot resolve one immutable digest for the configured image")
    return configured + "@" + matches[0].split("@", 1)[1]


def database_evidence(topology, *, database=None):
    versions_raw = psql(topology, "SELECT version FROM prism_schema_migrations ORDER BY version", database=database)
    counts = psql(
        topology,
        "SELECT (SELECT count(*) FROM model_configs),(SELECT count(*) FROM connections),"
        "(SELECT count(*) FROM model_access_targets),(SELECT count(*) FROM request_logs),"
        "(SELECT count(*) FROM usage_request_events)",
        database=database,
    ).split("|")
    name = psql(topology, "SELECT current_database()", database=database)
    size = int(psql(topology, "SELECT pg_database_size(current_database())", database=database))
    historical = {}
    for key, table, column in (
        ("request_upstream_nonnull", "request_logs", "upstream_model_id"),
        ("usage_upstream_nonnull", "usage_request_events", "upstream_model_id"),
        ("request_output_rate_nonnull", "request_logs", "output_rate_state"),
        ("usage_output_rate_nonnull", "usage_request_events", "output_rate_state"),
    ):
        exists = psql(
            topology,
            f"SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='{table}' AND column_name='{column}'",
            database=database,
        )
        historical[key] = (
            int(psql(topology, f"SELECT count(*) FROM {table} WHERE {column} IS NOT NULL", database=database))
            if exists == "1"
            else None
        )
    duplicate_owner_groups = int(
        psql(
            topology,
            "SELECT count(*) FROM (SELECT target_connection_id FROM model_access_targets WHERE target_connection_id IS NOT NULL GROUP BY target_connection_id HAVING count(*) > 1) d",
            database=database,
        )
    )
    orphan_connections = int(
        psql(
            topology,
            "SELECT count(*) FROM connections c LEFT JOIN model_access_targets m ON m.target_connection_id=c.id WHERE m.id IS NULL",
            database=database,
        )
    )
    return {
        "name": name,
        "size_bytes": size,
        "schema_versions": [line for line in versions_raw.splitlines() if line],
        "counts": {
            "models": int(counts[0]), "connections": int(counts[1]),
            "access_targets": int(counts[2]), "request_logs": int(counts[3]),
            "usage_request_events": int(counts[4]),
        },
        "historical_evidence": historical,
        "duplicate_owner_groups": duplicate_owner_groups,
        "orphan_connections": orphan_connections,
    }


def wait_health(topology, expected_version=None, attempts=120, interval=5):
    for _ in range(attempts):
        info = inspect(topology["app_id"])
        state = safe_state(info)
        if state["status"] == "running" and state["health"] == "healthy":
            raw = run(["docker", "exec", topology["app_id"], "curl", "-fsS", "http://127.0.0.1:8080/health"])
            health = json.loads(raw)
            if health.get("status") == "ok" and health.get("startup") == "complete":
                if expected_version is None or health.get("version") == expected_version:
                    return health
        time.sleep(interval)
    raise RuntimeError("Prism health deadline exceeded")


def stop_app(topology):
    run(compose_argv(topology, "stop", "-t", "60", "prism"))


def start_existing_app(topology):
    run(compose_argv(topology, "start", "prism"))
    return wait_health(topology)


def version_spec(image_ref):
    marker = "/prism:"
    if marker not in image_ref:
        raise RuntimeError("image reference is not a tagged Prism image")
    return image_ref.split(marker, 1)[1]


def deploy_image(topology, image_ref):
    env = os.environ.copy()
    env[topology["version_var"]] = version_spec(image_ref)
    run(compose_argv(topology, "pull"), env=env)
    run(compose_argv(topology, "up", "-d", "--remove-orphans"), env=env)
"""
