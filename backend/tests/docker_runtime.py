from __future__ import annotations

import json
import os
import subprocess
import sys
from collections.abc import MutableMapping
from pathlib import Path
from typing import Callable

_COLIMA_RYUK_SOCKET = "/var/run/docker.sock"


def _is_colima_docker_host(docker_host: str) -> bool:
    return ".colima/" in docker_host


def _read_current_docker_context_host() -> str | None:
    try:
        show = subprocess.run(
            ["docker", "context", "show"],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return None
    if show.returncode != 0:
        return None

    context_name = show.stdout.strip()
    if not context_name:
        return None

    try:
        inspect = subprocess.run(
            ["docker", "context", "inspect", context_name],
            check=False,
            capture_output=True,
            text=True,
        )
    except OSError:
        return None
    if inspect.returncode != 0:
        return None

    try:
        payload = json.loads(inspect.stdout)
    except json.JSONDecodeError:
        return None
    if not isinstance(payload, list) or not payload:
        return None

    docker_endpoint = payload[0].get("Endpoints", {}).get("docker", {})
    host = docker_endpoint.get("Host")
    return host if isinstance(host, str) and host else None


def _find_colima_socket(home: Path | None = None) -> Path | None:
    home_dir = home or Path.home()
    for candidate in (
        home_dir / ".colima" / "default" / "docker.sock",
        home_dir / ".colima" / "docker.sock",
    ):
        if candidate.exists():
            return candidate
    return None


def configure_testcontainers_docker_env(
    *,
    env: MutableMapping[str, str] | None = None,
    current_context_host_reader: Callable[[], str | None] = _read_current_docker_context_host,
    home: Path | None = None,
) -> None:
    target_env = env if env is not None else os.environ
    docker_host = target_env.get("DOCKER_HOST")

    if not docker_host:
        docker_host = current_context_host_reader()
        if not docker_host and sys.platform == "darwin":
            colima_socket = _find_colima_socket(home)
            if colima_socket is not None:
                docker_host = f"unix://{colima_socket}"
        if docker_host:
            target_env["DOCKER_HOST"] = docker_host

    if docker_host and _is_colima_docker_host(docker_host):
        target_env.setdefault("TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE", _COLIMA_RYUK_SOCKET)


__all__ = [
    "configure_testcontainers_docker_env",
]
