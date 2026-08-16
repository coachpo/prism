"""Case model, execution, and redacted evidence output.

A case reports one of four outcomes and they are never collapsed into each
other: PASSED, FAILED (an assertion was checked and did not hold), BLOCKED (a
precondition the suite cannot supply itself is missing), and ERRORED (the case
could not be evaluated at all). A run that cannot read the database reports
ERRORED, never an empty PASSED.
"""

from __future__ import annotations

import json
import time
import traceback
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable

PASSED = "passed"
FAILED = "failed"
BLOCKED = "blocked"
ERRORED = "errored"

REDACTION = "[REDACTED]"


class Blocked(Exception):
    """Raised by a case whose precondition is absent."""


@dataclass
class Check:
    description: str
    ok: bool
    expected: object = None
    actual: object = None

    def to_json(self) -> dict:
        payload: dict = {"description": self.description, "ok": self.ok}
        if self.expected is not None or self.actual is not None:
            payload["expected"] = self.expected
            payload["actual"] = self.actual
        return payload


@dataclass
class CaseContext:
    """What a case body is handed, and where it records what it observed."""

    env: object
    gateway: object
    store: object
    harness: object
    state: dict
    checks: list[Check] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)
    artifacts: dict[str, object] = field(default_factory=dict)

    def expect(self, description: str, ok: bool, *, expected: object = None, actual: object = None) -> bool:
        self.checks.append(Check(description=description, ok=bool(ok), expected=expected, actual=actual))
        return bool(ok)

    def expect_eq(self, description: str, expected: object, actual: object) -> bool:
        return self.expect(description, expected == actual, expected=expected, actual=actual)

    def note(self, message: str) -> None:
        self.notes.append(message)

    def record(self, name: str, value: object) -> None:
        self.artifacts[name] = value

    def block(self, reason: str) -> None:
        raise Blocked(reason)


@dataclass
class Case:
    id: str
    link: str
    title: str
    body: Callable[[CaseContext], None]
    requires_live_upstream: bool = False


@dataclass
class CaseResult:
    id: str
    link: str
    title: str
    status: str
    duration_s: float
    checks: list[Check]
    notes: list[str]
    artifacts: dict
    failure: str | None = None

    def to_json(self) -> dict:
        return {
            "id": self.id,
            "link": self.link,
            "title": self.title,
            "status": self.status,
            "duration_s": round(self.duration_s, 3),
            "checks_total": len(self.checks),
            "checks_failed": sum(1 for check in self.checks if not check.ok),
            "checks": [check.to_json() for check in self.checks],
            "notes": self.notes,
            "artifacts": self.artifacts,
            "failure": self.failure,
        }


def redact(value: object, secrets: list[str]) -> object:
    """Replace every known secret literal anywhere in a JSON-shaped value."""
    live = [secret for secret in secrets if secret]
    if not live:
        return value
    if isinstance(value, str):
        result = value
        for secret in live:
            if secret in result:
                result = result.replace(secret, REDACTION)
        return result
    if isinstance(value, dict):
        return {key: redact(item, live) for key, item in value.items()}
    if isinstance(value, list):
        return [redact(item, live) for item in value]
    return value


