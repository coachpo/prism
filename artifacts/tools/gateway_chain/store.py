"""Read-side access to the live Prism PostgreSQL database.

Queries run through `docker exec ... psql` against the container the launcher
started, so the suite needs no database driver installed. Every helper returns
plain Python values and raises StoreUnavailable rather than returning an empty
result when the database itself cannot be reached — a read failure must never
be mistaken for "no rows".
"""

from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass

from .env import RunEnv

# Both markers must survive an argv round trip, so no NUL bytes here, and both
# must be improbable enough that a real value never collides with them.
FIELD_SEPARATOR = "\x1f"
NULL_MARKER = "\x1e<NULL>\x1e"


class StoreUnavailable(RuntimeError):
    """The database could not be reached or the query failed to execute."""


@dataclass
class Store:
    env: RunEnv
    _container_id: str | None = None

    def container_id(self) -> str:
        if self._container_id:
            return self._container_id
        result = subprocess.run(
            [
                "docker",
                "compose",
                "--project-name",
                self.env.compose_project,
                "-f",
                str(self.env.compose_file),
                "ps",
                "-q",
                "postgres",
            ],
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
        container = result.stdout.strip().splitlines()
        if not container or not container[0].strip():
            raise StoreUnavailable(
                f"no postgres container for compose project {self.env.compose_project}"
            )
        self._container_id = container[0].strip()
        return self._container_id

    def _psql(self, sql: str, *, tuples_only: bool) -> str:
        arguments = ["docker", "exec", "-i", self.container_id(), "psql", "-U", "prism", "-d", "prism", "-v", "ON_ERROR_STOP=1"]
        if tuples_only:
            arguments += ["-t", "-A", "-F", FIELD_SEPARATOR]
        arguments += ["-c", sql]
        try:
            result = subprocess.run(arguments, capture_output=True, text=True, timeout=120, check=False)
        except (OSError, subprocess.SubprocessError) as error:
            raise StoreUnavailable(f"psql invocation failed: {error}") from error
        if result.returncode != 0:
            raise StoreUnavailable(f"psql exited {result.returncode}: {result.stderr.strip()[:500]}")
        return result.stdout

    def rows(self, sql: str, columns: list[str]) -> list[dict[str, str | None]]:
        """Run a SELECT and return one dict per row, NULLs preserved as None."""
        wrapped = (
            "select "
            + ", ".join(f"coalesce(({column})::text, '{NULL_MARKER}')" for column in columns)
            + f" from ({sql}) as chain_query"
        )
        output = self._psql(wrapped, tuples_only=True)
        parsed: list[dict[str, str | None]] = []
        for line in output.splitlines():
            if not line.strip():
                continue
            fields = line.split(FIELD_SEPARATOR)
            if len(fields) != len(columns):
                raise StoreUnavailable(
                    f"expected {len(columns)} fields, got {len(fields)} in row: {line[:200]!r}"
                )
            parsed.append(
                {column: (None if value == NULL_MARKER else value) for column, value in zip(columns, fields)}
            )
        return parsed

    def scalar(self, sql: str) -> str | None:
        result = self.rows(sql, ["value"])
        if not result:
            return None
        return result[0]["value"]

    def count(self, table: str, where: str = "true") -> int:
        value = self.scalar(f"select count(*) as value from {table} where {where}")
        if value is None:
            raise StoreUnavailable(f"count on {table} returned no row")
        return int(value)

    def json_rows(self, sql: str) -> list[dict]:
        """Return rows as JSON objects, for wide tables where naming every column is noise."""
        wrapped = f"select coalesce(json_agg(row_to_json(chain_query))::text, '[]') from ({sql}) as chain_query"
        output = self._psql(wrapped, tuples_only=True).strip()
        if not output:
            return []
        try:
            return json.loads(output)
        except json.JSONDecodeError as error:
            raise StoreUnavailable(f"could not decode json rows: {error}") from error

    def max_id(self, table: str) -> int:
        """Watermark used to correlate new rows with the request that produced them."""
        value = self.scalar(f"select coalesce(max(id), 0) as value from {table}")
        if value is None:
            raise StoreUnavailable(f"max(id) on {table} returned no row")
        return int(value)

    def watermarks(self) -> dict[str, int]:
        return {
            "request_logs": self.max_id("request_logs"),
            "usage_request_events": self.max_id("usage_request_events"),
            "audit_logs": self.max_id("audit_logs"),
            "loadbalance_events": self.max_id("loadbalance_events"),
        }

    def counts_since(self, watermarks: dict[str, int]) -> dict[str, int]:
        return {
            table: self.count(table, f"id > {int(watermark)}")
            for table, watermark in watermarks.items()
        }

    def discard_outbox_rows_after(self, after_id: int) -> int:
        """Drop outbox rows a case knowingly wedged, so one defect cannot void the run.

        This is the only write the suite performs against the database. It
        exists because a row the materializer can never insert blocks every
        later row: leaving it in place would turn one real finding into a wall
        of unrelated failures. Callers must record that they used it.
        """
        output = self._psql(f"DELETE FROM runtime_telemetry_outbox WHERE id > {int(after_id)}", tuples_only=True)
        for token in output.split():
            if token.isdigit():
                return int(token)
        return 0

    def is_reachable(self) -> bool:
        try:
            self.scalar("select 1 as value")
        except StoreUnavailable:
            return False
        return True
