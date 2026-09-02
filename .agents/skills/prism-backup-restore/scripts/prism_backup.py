#!/usr/bin/env python3
"""Plan or execute a verified Prism PostgreSQL/config backup."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

from prism_backup_common import REMOTE_COMMON, OpsError, ssh_python, validate_service

REMOTE_BACKUP = (
    REMOTE_COMMON
    + r"""

def backup_main():
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    app_state = safe_state(inspect(topology["app_id"]))
    source_image_ref = immutable_image_ref(inspect(topology["app_id"]))
    pg_state = safe_state(inspect(topology["pg_id"]))
    if app_state["health"] != "healthy" or pg_state["health"] != "healthy":
        raise RuntimeError("app and postgres must be healthy before backup")
    database = database_evidence(topology)
    if database["duplicate_owner_groups"] != 0 or database["orphan_connections"] != 0:
        raise RuntimeError("owner/orphan invariant failed before backup")
    config_sha = file_sha(topology["config_path"])
    backup_service_root = topology["backup_root"] / topology["service"]
    plan = {
        "action": args["action"],
        "service": topology["service"],
        "mode": args["mode"],
        "compression": args["compression"],
        "backup_root": str(backup_service_root),
        "source_image_ref": source_image_ref,
        "database_size_bytes": database["size_bytes"],
        "config_sha256": config_sha,
        "restart_on_success": args["restart_on_success"],
    }
    if args["action"] == "plan":
        print(json.dumps(plan, sort_keys=True))
        return
    if args.get("confirm_backup") != topology["service"]:
        raise RuntimeError("confirmation token must equal the service name")
    backup_service_root.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(backup_service_root, 0o700)
    free = shutil.disk_usage(backup_service_root).free
    multiplier = 8 if args["compression"] == 0 else 2
    required = database["size_bytes"] * multiplier + 5 * 1024**3
    if free <= required:
        raise RuntimeError(f"insufficient backup capacity: required>{required} free={free}")
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup_dir = backup_service_root / f"{stamp}-managed"
    backup_dir.mkdir(mode=0o700)
    incomplete = backup_dir / ".incomplete"
    incomplete.write_text("backup in progress\n", encoding="utf-8")
    os.chmod(incomplete, 0o600)
    stopped = False
    stop_attempted = False
    try:
        if args["mode"] == "quiesced":
            stop_attempted = True
            stop_app(topology)
            stopped = True
            if safe_state(inspect(topology["pg_id"]))["health"] != "healthy":
                raise RuntimeError("postgres became unhealthy after app stop")
        # Capture the manifest evidence after quiescing so it describes the dump.
        database = database_evidence(topology)
        if database["duplicate_owner_groups"] != 0 or database["orphan_connections"] != 0:
            raise RuntimeError("owner/orphan invariant failed at snapshot boundary")
        config_copy = backup_dir / "config.json"
        shutil.copy2(topology["config_path"], config_copy)
        os.chmod(config_copy, 0o600)
        config_sha = file_sha(config_copy)
        preflight = {
            "schema_version": 1,
            "observed_at": dt.datetime.now(dt.timezone.utc).isoformat(),
            "service": topology["service"],
            "compose_project": topology["service"],
            "compose_file": str(topology["config_file"]),
            "app": app_state,
            "postgres": pg_state,
            "database": database,
            "config_sha256": config_sha,
            "evidence_consistency": (
                "quiesced_exact" if args["mode"] == "quiesced" else "online_advisory"
            ),
        }
        write_atomic(backup_dir / "preflight.json", preflight)
        dump_path = backup_dir / "database.dump"
        command = (
            'exec pg_dump -Fc --compress=' + str(args["compression"]) +
            ' --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
        )
        with dump_path.open("wb") as output:
            run(["docker", "exec", topology["pg_id"], "sh", "-lc", command], output_stream=output)
        os.chmod(dump_path, 0o600)
        if dump_path.stat().st_size == 0:
            raise RuntimeError("pg_dump produced an empty archive")
        list_path = backup_dir / "database.list"
        with dump_path.open("rb") as input_stream, list_path.open("wb") as output_stream:
            run(["docker", "exec", "-i", topology["pg_id"], "pg_restore", "--list"], input_stream=input_stream, output_stream=output_stream)
        os.chmod(list_path, 0o600)
        if list_path.stat().st_size == 0:
            raise RuntimeError("pg_restore --list produced no evidence")
        hashes = {name: file_sha(backup_dir / name) for name in ("database.dump", "config.json", "database.list")}
        sums_path = backup_dir / "SHA256SUMS"
        sums_path.write_text("".join(f"{hashes[name]}  {name}\n" for name in sorted(hashes)), encoding="utf-8")
        os.chmod(sums_path, 0o600)
        for name, expected in hashes.items():
            if file_sha(backup_dir / name) != expected:
                raise RuntimeError(f"independent checksum verification failed: {name}")
        manifest = {
            "schema_version": 1,
            "status": "verified",
            "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
            "service": topology["service"],
            "host": args["host"],
            "backup_mode": args["mode"],
            "compression": args["compression"],
            "source_image_ref": source_image_ref,
            "source_image_id": app_state["image_id"],
            "database": database,
            "config_sha256": config_sha,
            "evidence_consistency": preflight["evidence_consistency"],
            "artifacts": {
                name: {"path": name, "sha256": digest, "size_bytes": (backup_dir / name).stat().st_size}
                for name, digest in hashes.items()
            },
            "preflight": {"path": "preflight.json", "sha256": file_sha(backup_dir / "preflight.json")},
        }
        write_atomic(backup_dir / "manifest.json", manifest)
        incomplete.unlink()
        if stopped and args["restart_on_success"]:
            start_existing_app(topology)
            stopped = False
        result = {"status": "verified", "backup_dir": str(backup_dir), "manifest": str(backup_dir / "manifest.json"), "plan": plan}
        print(json.dumps(result, sort_keys=True))
    except Exception as primary_error:
        recovery_error = None
        if stopped or stop_attempted:
            try:
                start_existing_app(topology)
            except Exception as error:
                recovery_error = error
        if recovery_error is not None:
            raise RuntimeError(
                f"backup failed and original app recovery also failed: {primary_error}; recovery: {recovery_error}"
            ) from primary_error
        raise


