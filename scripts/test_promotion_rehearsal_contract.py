#!/usr/bin/env python3
"""
Cross-language exit-code → label contract pin for promotion_rehearsal.sh.

Complementary to test_promotion_rehearsal.sh (which exercises bash helpers in
isolation). This file enforces the contract BETWEEN promotion_rehearsal.sh's
translator functions and the THREE Python decision-grade helpers they invoke:

    forward_paper_resolution.py   ← resolution_tier(exit_code)
    stage_promotion_check.py      ← promote_check_tier(exit_code)
    kill_protocol_check.py        ← kill_check_tier(exit_code)

Each rehearsal translator hardcodes a {exit_code → label → rehearsal_tier}
mapping derived from the Python helpers' documented semantics. The existing
bash test_promotion_rehearsal.sh asserts the translator behaviour in
isolation — it never reads the Python source, so the helpers are free to
rename a label or move a code value without any test firing. The rehearsal
would then silently misclassify:

    Example drift: forward_paper_resolution.py renames "PROMOTE" → "READY"
    (label change only, value unchanged).
      - Python helper still returns exit 1 with verdict "READY"
      - Rehearsal's case still has `1) echo "$TIER_READY"` (correct by luck)
      - But the rehearsal's COMMENT documents `1 PROMOTE → READY` (now stale)
      - Downstream operator confusion (weekly_audit Telegram, dashboards)

    Worse drift: helper moves PROMOTE from exit 1 → exit 4
      - Rehearsal's `1) echo "$TIER_READY"` now triggers on a non-promote path
      - Rehearsal's `4) echo "$TIER_BLOCKED"` blocks legitimate promotions
      - SILENT: bash test still passes (its fixture exit codes match the
        rehearsal's case branches, not the Python helper's actual returns)

Pattern family: writer-equals-fixture drift, third paired implementation
(L2-7 FixtureMsgVsEngineEmitTest + L3 JournalFieldsMatchWriterTest are the
prior two). Here the 'writer' is the Python helper's `Verdict(N, "L", ...)`
or `return "L — ...", N` statement, the 'fixture' is the rehearsal's case
branch (and its accompanying comment), and the 'reader' is whoever consumes
the rehearsal aggregate (weekly_audit tier mapping, the operator).

Run:
  python3 scripts/test_promotion_rehearsal_contract.py
"""
from __future__ import annotations

import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
REHEARSAL = REPO / "scripts" / "promotion_rehearsal.sh"


# Authoritative cross-language contract. If you change a (code, label) pair
# here you MUST also update (a) the Python helper that emits it, AND (b) the
# corresponding rehearsal translator case branch AND its docstring comment.
# The two tests below ensure none of those three sites drift without the
# others.
CONTRACT = {
    "forward_paper_resolution.py": {
        0: "CONTINUE",
        1: "PROMOTE",
        2: "WATCH",
        3: "OPERATOR_REVIEW",
        4: "KILL",
        # 5 = INPUT_ERROR uses a direct `return 5` path with a JSON envelope
        # rather than a Verdict tuple. Pinned in test_resolution_input_error.
    },
    "stage_promotion_check.py": {
        0: "PROMOTE",
        1: "BLOCKED",
        2: "WAITING",
        4: "PROMOTE-CANDIDATE",
        # 3 = ERROR is a direct `return 3` on parse/IO failure. Pinned in
        # test_stage_promotion_error_exit_3.
    },
    "kill_protocol_check.py": {
        0: "CONTINUE",
        1: "KILL",
        2: "WAITING",
        4: "OPERATOR-VERIFY",
        # 3 = ERROR is a direct `return 3`. Pinned in test_kill_protocol_error.
    },
}