class Runner:
    def __init__(
        self,
        context: CaseContext,
        secrets: list[str],
        *,
        evidence_dir: Path | None = None,
        sentinel: Callable[[], str | None] | None = None,
    ):
        self.context = context
        self.secrets = secrets
        self.evidence_dir = evidence_dir
        # A sentinel names an environment-level fault that would make every
        # later case fail for the same reason. Reporting twenty derived
        # failures hides the one cause, so a tripped sentinel short-circuits
        # the case with that cause instead of running it.
        self.sentinel = sentinel
        self.results: list[CaseResult] = []
        self.sentinel_trips: list[str] = []

    def run(self, cases: list[Case], *, only: list[str] | None = None) -> list[CaseResult]:
        for case in cases:
            if only and case.id not in only:
                continue
            fault = self._check_sentinel(case)
            if fault is not None:
                self.results.append(self._short_circuit(case, fault))
                continue
            self.results.append(self._run_one(case))
        return self.results

    def _check_sentinel(self, case: Case) -> str | None:
        if self.sentinel is None or case.link == "pipeline":
            # The pipeline cases exist to diagnose exactly this fault, so they
            # must still run when it is present.
            return None
        try:
            return self.sentinel()
        except Exception as error:  # noqa: BLE001
            return f"sentinel could not be evaluated: {type(error).__name__}: {error}"

    def _short_circuit(self, case: Case, fault: str) -> CaseResult:
        if fault not in self.sentinel_trips:
            self.sentinel_trips.append(fault)
        result = CaseResult(
            id=case.id,
            link=case.link,
            title=case.title,
            status=ERRORED,
            duration_s=0.0,
            checks=[],
            notes=["not evaluated: an environment-level fault would make this case fail for an unrelated reason"],
            artifacts={},
            failure=redact(fault, self.secrets),
        )
        print(f"[{ERRORED.upper():7}] {case.id}  {case.title}\n           -> {fault}", flush=True)
        return result

    def _run_one(self, case: Case) -> CaseResult:
        self.context.checks = []
        self.context.notes = []
        self.context.artifacts = {}
        started = time.monotonic()
        status = PASSED
        failure: str | None = None
        try:
            case.body(self.context)
            if any(not check.ok for check in self.context.checks):
                status = FAILED
            elif not self.context.checks:
                # A case that asserted nothing has not verified anything.
                status = ERRORED
                failure = "case recorded no checks"
        except Blocked as blocked:
            status = BLOCKED
            failure = str(blocked)
        except Exception as error:  # noqa: BLE001 - a case must never abort the run
            status = ERRORED
            failure = f"{type(error).__name__}: {error}\n{traceback.format_exc(limit=6)}"

        result = CaseResult(
            id=case.id,
            link=case.link,
            title=case.title,
            status=status,
            duration_s=time.monotonic() - started,
            checks=list(self.context.checks),
            notes=list(self.context.notes),
            artifacts=redact(dict(self.context.artifacts), self.secrets),
            failure=redact(failure, self.secrets) if failure else None,
        )
        line = f"[{result.status.upper():7}] {result.id}  {result.title}"
        if result.status in (FAILED, ERRORED, BLOCKED) and result.failure:
            line += f"\n           -> {result.failure.splitlines()[0]}"
        for check in result.checks:
            if not check.ok:
                line += f"\n           x {check.description} (expected={check.expected!r} actual={check.actual!r})"
        print(line, flush=True)
        return result

    def summary(self) -> dict:
        tally = {PASSED: 0, FAILED: 0, BLOCKED: 0, ERRORED: 0}
        for result in self.results:
            tally[result.status] = tally.get(result.status, 0) + 1
        return {
            "total": len(self.results),
            **tally,
            "generated_at": datetime.now(timezone.utc).isoformat(),
        }

    def write_evidence(self, run_id: str, extra: dict | None = None) -> Path | None:
        if self.evidence_dir is None:
            return None
        self.evidence_dir.mkdir(parents=True, exist_ok=True)
        report = {
            "run_id": run_id,
            "summary": self.summary(),
            "environment_faults": self.sentinel_trips,
            "environment": redact(extra or {}, self.secrets),
            "cases": [result.to_json() for result in self.results],
        }
        target = self.evidence_dir / "report.json"
        # Assertions compare whatever shape is natural (sets of missing names,
        # tuples of two states). Losing a whole run's evidence to an encoder
        # error would be far worse than rendering those as lists.
        def encode(value: object) -> object:
            if isinstance(value, (set, frozenset)):
                return sorted(str(item) for item in value)
            if isinstance(value, (bytes, bytearray)):
                return value.decode("utf-8", errors="replace")
            return str(value)

        target.write_text(
            json.dumps(report, indent=2, ensure_ascii=False, default=encode) + "\n", encoding="utf-8"
        )
        return target
