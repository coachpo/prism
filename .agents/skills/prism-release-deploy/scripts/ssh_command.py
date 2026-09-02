"""Build SSH argv with one safely quoted remote command string."""

from __future__ import annotations

import shlex


def ssh_command(host: str, remote_argv: list[str]) -> list[str]:
    if not host or not remote_argv or any(not value for value in remote_argv):
        raise ValueError("SSH host and remote argv must be non-empty")
    return ["ssh", "-o", "BatchMode=yes", host, shlex.join(remote_argv)]
