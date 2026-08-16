"""Lifecycle control for a real Prism stack started by ./start.sh.

The launcher installs a parent-death watchdog and tears the whole stack down
when it exits, so the suite must start it in its own session: a signal aimed at
the caller's process group would otherwise reach the launcher and destroy the
stack mid-run.
"""

from __future__ import annotations

import json
import os
import signal
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path

from . import httpclient
from .env import LAUNCHER_DATABASE_URL, RunEnv


class HarnessError(RuntimeError):
    """The stack could not be brought to the requested state."""


def _run(arguments: list[str], *, timeout: float = 300.0, stdin_path: Path | None = None) -> subprocess.CompletedProcess:
    stdin_handle = stdin_path.open("rb") if stdin_path else None
    try:
        return subprocess.run(
            arguments,
            capture_output=True,
            text=True,
            timeout=timeout,
            stdin=stdin_handle,
            check=False,
        )
    finally:
        if stdin_handle:
            stdin_handle.close()


@dataclass
class Harness:
    env: RunEnv

    # --- configuration sync --------------------------------------------------

    def sync_from_remote(self, *, workdir: Path) -> dict:
        """Copy a live instance's bootstrap config and database into the local stack.

        Only the two values that must be local are rewritten: the database DSN
        the launcher demands, and the CORS origins for the local frontend. Every
        secret — notably runtime.secretEncryptionKey — is carried over verbatim,
        because the endpoint API keys in the dump are encrypted with it.
        """
        if not (self.env.sync_host and self.env.sync_container and self.env.sync_database):
            raise HarnessError("sync requires PRISM_CHAIN_SYNC_HOST, _CONTAINER and _DATABASE")
        workdir.mkdir(parents=True, exist_ok=True)
        host = self.env.sync_host
        container = self.env.sync_container
        database = self.env.sync_database
        config_source = self.env.sync_config_path or "/app/config/config.json"

        config_raw = _run(["ssh", host, f"docker exec {container} cat {config_source}"], timeout=120)
        if config_raw.returncode != 0:
            # The bootstrap file is usually a host bind mount, not inside the
            # container; fall back to reading it from the host filesystem.
            config_raw = _run(["ssh", host, f"cat {config_source}"], timeout=120)
        if config_raw.returncode != 0:
            raise HarnessError(f"could not read remote config: {config_raw.stderr.strip()[:300]}")
        remote_config = json.loads(config_raw.stdout)

        local_config = dict(remote_config)
        local_config.setdefault("database", {})
        local_config["database"] = {**local_config["database"], "url": LAUNCHER_DATABASE_URL}
        local_config["server"] = {"host": "0.0.0.0", "port": self.env.backend_port}
        local_config["http"] = {
            **local_config.get("http", {}),
            "corsAllowedOrigins": ["http://localhost:5173", "http://127.0.0.1:5173"],
        }
        self.env.config_path.write_text(json.dumps(local_config, indent=2) + "\n", encoding="utf-8")

        schema_path = workdir / "remote-schema.sql"
        data_path = workdir / "remote-data.sql"
        for target, flags in ((schema_path, "--schema-only"), (data_path, "--data-only --disable-triggers")):
            dump = _run(
                ["ssh", host, f"docker exec {container} pg_dump -U prism -d {database} --no-owner --no-privileges {flags}"],
                timeout=900,
            )
            if dump.returncode != 0:
                raise HarnessError(f"pg_dump {flags} failed: {dump.stderr.strip()[:300]}")
            target.write_text(dump.stdout, encoding="utf-8")

        # pg_dump hardens search_path to ''. This schema's PL/pgSQL bodies call
        # sibling functions unqualified, so CHECK constraints cannot resolve
        # them during COPY unless the ordinary path is restored.
        patched = data_path.read_text(encoding="utf-8").replace(
            "SELECT pg_catalog.set_config('search_path', '', false);",
            "SELECT pg_catalog.set_config('search_path', 'public', false);",
        )
        data_path.write_text(patched, encoding="utf-8")
        return {
            "config_path": str(self.env.config_path),
            "schema_dump": str(schema_path),
            "data_dump": str(data_path),
            "rewritten": ["database.url", "server", "http.corsAllowedOrigins"],
        }

    def restore_database(self, *, workdir: Path) -> dict:
        """Load the synced dump into a freshly created local database."""
        schema_path = workdir / "remote-schema.sql"
        data_path = workdir / "remote-data.sql"
        for path in (schema_path, data_path):
            if not path.is_file():
                raise HarnessError(f"missing dump {path}; run sync first")

        self.compose(["up", "-d", "postgres"], timeout=300)
        self._wait_for_postgres()
        container = self._postgres_container()

        reset = _run(
            ["docker", "exec", "-i", container, "psql", "-U", "prism", "-d", "prism", "-q", "-c",
             "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"],
            timeout=300,
        )
        if reset.returncode != 0:
            raise HarnessError(f"schema reset failed: {reset.stderr.strip()[:300]}")

        for path in (schema_path, data_path):
            loaded = _run(
                ["docker", "exec", "-i", container, "psql", "-U", "prism", "-d", "prism", "-q", "-v", "ON_ERROR_STOP=1"],
                timeout=900,
                stdin_path=path,
            )
            if loaded.returncode != 0:
                raise HarnessError(f"restore of {path.name} failed: {loaded.stderr.strip()[:500]}")
        return {"restored": [schema_path.name, data_path.name]}

    # --- compose helpers -----------------------------------------------------

    def compose(self, arguments: list[str], *, timeout: float = 300.0) -> subprocess.CompletedProcess:
        return _run(
            ["docker", "compose", "--project-name", self.env.compose_project, "-f", str(self.env.compose_file), *arguments],
            timeout=timeout,
        )

    def _postgres_container(self) -> str:
        result = self.compose(["ps", "-q", "postgres"], timeout=60)
        container = result.stdout.strip().splitlines()
        if not container or not container[0].strip():
            raise HarnessError("postgres container is not running")
        return container[0].strip()

    def _wait_for_postgres(self, *, timeout_s: float = 90.0) -> None:
        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            probe = self.compose(["exec", "-T", "postgres", "pg_isready", "-U", "prism", "-d", "prism"], timeout=30)
            if probe.returncode == 0:
                return
            time.sleep(1.0)
        raise HarnessError(f"postgres was not ready within {timeout_s:.0f}s")

    # --- launcher lifecycle --------------------------------------------------

    @property
    def _pid_file(self) -> Path:
        return self.env.state_dir / "launcher.pid"

    @property
    def launcher_log(self) -> Path:
        return self.env.state_dir / "start.log"

    def effective_startup_settings(self) -> dict[str, str]:
        """Ask the backend binary what the current bootstrap file resolves to.

        This is the same introspection start.sh uses for its launcher contract,
        and it needs no running server. It is NOT read-only, though: with no
        config file present the probe seeds one at 0600 from canonical defaults,
        complete with the dev placeholder secrets. Seeding a throwaway config
        underneath a live instance would be worse than failing, so refuse.
        """
        if not self.env.config_path.is_file():
            raise HarnessError(
                f"{self.env.config_path} does not exist. The effective-settings probe would seed a "
                "default config with placeholder secrets instead of reporting on yours; run 'sync' "
                "or provide a config first."
            )
        binary = self.env.repo_root / "backend" / "prism-backend"
        if not binary.is_file():
            build = _run(["go", "build", "-o", str(binary), "./cmd/prism-backend"], timeout=600)
            if build.returncode != 0:
                raise HarnessError(f"backend build failed: {build.stderr.strip()[:400]}")
        environment = dict(os.environ)
        environment["PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS"] = "1"
        environment["PRISM_CONFIG_PATH"] = str(self.env.config_path)
        result = subprocess.run(
            [str(binary)],
            cwd=str(self.env.repo_root / "backend"),
            capture_output=True,
            text=True,
            timeout=120,
            env=environment,
            check=False,
        )
        if result.returncode != 0:
            raise HarnessError(f"effective-settings probe exited {result.returncode}: {result.stderr.strip()[:300]}")
        settings: dict[str, str] = {}
        for line in result.stdout.splitlines():
            if "=" in line:
                key, _, value = line.partition("=")
                settings[key.strip()] = value.strip()
        return settings

    def start(self, *, mode: str = "headless", ready_timeout_s: float = 180.0) -> dict:
        """Start ./start.sh in its own session and wait for readiness."""
        self.env.state_dir.mkdir(parents=True, exist_ok=True)
        if self.is_running():
            return {"already_running": True, "pid": self.launcher_pid()}

        log_path = self.launcher_log
        log_path.write_text("", encoding="utf-8")
        pid = os.fork()
        if pid == 0:  # pragma: no cover - child never returns
            try:
                os.setsid()
                handle = os.open(str(log_path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o644)
                os.dup2(handle, 1)
                os.dup2(handle, 2)
                os.dup2(os.open(os.devnull, os.O_RDONLY), 0)
                os.chdir(str(self.env.repo_root))
                os.execv("/bin/bash", ["bash", str(self.env.repo_root / "start.sh"), mode])
            except BaseException:
                os._exit(127)
        self._pid_file.write_text(str(pid), encoding="utf-8")

        ready = self.wait_for_ready(timeout_s=ready_timeout_s)
        if not ready:
            tail = log_path.read_text(encoding="utf-8", errors="replace")[-3000:]
            raise HarnessError(f"stack did not become ready within {ready_timeout_s:.0f}s. Launcher log tail:\n{tail}")
        return {"pid": pid, "mode": mode, "log": str(log_path)}

    def wait_for_ready(self, *, timeout_s: float = 180.0) -> bool:
        deadline = time.monotonic() + timeout_s
        gateway = httpclient.Gateway(self.env.runtime_base, self.env.management_base)
        while time.monotonic() < deadline:
            response = gateway.health()
            if response.status == 200:
                payload = response.json()
                if isinstance(payload, dict) and payload.get("readiness") == "ready":
                    return True
            time.sleep(1.0)
        return False

    def launcher_pid(self) -> int | None:
        if not self._pid_file.is_file():
            return None
        try:
            return int(self._pid_file.read_text(encoding="utf-8").strip())
        except ValueError:
            return None

    def is_running(self) -> bool:
        pid = self.launcher_pid()
        if pid is None:
            return False
        try:
            os.kill(pid, 0)
        except OSError:
            return False
        return True

    def stop(self, *, timeout_s: float = 90.0) -> dict:
        """Signal the launcher and confirm it took the whole stack down with it."""
        pid = self.launcher_pid()
        if pid is None:
            return {"stopped": False, "reason": "no launcher pid recorded"}
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            self._pid_file.unlink(missing_ok=True)
            return {"stopped": True, "reason": "launcher already gone"}

        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            try:
                os.kill(pid, 0)
            except OSError:
                break
            time.sleep(0.5)
        else:
            os.kill(pid, signal.SIGKILL)

        self._pid_file.unlink(missing_ok=True)
        leftovers = self.compose(["ps", "-q"], timeout=60).stdout.strip()
        return {"stopped": True, "compose_containers_left": len(leftovers.splitlines()) if leftovers else 0}