class RehearsalExitCodeContractTest(unittest.TestCase):
    """Pin the exit-code → label contract between promotion_rehearsal.sh's
    translator functions and the three Python decision-grade helpers."""

    @classmethod
    def setUpClass(cls):
        if not REHEARSAL.exists():
            raise unittest.SkipTest(f"rehearsal missing: {REHEARSAL}")
        cls.rehearsal_text = REHEARSAL.read_text(encoding="utf-8")
        cls.helper_sources = {}
        for name in CONTRACT:
            path = REPO / "scripts" / name
            if not path.exists():
                raise unittest.SkipTest(f"helper missing: {path}")
            cls.helper_sources[name] = path.read_text(encoding="utf-8")

    # ── forward_paper_resolution.py uses Verdict(N, "LABEL", ...) ──────────

    def test_resolution_verdict_tuples_match_contract(self):
        """forward_paper_resolution.py emits each (code, label) via
        `Verdict(N, "LABEL", ...)`. Each contract pair MUST appear as a
        literal substring."""
        src = self.helper_sources["forward_paper_resolution.py"]
        for code, label in CONTRACT["forward_paper_resolution.py"].items():
            pattern = f'Verdict({code}, "{label}"'
            self.assertIn(
                pattern, src,
                f"forward_paper_resolution.py no longer emits "
                f"`Verdict({code}, \"{label}\", ...)` — rehearsal's "
                f"resolution_tier({code}) would silently misclassify. "
                f"Update CONTRACT, rehearsal, AND helper in lockstep.",
            )

    def test_resolution_input_error_exit_5(self):
        """Exit 5 (INPUT_ERROR) goes through a `return 5` path that prints a
        JSON envelope `{"verdict": "INPUT_ERROR", "exit_code": 5}` — pin
        both the code and the label string."""
        src = self.helper_sources["forward_paper_resolution.py"]
        self.assertIn('return 5', src,
            "forward_paper_resolution.py no longer has `return 5` "
            "(INPUT_ERROR exit code).")
        self.assertIn('"verdict": "INPUT_ERROR"', src,
            "forward_paper_resolution.py no longer emits INPUT_ERROR verdict.")
        self.assertIn('"exit_code": 5', src,
            "INPUT_ERROR code 5 no longer paired with the verdict label.")

    # ── stage_promotion_check.py uses `return "LABEL — ...", N` ────────────

    def _assert_label_code_pair_in_helper(self, helper: str, code: int, label: str):
        """Helpers return tuples like `"LABEL — text", N` or
        `(f"LABEL — text"), N`. Match across an arbitrary number of lines
        (some are wrapped f-strings)."""
        src = self.helper_sources[helper]
        # The label is at the start of the return-tuple string, separated
        # by " — " (em-dash). The code follows the close-quote after
        # arbitrary chars (including newlines for wrapped strings).
        # Tolerate optional `f` prefix and optional wrapping parens.
        pattern = rf'\(?(?:f)?"{re.escape(label)} — .*?\)?,\s*{code}\b'
        match = re.search(pattern, src, re.DOTALL)
        self.assertIsNotNone(
            match,
            f"{helper} no longer pairs label {label!r} with exit code {code} "
            f"in a `return \"{label} — ...\", {code}` statement. Rehearsal's "
            f"translator for ({helper.split('_')[0]}) exit {code} would "
            f"silently misclassify.",
        )

    def test_stage_promotion_verdict_pairs_match_contract(self):
        for code, label in CONTRACT["stage_promotion_check.py"].items():
            self._assert_label_code_pair_in_helper(
                "stage_promotion_check.py", code, label)

    def test_stage_promotion_error_exit_3(self):
        """ERROR (exit 3) is a direct `return 3` on parse/IO failure."""
        src = self.helper_sources["stage_promotion_check.py"]
        self.assertIn('return 3', src,
            "stage_promotion_check.py no longer has `return 3` (ERROR exit).")

    # ── kill_protocol_check.py uses the same pattern ───────────────────────

    def test_kill_protocol_verdict_pairs_match_contract(self):
        for code, label in CONTRACT["kill_protocol_check.py"].items():
            self._assert_label_code_pair_in_helper(
                "kill_protocol_check.py", code, label)

    def test_kill_protocol_error_exit_3(self):
        """ERROR (exit 3) is a direct `return 3`."""
        src = self.helper_sources["kill_protocol_check.py"]
        self.assertIn('return 3', src,
            "kill_protocol_check.py no longer has `return 3` (ERROR exit).")

    # ── Rehearsal documentation mirror ─────────────────────────────────────

    def test_rehearsal_documents_same_contract(self):
        """Belt-and-braces: the rehearsal's translator-function docstrings
        document each (code, label) pair as `{code} {label} → {tier}`. If
        a future edit renames a label in the comments but forgets to update
        either the translator OR the Python helper, this test fires."""
        for helper, mapping in CONTRACT.items():
            for code, label in mapping.items():
                comment_fragment = f"{code} {label}"
                self.assertIn(
                    comment_fragment, self.rehearsal_text,
                    f"promotion_rehearsal.sh no longer documents "
                    f"`{code} {label}` (for {helper}). The translator's "
                    f"docstring comment AND its case branch AND the Python "
                    f"helper AND CONTRACT must update in lockstep.",
                )

    def test_rehearsal_documents_error_codes(self):
        """The direct-`return 3`/`return 5` paths also appear in the
        rehearsal comments (exit 5 = INPUT_ERROR for resolution; exit 3 =
        ERROR for stage_promotion + kill_protocol)."""
        self.assertIn("5 INPUT_ERROR", self.rehearsal_text,
            "rehearsal no longer documents `5 INPUT_ERROR` for resolution.py")
        self.assertIn("3 ERROR", self.rehearsal_text,
            "rehearsal no longer documents `3 ERROR` for promote/kill helpers")


if __name__ == "__main__":
    unittest.main(verbosity=2)
