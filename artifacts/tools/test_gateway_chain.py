"""Unit tests for the pure logic inside the live gateway-chain suite.

These never touch Docker, the network, or a running Prism. They cover the parts
that would silently corrupt a live run if they were wrong: secret redaction,
row decoding, outcome classification, and the compose-project derivation that
decides which database the suite reads.

    python3 -m unittest discover -s artifacts/tools -p 'test_gateway_chain.py'
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from gateway_chain import ALL_CASES  # noqa: E402
from gateway_chain.cases_audit import _decode_bytea  # noqa: E402
from gateway_chain.env import _normalize_branch  # noqa: E402
from gateway_chain.runner import (  # noqa: E402
    BLOCKED,
    Blocked,
    Case,
    CaseContext,
    ERRORED,
    FAILED,
    PASSED,
    REDACTION,
    Runner,
    redact,
)
from gateway_chain.store import NULL_MARKER  # noqa: E402


def _context() -> CaseContext:
    return CaseContext(env=None, gateway=None, store=None, harness=None, state={})


def _runner(secrets=None) -> Runner:
    return Runner(_context(), secrets=secrets or [])


class RedactionTest(unittest.TestCase):
    def test_replaces_a_secret_anywhere_in_a_nested_value(self):
        secret = "cp-super-secret-value"
        payload = {
            "headers": [f"Authorization: Bearer {secret}"],
            "nested": {"key": secret, "count": 3},
        }
        cleaned = redact(payload, [secret])
        self.assertNotIn(secret, str(cleaned))
        self.assertIn(REDACTION, cleaned["headers"][0])
        self.assertEqual(REDACTION, cleaned["nested"]["key"])
        self.assertEqual(3, cleaned["nested"]["count"])

    def test_leaves_values_alone_when_there_is_nothing_to_redact(self):
        payload = {"a": ["b", 1, None]}
        self.assertEqual(payload, redact(payload, []))
        self.assertEqual(payload, redact(payload, [""]))

    def test_redacts_every_supplied_secret(self):
        cleaned = redact("one=A two=B", ["A", "B"])
        self.assertEqual(f"one={REDACTION} two={REDACTION}", cleaned)


class ByteaDecodingTest(unittest.TestCase):
    def test_decodes_a_hex_encoded_body(self):
        self.assertEqual("PRISM_OK", _decode_bytea("\\x505249534d5f4f4b"))

    def test_empty_and_null_decode_to_empty_string(self):
        self.assertEqual("", _decode_bytea(None))
        self.assertEqual("", _decode_bytea(""))

    def test_malformed_hex_does_not_raise(self):
        self.assertEqual("", _decode_bytea("\\xZZ"))

    def test_plain_text_passes_through(self):
        self.assertEqual("already text", _decode_bytea("already text"))


class OutcomeClassificationTest(unittest.TestCase):
    """The four outcomes must never collapse into one another."""

    def test_all_checks_true_is_passed(self):
        runner = _runner()
        runner.run([Case("T1", "unit", "ok", lambda context: context.expect("yes", True))])
        self.assertEqual(PASSED, runner.results[0].status)

    def test_one_false_check_is_failed(self):
        def body(context):
            context.expect("first", True)
            context.expect("second", False)

        runner = _runner()
        runner.run([Case("T2", "unit", "mixed", body)])
        self.assertEqual(FAILED, runner.results[0].status)
        self.assertEqual(1, runner.results[0].to_json()["checks_failed"])

    def test_a_missing_precondition_is_blocked_not_passed(self):
        def body(context):
            raise Blocked("no upstream credential")

        runner = _runner()
        runner.run([Case("T3", "unit", "blocked", body)])
        self.assertEqual(BLOCKED, runner.results[0].status)
        self.assertIn("no upstream credential", runner.results[0].failure)

    def test_an_exception_is_errored_not_failed(self):
        def body(context):
            raise RuntimeError("database went away")

        runner = _runner()
        runner.run([Case("T4", "unit", "errored", body)])
        self.assertEqual(ERRORED, runner.results[0].status)
        self.assertIn("database went away", runner.results[0].failure)

    def test_a_case_that_asserts_nothing_is_errored_not_passed(self):
        runner = _runner()
        runner.run([Case("T5", "unit", "vacuous", lambda context: None)])
        self.assertEqual(ERRORED, runner.results[0].status)
        self.assertEqual("case recorded no checks", runner.results[0].failure)

    def test_failure_text_is_redacted(self):
        secret = "cp-leak-me"

        def body(context):
            raise RuntimeError(f"upstream rejected {secret}")

        runner = _runner(secrets=[secret])
        runner.run([Case("T6", "unit", "leaky", body)])
        self.assertNotIn(secret, runner.results[0].failure)

    def test_artifacts_are_redacted(self):
        secret = "cp-leak-me-too"

        def body(context):
            context.record("headers", f"Bearer {secret}")
            context.expect("recorded", True)

        runner = _runner(secrets=[secret])
        runner.run([Case("T7", "unit", "artifact", body)])
        self.assertNotIn(secret, str(runner.results[0].artifacts))

    def test_summary_counts_each_outcome_separately(self):
        def failing(context):
            context.expect("nope", False)

        def blocked(context):
            raise Blocked("missing")

        runner = _runner()
        runner.run(
            [
                Case("S1", "unit", "pass", lambda context: context.expect("ok", True)),
                Case("S2", "unit", "fail", failing),
                Case("S3", "unit", "blocked", blocked),
                Case("S4", "unit", "error", lambda context: (_ for _ in ()).throw(ValueError("x"))),
            ]
        )
        summary = runner.summary()
        self.assertEqual(4, summary["total"])
        self.assertEqual(1, summary[PASSED])
        self.assertEqual(1, summary[FAILED])
        self.assertEqual(1, summary[BLOCKED])
        self.assertEqual(1, summary[ERRORED])

    def test_only_filter_selects_a_subset(self):
        runner = _runner()
        runner.run(
            [
                Case("P1", "unit", "one", lambda context: context.expect("ok", True)),
                Case("P2", "unit", "two", lambda context: context.expect("ok", True)),
            ],
            only=["P2"],
        )
        self.assertEqual(["P2"], [result.id for result in runner.results])


class ComposeProjectTest(unittest.TestCase):
    def test_branch_names_normalize_the_way_start_sh_does(self):
        self.assertEqual("claude-my-branch", _normalize_branch("claude/My_Branch"))
        self.assertEqual("main", _normalize_branch("main"))
        self.assertEqual("feature-abc-123", _normalize_branch("Feature/ABC.123"))

    def test_leading_and_trailing_separators_are_stripped(self):
        self.assertEqual("branch", _normalize_branch("/branch/"))
        self.assertEqual("", _normalize_branch("///"))


class StoreMarkerTest(unittest.TestCase):
    def test_null_marker_survives_an_argv_round_trip(self):
        # A NUL byte cannot be passed to exec, which would break every query.
        self.assertNotIn("\x00", NULL_MARKER)


class CaseMatrixTest(unittest.TestCase):
    def test_case_ids_are_unique(self):
        ids = [case.id for case in ALL_CASES]
        self.assertEqual(len(ids), len(set(ids)), "duplicate case ids would overwrite each other in the report")

    def test_every_chain_link_is_covered(self):
        links = {case.link for case in ALL_CASES}
        self.assertEqual(
            {"launcher", "ingress", "forward", "records", "audit", "readback", "pipeline"}, links
        )

    def test_every_case_has_a_callable_body(self):
        for case in ALL_CASES:
            self.assertTrue(callable(case.body), f"{case.id} has no body")


if __name__ == "__main__":
    unittest.main()