try:
    backup_main()
except Exception as exc:
    print(json.dumps({"error": str(exc)}), file=sys.stderr)
    raise SystemExit(1)
"""
)


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    subparsers = result.add_subparsers(dest="action", required=True)
    for action in ("plan", "execute"):
        command = subparsers.add_parser(action)
        command.add_argument("--host", default="capy")
        command.add_argument("--service", required=True)
        command.add_argument(
            "--mode", choices=("quiesced", "online"), default="quiesced"
        )
        command.add_argument("--compression", type=int, choices=range(10), default=6)
        command.add_argument(
            "--backup-root", help="absolute remote backup root override"
        )
        command.add_argument("--restart-on-success", action="store_true")
        command.add_argument("--confirm-backup")
        command.add_argument("--confirm-prune")
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    service = validate_service(args.service)
    if args.backup_root and not Path(args.backup_root).is_absolute():
        raise OpsError("--backup-root must be an absolute remote path")
    if args.action == "execute" and args.confirm_backup != service:
        raise OpsError(f"execute requires --confirm-backup {service}")
    payload = {
        "action": args.action,
        "host": args.host,
        "service": service,
        "mode": args.mode,
        "compression": args.compression,
        "backup_root": args.backup_root,
        "restart_on_success": args.restart_on_success,
        "confirm_backup": args.confirm_backup,
    }
    result = ssh_python(args.host, REMOTE_BACKUP, payload, timeout=None)
    if args.action == "execute" and args.confirm_prune:
        script = Path(__file__).with_name("prism_prune_backups.py")
        command = [
            sys.executable,
            str(script),
            "execute",
            "--host",
            args.host,
            "--service",
            service,
            "--keep",
            "3",
            "--confirm-prune",
            args.confirm_prune,
        ]
        if args.backup_root:
            command += ["--backup-root", args.backup_root]
        subprocess.run(command, check=True)
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OpsError, subprocess.CalledProcessError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
