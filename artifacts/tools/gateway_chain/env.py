"""Resolved run environment for the live gateway-chain suite.

Every path, port and credential the suite needs is derived here once, so no
other module reaches for os.environ or guesses a repo layout.
"""

from __future__ import annotations

import os
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

# The launcher derives its compose project from the branch name; the suite must
# resolve the identical value or it would talk to a different database.
COMPOSE_PROJECT_PREFIX = "prism-"

DEFAULT_BACKEND_PORT = 8000
DEFAULT_DATABASE_PORT = 15432
LAUNCHER_DATABASE_URL = "postgres://prism:prism@localhost:15432/prism?sslmode=disable"


class EnvError(RuntimeError):
    """Raised when the environment cannot support a live run."""


def _normalize_branch(branch: str) -> str:
    lowered = branch.strip().lower()
    normalized = "".join(character if character.isalnum() or character == "-" else "-" for character in lowered)
    return normalized.strip("-")


def compose_project_for(repo_root: Path) -> str:
    """Mirror start.sh's default_database_compose_project()."""
    override = os.environ.get("PRISM_DATABASE_COMPOSE_PROJECT", "").strip()
    if override:
        return override
    try:
        branch = subprocess.run(
            ["git", "-C", str(repo_root), "rev-parse", "--abbrev-ref", "HEAD"],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        ).stdout
    except (OSError, subprocess.SubprocessError):
        branch = ""
    normalized = _normalize_branch(branch)
    if not normalized or normalized == "head":
        return "prism"
    return f"{COMPOSE_PROJECT_PREFIX}{normalized}"


def _find_repo_root() -> Path:
    override = os.environ.get("PRISM_REPO_ROOT", "").strip()
    if override:
        return Path(override).resolve()
    # env.py lives at <root>/artifacts/tools/gateway_chain/env.py
    return Path(__file__).resolve().parents[3]


@dataclass(frozen=True)
class RunEnv:
    repo_root: Path
    compose_project: str
    compose_file: Path
    config_path: Path
    backend_port: int
    database_port: int
    backend_base: str
    evidence_root: Path
    state_dir: Path
    # Live-upstream inputs. Absent values downgrade cases to BLOCKED, never to
    # a silent pass.
    upstream_api_key: str | None = field(default=None, repr=False)
    upstream_endpoint_id: int | None = None
    live_model: str | None = None
    # Optional remote instance the config is synced from.
    sync_host: str | None = None
    sync_container: str | None = None
    sync_database: str | None = None
    sync_config_path: str | None = None

    @property
    def runtime_base(self) -> str:
        return self.backend_base

    @property
    def management_base(self) -> str:
        return f"{self.backend_base}/api"


def load_env() -> RunEnv:
    repo_root = _find_repo_root()
    if not (repo_root / "start.sh").is_file():
        raise EnvError(f"start.sh not found under {repo_root}; set PRISM_REPO_ROOT")

    backend_port = int(os.environ.get("PRISM_CHAIN_BACKEND_PORT", DEFAULT_BACKEND_PORT))
    database_port = int(os.environ.get("PRISM_CHAIN_DATABASE_PORT", DEFAULT_DATABASE_PORT))
    config_path = Path(os.environ.get("PRISM_CONFIG_PATH", str(repo_root / "config.json")))
    evidence_root = Path(
        os.environ.get("PRISM_CHAIN_EVIDENCE_ROOT", str(repo_root / "artifacts" / "evidence"))
    )
    state_dir = Path(os.environ.get("PRISM_CHAIN_STATE_DIR", str(repo_root / "artifacts" / ".gateway-chain")))

    upstream_key = os.environ.get("PRISM_CHAIN_UPSTREAM_API_KEY", "").strip() or None
    endpoint_id_raw = os.environ.get("PRISM_CHAIN_UPSTREAM_ENDPOINT_ID", "").strip()
    live_model = os.environ.get("PRISM_CHAIN_LIVE_MODEL", "").strip() or None

    return RunEnv(
        repo_root=repo_root,
        compose_project=compose_project_for(repo_root),
        compose_file=repo_root / "docker-compose.yml",
        config_path=config_path,
        backend_port=backend_port,
        database_port=database_port,
        backend_base=f"http://localhost:{backend_port}",
        evidence_root=evidence_root,
        state_dir=state_dir,
        upstream_api_key=upstream_key,
        upstream_endpoint_id=int(endpoint_id_raw) if endpoint_id_raw else None,
        live_model=live_model,
        sync_host=os.environ.get("PRISM_CHAIN_SYNC_HOST", "").strip() or None,
        sync_container=os.environ.get("PRISM_CHAIN_SYNC_CONTAINER", "").strip() or None,
        sync_database=os.environ.get("PRISM_CHAIN_SYNC_DATABASE", "").strip() or None,
        sync_config_path=os.environ.get("PRISM_CHAIN_SYNC_CONFIG_PATH", "").strip() or None,
    )


def secret_values(env: RunEnv, extra: list[str] | None = None) -> list[str]:
    """Every literal that must never reach an evidence file."""
    values = [value for value in [env.upstream_api_key] if value]
    values.extend(value for value in (extra or []) if value)
    return values
