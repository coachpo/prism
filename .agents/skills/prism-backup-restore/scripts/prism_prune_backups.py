#!/usr/bin/env python3
"""Plan or execute strict keep-N retention for managed Prism backups."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from prism_backup_common import REMOTE_COMMON, OpsError, ssh_python, validate_service

REMOTE_PRUNE = (
    REMOTE_COMMON
    + r"""

def checksum_entries(path):
    entries = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if "  " not in line:
            continue
        digest, name = line.split("  ", 1)
        entries[name] = digest
    return entries


def eligible_backups(service_root, service, protected):
    values = []
    if not service_root.is_dir():
        return values
    root_real = service_root.resolve()
    for child in service_root.iterdir():
        if child.is_symlink() or not child.is_dir() or child.resolve().parent != root_real:
            continue
        manifest_path = child / "manifest.json"
        sums_path = child / "SHA256SUMS"
        if not manifest_path.is_file() or manifest_path.is_symlink() or not sums_path.is_file() or sums_path.is_symlink():
            continue
        try:
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            if manifest.get("schema_version") != 1 or manifest.get("status") != "verified" or manifest.get("service") != service:
                continue
            sums = checksum_entries(sums_path)
            artifacts = manifest.get("artifacts", {})
            if not isinstance(artifacts, dict) or set(artifacts) != {
                "database.dump",
                "config.json",
                "database.list",
            }:
                continue
            preflight = manifest.get("preflight")
            if not isinstance(preflight, dict) or set(preflight) != {"path", "sha256"}:
                continue
            valid = True
            for name, evidence in artifacts.items():
                if not isinstance(evidence, dict):
                    valid = False
                    break
                relative = Path(evidence.get("path", ""))
                if len(relative.parts) != 1 or relative.name != evidence.get("path"):
                    valid = False
                    break
                item = child / relative
                if item.parent.resolve() != child.resolve() or item.is_symlink() or not item.is_file():
                    valid = False
                    break
                if sums.get(evidence.get("path")) != evidence.get("sha256"):
                    valid = False
                    break
                if file_sha(item) != evidence.get("sha256"):
                    valid = False
                    break
            if not valid:
                continue
            preflight_relative = Path(preflight.get("path", ""))
            preflight_item = child / preflight_relative
            if (
                len(preflight_relative.parts) != 1
                or preflight_relative.name != preflight.get("path")
                or
                preflight_item.is_symlink()
                or not preflight_item.is_file()
                or preflight_item.resolve().parent != child.resolve()
                or file_sha(preflight_item) != preflight.get("sha256")
            ):
                valid = False
            try:
                config = json.loads((child / artifacts["config.json"]["path"]).read_text(encoding="utf-8"))
                if not isinstance(config, dict) or not isinstance(
                    config.get("database", {}).get("url"), str
                ):
                    valid = False
            except (OSError, ValueError, TypeError):
                valid = False
            if not valid:
                continue
            created = manifest.get("created_at") or child.name
            values.append({"path": child, "created_at": created, "protected": str(child.resolve()) in protected})
        except (OSError, ValueError, TypeError, KeyError, AttributeError):
            continue
    return values


def select_candidates(managed, keep):
    unprotected = [item for item in managed if not item["protected"]]
    retained_paths = {str(item["path"].resolve()) for item in managed[:keep]}
    candidates = [item for item in unprotected if str(item["path"].resolve()) not in retained_paths]
    return retained_paths, candidates


def prune_main():
    args = payload()
    topology = discover(args["service"], args.get("backup_root"))
    service_root = (topology["backup_root"] / topology["service"]).resolve()
    protected = {str(Path(value).resolve()) for value in args.get("protect", [])}
    managed = sorted(eligible_backups(service_root, topology["service"], protected), key=lambda item: item["created_at"], reverse=True)
    retained_paths, candidates = select_candidates(managed, args["keep"])
    result = {
        "action": args["action"], "service": topology["service"], "keep": args["keep"],
        "managed_count": len(managed), "retained": [str(item["path"]) for item in managed if str(item["path"].resolve()) in retained_paths or item["protected"]],
        "candidates": [str(item["path"]) for item in candidates], "deleted": [],
    }
    if args["action"] == "execute":
        expected = f"{topology['service']}:keep-{args['keep']}"
        if args.get("confirm_prune") != expected:
            raise RuntimeError(f"confirmation token must equal {expected}")
        root_real = service_root.resolve()
        refreshed = sorted(eligible_backups(service_root, topology["service"], protected), key=lambda item: item["created_at"], reverse=True)
        _, refreshed_candidates = select_candidates(refreshed, args["keep"])
        if [str(item["path"].resolve()) for item in refreshed_candidates] != [str(item["path"].resolve()) for item in candidates]:
            raise RuntimeError("retention inventory drifted before deletion")
        # Re-resolve each exact, revalidated target immediately before deletion.
        for item in refreshed_candidates:
            target = item["path"]
            if target.is_symlink() or target.resolve().parent != root_real or not target.is_dir():
                raise RuntimeError("retention target drifted before deletion")
            shutil.rmtree(target)
            result["deleted"].append(str(target))
    print(json.dumps(result, sort_keys=True))


try:
    prune_main()
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
        command.add_argument("--backup-root")
        command.add_argument("--keep", type=int, default=3)
        command.add_argument("--protect", action="append", default=[])
        command.add_argument("--confirm-prune")
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    service = validate_service(args.service)
    if args.backup_root and not Path(args.backup_root).is_absolute():
        raise OpsError("--backup-root must be an absolute remote path")
    if args.keep != 3:
        raise OpsError("this project policy requires --keep 3")
    expected = f"{service}:keep-3"
    if args.action == "execute" and args.confirm_prune != expected:
        raise OpsError(f"execute requires --confirm-prune {expected}")
    result = ssh_python(
        args.host,
        REMOTE_PRUNE,
        {
            "action": args.action,
            "service": service,
            "backup_root": args.backup_root,
            "keep": args.keep,
            "protect": args.protect,
            "confirm_prune": args.confirm_prune,
        },
    )
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except OpsError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
