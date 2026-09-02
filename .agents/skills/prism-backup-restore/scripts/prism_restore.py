#!/usr/bin/env python3
"""Plan or execute a side-by-side Prism database restore."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import sys
from pathlib import Path
from urllib.parse import quote, urlsplit, urlunsplit

from prism_backup_common import (
    REMOTE_COMMON,
    OpsError,
    ssh_python,
    validate_database_name,
    validate_service,
)


def rewrite_database_url(value: str, target_database: str) -> str:
    """Replace only the URL path/database name."""
    validate_database_name(target_database)
    parsed = urlsplit(value)
    if not parsed.scheme or not parsed.netloc:
        raise OpsError("database.url is not an absolute URL")
    return urlunsplit(
        (
            parsed.scheme,
            parsed.netloc,
            "/" + quote(target_database, safe=""),
            parsed.query,
            parsed.fragment,
        )
    )


REMOTE_RESTORE = (
    REMOTE_COMMON
    + r"""

def restore_manifest(topology, manifest_value):
    service_root = (topology["backup_root"] / topology["service"]).resolve()
    manifest_source = Path(manifest_value)
    if manifest_source.is_symlink() or not manifest_source.is_file():
        raise RuntimeError("restore manifest is not a regular file")
    if manifest_source.parent.is_symlink():
        raise RuntimeError("restore backup directory may not be a symlink")
    manifest_path = manifest_source.resolve()
    if manifest_path.parent.parent.resolve() != service_root:
        raise RuntimeError("restore manifest must be in a direct managed backup directory")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("schema_version") != 1 or manifest.get("status") != "verified":
        raise RuntimeError("restore manifest is not verified schema version 1")
    if manifest.get("service") != topology["service"]:
        raise RuntimeError("restore manifest belongs to another service")
    backup_dir = manifest_path.parent.resolve()
    artifacts = manifest.get("artifacts")
    if not isinstance(artifacts, dict) or set(artifacts) != {"database.dump", "config.json", "database.list"}:
        raise RuntimeError("restore manifest does not declare the complete artifact set")
    database = manifest.get("database")
    if not isinstance(database, dict) or not isinstance(database.get("schema_versions"), list) or not isinstance(database.get("counts"), dict):
        raise RuntimeError("restore manifest is missing database evidence")
    preflight = manifest.get("preflight")
    if not isinstance(preflight, dict) or set(preflight) != {"path", "sha256"}:
        raise RuntimeError("restore manifest is missing preflight evidence")
    sums_path = backup_dir / "SHA256SUMS"
    if sums_path.is_symlink() or not sums_path.is_file():
        raise RuntimeError("restore backup is missing SHA256SUMS")
    sums = {}
    for line in sums_path.read_text(encoding="utf-8").splitlines():
        if "  " in line:
            digest, name = line.split("  ", 1)
            sums[name] = digest
    for name, evidence in artifacts.items():
        relative = Path(evidence.get("path", ""))
        if len(relative.parts) != 1 or relative.name != evidence.get("path"):
            raise RuntimeError(f"restore artifact must be a direct filename: {name}")
        item_source = backup_dir / relative
        if item_source.is_symlink() or not item_source.is_file():
            raise RuntimeError(f"invalid restore artifact path: {name}")
        item = item_source.resolve()
        if item.parent != backup_dir:
            raise RuntimeError(f"restore artifact escapes backup directory: {name}")
        if file_sha(item) != evidence.get("sha256"):
            raise RuntimeError(f"restore artifact checksum mismatch: {name}")
        if sums.get(evidence.get("path")) != evidence.get("sha256"):
            raise RuntimeError(f"SHA256SUMS mismatch: {name}")
    preflight_relative = Path(preflight["path"])
    if len(preflight_relative.parts) != 1 or preflight_relative.name != preflight["path"]:
        raise RuntimeError("preflight evidence must be a direct filename")
    preflight_source = backup_dir / preflight_relative
    if preflight_source.is_symlink() or not preflight_source.is_file():
        raise RuntimeError("invalid preflight evidence path")
    preflight_path = preflight_source.resolve()
    if preflight_path.parent != backup_dir or file_sha(preflight_path) != preflight["sha256"]:
        raise RuntimeError("preflight evidence checksum mismatch")
    return manifest_path, manifest, backup_dir


def validate_backup_config(path):
    document = json.loads(path.read_text(encoding="utf-8"))
    database = document.get("database")
    if not isinstance(database, dict) or not isinstance(database.get("url"), str):
        raise RuntimeError("backup config is missing database.url")
    parsed = urlsplit(database["url"])
    if not parsed.scheme or not parsed.netloc:
        raise RuntimeError("backup database.url is not absolute")


def rewrite_config(source, destination, target_database):
    document = json.loads(source.read_text(encoding="utf-8"))
    database = document.get("database")
    if not isinstance(database, dict) or not isinstance(database.get("url"), str):
        raise RuntimeError("backup config is missing database.url")
    parsed = urlsplit(database["url"])
    if not parsed.scheme or not parsed.netloc:
        raise RuntimeError("database.url is not absolute")
    database["url"] = urlunsplit((parsed.scheme, parsed.netloc, "/" + quote(target_database, safe=""), parsed.query, parsed.fragment))
    write_atomic(destination, document)


def install_config(source, destination):
    temporary = destination.with_name(f".{destination.name}.{os.getpid()}.restore")
    shutil.copy2(source, temporary)
    os.chmod(temporary, 0o600)
    os.replace(temporary, destination)


