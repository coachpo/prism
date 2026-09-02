#!/usr/bin/env python3
"""Plan or execute a Prism release and write an immutable release manifest."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


class ReleaseError(RuntimeError):
    pass


SECRET_PATTERN = re.compile(
    r"(?i)(password|passwd|token|secret|api[-_ ]?key|credential|authorization|cookie|database[_ .-]?url)"
)


def redact(value: str) -> str:
    return "\n".join(
        "<redacted secret-bearing line>" if SECRET_PATTERN.search(line) else line
        for line in value.splitlines()
    )


def run(argv: list[str], *, cwd: Path | None = None) -> str:
    result = subprocess.run(argv, cwd=cwd, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        detail = redact((result.stderr or result.stdout).strip())
        raise ReleaseError(f"command failed ({result.returncode}): {argv[0]}: {detail}")
    return result.stdout.strip()


def parse_release_plan(output: str) -> dict[str, str]:
    fields = {}
    patterns = {
        "current_version": r"^\s*Current version\s*:\s*(\S+)\s*$",
        "version": r"^\s*Target version\s*:\s*(\S+)\s*$",
        "tag": r"^\s*Root tag\s*:\s*(\S+)\s*$",
    }
    for name, pattern in patterns.items():
        match = re.search(pattern, output, re.MULTILINE)
        if not match:
            raise ReleaseError(f"release dry-run did not report {name}")
        fields[name] = match.group(1)
    if fields["tag"] != "v" + fields["version"]:
        raise ReleaseError("release tag/version mismatch")
    return fields


def parse_origin_slug(origin: str) -> str:
    match = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$", origin)
    if not match:
        raise ReleaseError("origin is not a supported GitHub repository")
    return f"{match.group(1)}/{match.group(2)}"


def github_json(url: str) -> dict[str, object]:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "prism-release",
        },
    )
    token = os.getenv("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            value = json.load(response)
    except (OSError, urllib.error.HTTPError, ValueError) as exc:
        raise ReleaseError(f"GitHub API request failed: {type(exc).__name__}") from exc
    if not isinstance(value, dict):
        raise ReleaseError("GitHub API returned a non-object")
    return value


def selected_workflows(
    payload: dict[str, object],
    *,
    release_sha: str | None = None,
    tag: str | None = None,
) -> dict[str, dict[str, object]]:
    selected: dict[str, dict[str, object]] = {}
    for item in payload.get("workflow_runs", []):
        name = item.get("name")
        if release_sha is not None and name in {"CI", "Docker Image"}:
            expected_branch = "main" if name == "CI" else tag
            if (
                item.get("event") != "push"
                or item.get("head_sha") != release_sha
                or item.get("head_branch") != expected_branch
            ):
                continue
        if name in {"CI", "Docker Image"} and name not in selected:
            selected[name] = {
                "name": name,
                "id": item.get("id"),
                "status": item.get("status"),
                "conclusion": item.get("conclusion"),
                "url": item.get("html_url"),
                "event": item.get("event"),
                "head_sha": item.get("head_sha"),
                "head_branch": item.get("head_branch"),
            }
    return selected


def workflow_gate(
    workflows: dict[str, dict[str, object]],
    *,
    release_sha: str | None = None,
    tag: str | None = None,
) -> str:
    if set(workflows) != {"CI", "Docker Image"}:
        return "pending"
    if release_sha is not None:
        expected = {
            "CI": {"event": "push", "head_sha": release_sha, "head_branch": "main"},
            "Docker Image": {
                "event": "push",
                "head_sha": release_sha,
                "head_branch": tag,
            },
        }
        for name, fields in expected.items():
            if any(workflows[name].get(key) != value for key, value in fields.items()):
                return "failed"
    for item in workflows.values():
        if item["status"] == "completed" and item["conclusion"] != "success":
            return "failed"
    if all(
        item["status"] == "completed" and item["conclusion"] == "success"
        for item in workflows.values()
    ):
        return "success"
    return "pending"


def wait_workflows(
    slug: str, release_sha: str, tag: str, timeout: int, poll: int
) -> dict[str, dict[str, object]]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        payload = github_json(
            f"https://api.github.com/repos/{slug}/actions/runs?head_sha={release_sha}&per_page=20"
        )
        workflows = selected_workflows(payload, release_sha=release_sha, tag=tag)
        gate = workflow_gate(workflows, release_sha=release_sha, tag=tag)
        if gate == "success":
            return workflows
        if gate == "failed":
            raise ReleaseError(f"release workflow failed: {workflows}")
        time.sleep(poll)
    raise ReleaseError("timed out waiting for release workflows")


def inspect_image(host: str, image_ref: str) -> dict[str, object]:
    manifest_raw = run(
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
        ]
    )
    image_raw = run(
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
        ]
    )
    try:
        manifest = json.loads(manifest_raw)
        image = json.loads(image_raw)
    except ValueError as exc:
        raise ReleaseError("image inspection returned invalid JSON") from exc
    labels = image.get("config", {}).get("Labels", {})
    return {
        "manifest_digest": manifest.get("digest"),
        "manifest_media_type": manifest.get("mediaType"),
        "os": image.get("os"),
        "architecture": image.get("architecture"),
        "revision": labels.get("org.opencontainers.image.revision"),
        "version": labels.get("org.opencontainers.image.version"),
    }


def normalized_host_arch(value: str) -> str:
    return {"aarch64": "arm64", "x86_64": "amd64"}.get(value.strip(), value.strip())


def version_surfaces(repo: Path) -> dict[str, str]:
    package = json.loads(
        (repo / "frontend" / "package.json").read_text(encoding="utf-8")
    )
    return {
        "root": (repo / "VERSION").read_text(encoding="utf-8").strip(),
        "backend": (repo / "backend" / "VERSION").read_text(encoding="utf-8").strip(),
        "frontend": (repo / "frontend" / "VERSION").read_text(encoding="utf-8").strip(),
        "frontend_package": str(package["version"]),
    }


def write_new(path: Path, value: dict[str, object]) -> None:
    if path.exists():
        raise ReleaseError(f"refusing to overwrite release manifest: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(
        json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    os.replace(temporary, path)


def release_plan(repo: Path, spec: str) -> dict[str, object]:
    helper = repo / "release.sh"
    if not helper.is_file():
        raise ReleaseError("release.sh not found")
    output = run([str(helper), spec, "--dry-run"], cwd=repo)
    plan = parse_release_plan(output)
    plan.update(
        {
            "action": "plan",
            "repo_root": str(repo),
            "head": run(["git", "rev-parse", "HEAD"], cwd=repo),
            "branch": run(["git", "branch", "--show-current"], cwd=repo),
            "dirty": bool(run(["git", "status", "--porcelain"], cwd=repo)),
        }
    )
    return plan


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    subparsers = result.add_subparsers(dest="action", required=True)
    for action in ("plan", "execute"):
        command = subparsers.add_parser(action)
        command.add_argument("--repo-root", type=Path, default=Path.cwd())
        command.add_argument("--spec", required=True)
        command.add_argument(
            "--host", default="capy", help="host used for OCI inspection"
        )
        command.add_argument("--manifest", type=Path)
        command.add_argument("--confirm-release")
        command.add_argument("--timeout-seconds", type=int, default=3600)
        command.add_argument("--poll-seconds", type=int, default=30)
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    repo = args.repo_root.resolve()
    plan = release_plan(repo, args.spec)
    if args.action == "plan":
        print(json.dumps(plan, ensure_ascii=False, indent=2, sort_keys=True))
        return 0
    if args.confirm_release != plan["tag"]:
        raise ReleaseError(f"execute requires --confirm-release {plan['tag']}")
    if plan["dirty"] or plan["branch"] != "main":
        raise ReleaseError("release execution requires a clean main branch")
    head = str(plan["head"])
    upstream = run(["git", "rev-parse", "origin/main"], cwd=repo)
    remote_main = run(
        ["git", "ls-remote", "origin", "refs/heads/main"], cwd=repo
    ).split()[0]
    if head != upstream or head != remote_main:
        raise ReleaseError("local main, origin/main, and remote main are not identical")
    if run(["git", "tag", "-l", str(plan["tag"])], cwd=repo):
        raise ReleaseError("release tag already exists locally")
    if run(
        ["git", "ls-remote", "--tags", "origin", f"refs/tags/{plan['tag']}"], cwd=repo
    ):
        raise ReleaseError("release tag already exists remotely")
    helper = repo / "release.sh"
    run([str(helper), args.spec, "--yes"], cwd=repo)
    release_sha = run(["git", "rev-parse", "HEAD"], cwd=repo)
    if run(["git", "rev-list", "-n1", str(plan["tag"])], cwd=repo) != release_sha:
        raise ReleaseError("release tag does not point to release HEAD")
    if run(["git", "status", "--porcelain"], cwd=repo):
        raise ReleaseError("release helper left a dirty worktree")
    origin = run(["git", "remote", "get-url", "origin"], cwd=repo)
    slug = parse_origin_slug(origin)
    workflows = wait_workflows(
        slug,
        release_sha,
        str(plan["tag"]),
        args.timeout_seconds,
        args.poll_seconds,
    )
    image_repository = f"ghcr.io/{slug.lower()}"
    mutable_ref = f"{image_repository}:{plan['tag']}"
    image = inspect_image(args.host, mutable_ref)
    host_arch = normalized_host_arch(
        run(["ssh", "-o", "BatchMode=yes", args.host, "uname", "-m"])
    )
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", str(image["manifest_digest"])):
        raise ReleaseError("published image has no manifest digest")
    if image["revision"] != release_sha or image["version"] != plan["version"]:
        raise ReleaseError("OCI revision/version does not match the release")
    if image["os"] != "linux" or image["architecture"] != host_arch:
        raise ReleaseError("published image platform does not match deployment host")
    surfaces = version_surfaces(repo)
    if set(surfaces.values()) != {plan["version"]}:
        raise ReleaseError("release version surfaces are not aligned")
    immutable_ref = f"{mutable_ref}@{image['manifest_digest']}"
    manifest = {
        "schema_version": 1,
        "status": "published",
        "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "repository": slug,
        "release_spec": args.spec,
        "version": plan["version"],
        "tag": plan["tag"],
        "release_sha": release_sha,
        "commit_subject": run(["git", "log", "-1", "--pretty=%s"], cwd=repo),
        "version_surfaces": surfaces,
        "workflows": workflows,
        "image": {"repository": image_repository, "ref": immutable_ref, **image},
    }
    manifest_path = args.manifest
    if manifest_path is None:
        stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        manifest_path = (
            repo
            / "artifacts"
            / "evidence"
            / "prism-ops"
            / "releases"
            / f"{stamp}-{plan['tag']}.json"
        )
    write_new(manifest_path.resolve(), manifest)
    print(
        json.dumps(
            {"manifest": str(manifest_path.resolve()), **manifest},
            ensure_ascii=False,
            indent=2,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ReleaseError as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
