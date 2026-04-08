from __future__ import annotations

from pathlib import Path

from tests.docker_runtime import configure_testcontainers_docker_env


def test_sets_colima_env_from_current_context() -> None:
    env: dict[str, str] = {}

    configure_testcontainers_docker_env(
        env=env,
        current_context_host_reader=lambda: "unix:///Users/tester/.colima/default/docker.sock",
        home=Path("/Users/tester"),
    )

    assert env["DOCKER_HOST"] == "unix:///Users/tester/.colima/default/docker.sock"
    assert env["TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE"] == "/var/run/docker.sock"


def test_falls_back_to_colima_socket_when_context_host_missing(tmp_path: Path) -> None:
    env: dict[str, str] = {}
    socket_path = tmp_path / ".colima" / "default" / "docker.sock"
    socket_path.parent.mkdir(parents=True, exist_ok=True)
    socket_path.touch()

    configure_testcontainers_docker_env(
        env=env,
        current_context_host_reader=lambda: None,
        home=tmp_path,
    )

    assert env["DOCKER_HOST"] == f"unix://{socket_path}"
    assert env["TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE"] == "/var/run/docker.sock"