def restore_main():
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    target_database = args["target_database"]
    if not re.fullmatch(r"[a-z][a-z0-9_]{0,62}", target_database):
        raise RuntimeError("invalid target database name")
    manifest_path, manifest, backup_dir = restore_manifest(topology, args["manifest"])
    validate_backup_config(backup_dir / manifest["artifacts"]["config.json"]["path"])
    manifest_sha = file_sha(manifest_path)
    expected_token = f"{topology['service']}:{manifest_sha[:12]}"
    desired_image = manifest.get("source_image_ref")
    if not isinstance(desired_image, str) or "@sha256:" not in desired_image:
        raise RuntimeError("manifest source image is not immutable")
    plan = {
        "action": args["action"], "service": topology["service"],
        "manifest": str(manifest_path), "manifest_sha256": manifest_sha,
        "target_database": target_database, "image_ref": desired_image,
        "confirm_restore": expected_token,
        "source_database_preserved": True,
    }
    exists = psql(topology, f"SELECT count(*) FROM pg_database WHERE datname = '{target_database}'", database="postgres")
    if exists != "0":
        raise RuntimeError("target database already exists")
    if args["action"] == "plan":
        print(json.dumps(plan, sort_keys=True))
        return
    if args.get("confirm_restore") != expected_token:
        raise RuntimeError(f"confirmation token must equal {expected_token}")
    current_app = safe_state(inspect(topology["app_id"]))
    if current_app["health"] != "healthy":
        raise RuntimeError("current app must be healthy before side-by-side restore")
    recovery_root = topology["backup_root"] / topology["service"] / "restores"
    recovery_root.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(recovery_root, 0o700)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    recovery_dir = recovery_root / stamp
    recovery_dir.mkdir(mode=0o700)
    original_config = recovery_dir / "original-config.json"
    shutil.copy2(topology["config_path"], original_config)
    os.chmod(original_config, 0o600)
    candidate_config = recovery_dir / "candidate-config.json"
    rewrite_config(backup_dir / manifest["artifacts"]["config.json"]["path"], candidate_config, target_database)
    switched = False
    stopped = False
    stop_attempted = False
    try:
        stop_attempted = True
        stop_app(topology)
        stopped = True
        create_command = f'exec createdb -U "$POSTGRES_USER" -O "$POSTGRES_USER" {target_database}'
        run(["docker", "exec", topology["pg_id"], "sh", "-lc", create_command])
        dump_path = backup_dir / manifest["artifacts"]["database.dump"]["path"]
        restore_command = (
            'exec pg_restore --single-transaction --exit-on-error --no-owner --no-acl '
            f'-U "$POSTGRES_USER" -d {target_database}'
        )
        with dump_path.open("rb") as input_stream:
            run(["docker", "exec", "-i", topology["pg_id"], "sh", "-lc", restore_command], input_stream=input_stream)
        restored = database_evidence(topology, database=target_database)
        expected_database = manifest.get("database", {})
        if restored["schema_versions"] != expected_database.get("schema_versions"):
            raise RuntimeError("restored migration history differs from manifest")
        if manifest.get("evidence_consistency") == "quiesced_exact":
            if restored["counts"] != expected_database.get("counts"):
                raise RuntimeError("restored entity counts differ from exact manifest evidence")
        else:
            for key, minimum in expected_database.get("counts", {}).items():
                if restored["counts"].get(key, -1) < minimum:
                    raise RuntimeError(f"restored entity count is below advisory evidence: {key}")
        install_config(candidate_config, topology["config_path"])
        switched = True
        deploy_image(topology, desired_image)
        expected_version = version_spec(desired_image).split("@", 1)[0].lstrip("v")
        health = wait_health(topology, expected_version=expected_version)
        stopped = False
        evidence = {
            "schema_version": 1, "status": "restored", "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
            "service": topology["service"], "manifest": str(manifest_path), "manifest_sha256": manifest_sha,
            "target_database": target_database, "source_database_preserved": True,
            "original_image_ref": current_app["image_ref"], "restored_image_ref": desired_image,
            "health": health, "recovery_dir": str(recovery_dir),
        }
        write_atomic(recovery_dir / "restore-result.json", evidence)
        print(json.dumps(evidence, sort_keys=True))
    except Exception as primary_error:
        recovery_errors = []
        if switched:
            try:
                stop_app(topology)
            except Exception as error:
                recovery_errors.append(f"stop candidate: {error}")
            try:
                install_config(original_config, topology["config_path"])
            except Exception as error:
                recovery_errors.append(f"restore original config: {error}")
        if stopped or switched or stop_attempted:
            try:
                deploy_image(topology, current_app["image_ref"])
                wait_health(topology)
            except Exception as error:
                recovery_errors.append(f"restart original app: {error}")
        if recovery_errors:
            raise RuntimeError(
                f"restore failed and original app recovery was incomplete: {primary_error}; {'; '.join(recovery_errors)}"
            ) from primary_error
        raise


try:
    restore_main()
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
            "--manifest", required=True, help="absolute remote manifest path"
        )
        command.add_argument("--target-database")
        command.add_argument("--backup-root")
        command.add_argument("--confirm-restore")
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    service = validate_service(args.service)
    if not Path(args.manifest).is_absolute():
        raise OpsError("--manifest must be an absolute remote path")
    if args.backup_root and not Path(args.backup_root).is_absolute():
        raise OpsError("--backup-root must be an absolute remote path")
    if args.action == "execute" and not args.target_database:
        raise OpsError(
            "execute requires an explicit --target-database from the reviewed plan"
        )
    target = args.target_database
    if not target:
        target = (
            "prism_restore_"
            + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ").lower()
        )
    validate_database_name(target)
    result = ssh_python(
        args.host,
        REMOTE_RESTORE,
        {
            "action": args.action,
            "service": service,
            "manifest": args.manifest,
            "target_database": target,
            "backup_root": args.backup_root,
            "confirm_restore": args.confirm_restore,
        },
        timeout=None,
    )
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except OpsError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
