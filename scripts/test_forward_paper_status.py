#!/usr/bin/env python3
"""
Functional tests for forward_paper_status.sh.

Drives the bash script in `local` mode via subprocess with a constructed
JOURNAL_DIR so the audit-pattern fixes (typo detection, JOURNAL_DIR
override honoring, helper-output validation, top_syms abs-PnL sort) all
have regression coverage. Avoids ssh + the Binance price-fetch path by
using local mode and zero open positions.

Run:
  python3 scripts/test_forward_paper_status.py
"""
from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "forward_paper_status.sh"


def write_close(jdir: Path, symbol: str, ts: str, pnl: float,
                outcome: str = "STOP",
                fee: float = 100.0, slip: float = 50.0,
                notional: float = 100000.0,
                cohort: str = "live") -> None:
    """Append a paired open + close to the cohort's journal file. Designed
    to produce zero open positions (one open + one close per pair) so the
    test path doesn't hit the Binance price-fetch network call."""
    if cohort == "live":
        target = jdir
    else:
        target = jdir / cohort
    target.mkdir(parents=True, exist_ok=True)
    ym = ts[:7]  # YYYY-MM
    path = target / f"{symbol}-{ym}.jsonl"
    open_evt = json.dumps({
        "event": "open", "symbol": symbol,
        "ts": ts.replace("close", "open"),
        "side": "LONG", "entry": 100, "stop": 99, "target": 106,
    })
    close_evt = json.dumps({
        "event": "close", "symbol": symbol, "ts": ts,
        "side": "LONG", "entry": 100, "exit": 99,
        "outcome": outcome, "pnl_usd": pnl,
        "fee_usd": fee, "slip_usd": slip, "notional_usd": notional,
    })
    with path.open("a") as f:
        f.write(open_evt + "\n")
        f.write(close_evt + "\n")


def run_script(journal_dir: str, host: str = "local",
               extra_env: dict | None = None) -> tuple[int, str, str]:
    env = os.environ.copy()
    env["JOURNAL_DIR"] = journal_dir
    if extra_env:
        env.update(extra_env)
    result = subprocess.run(
        ["bash", str(SCRIPT), host],
        capture_output=True, text=True, env=env,
    )
    return result.returncode, result.stdout, result.stderr


class TypoGuardTest(unittest.TestCase):

    def test_missing_journal_dir_exits_2(self):
        # Audit-pattern regression: a typo'd JOURNAL_DIR used to silently
        # render "(no data yet)" — indistinguishable from a legitimate
        # fresh-deploy state. The fix detects the missing dir and exits 2
        # with a stderr message naming the path + remediation.
        code, out, err = run_script("/tmp/definitely-not-a-real-path-xyz123")
        self.assertEqual(code, 2,
            f"expected exit 2 (typo'd dir), got {code}\nstdout:\n{out}\nstderr:\n{err}")
        self.assertIn("not found", err)
        self.assertIn("operator misconfiguration", err)
        self.assertIn("definitely-not-a-real-path-xyz123", err)

    def test_empty_journal_dir_does_not_falsely_typo(self):
        # An empty directory IS a legitimate fresh-deploy state. Must NOT
        # be confused with a typo (which is "directory missing entirely").
        with tempfile.TemporaryDirectory() as tmp:
            code, out, err = run_script(tmp)
            self.assertNotEqual(code, 2,
                f"empty dir should NOT trip the typo guard\nstderr:\n{err}")
            # Empty dir → live cohort emits NODATA → script renders
            # "(no data yet)" line and exits 0.
            self.assertIn("(no data yet)", out)


class JournalDirOverrideTest(unittest.TestCase):

    def test_local_journal_dir_override_renders_synthetic_data(self):
        # Verify JOURNAL_DIR override is actually consumed in local mode.
        # If the override were ignored (the previous remote-mode bug
        # mirrored locally), the synthetic XLMUSDT close would not appear
        # in the rendered output.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "XLMUSDT", "2026-05-08T08:31:59Z", -1306.45)
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0,
                f"expected exit 0, got {code}\nstderr:\n{err}")
            self.assertIn("XLMUSDT", out,
                "synthetic close was not parsed — JOURNAL_DIR override "
                "may not be honored")
            self.assertIn("Trades closed:          1", out)


class TopSymsSortTest(unittest.TestCase):

    def test_top_syms_sorted_by_absolute_pnl(self):
        # Audit-pattern regression: previously sort -gr ranked by signed
        # PnL, so in mixed cohorts (winners + losers) the biggest losers
        # were hidden behind the biggest winners. The fix sorts by abs
        # PnL — consistent with the single_sym_pct gate.
        # Construct a mixed cohort: 1 big winner (+5000) + 1 big loser
        # (-9000). Expected: big loser appears first in top_syms.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "WINNERUSDT", "2026-05-08T08:00:00Z",
                        +5000.0, outcome="TARGET", slip=0.0)
            write_close(d, "LOSERUSDT",  "2026-05-08T09:00:00Z",
                        -9000.0, outcome="STOP")
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0, f"stderr:\n{err}")
            # Find the "Top symbols:" line and check ordering.
            top_line = next((ln for ln in out.splitlines()
                             if "Top symbols:" in ln), None)
            self.assertIsNotNone(top_line, f"no Top symbols line in:\n{out}")
            loser_pos = top_line.find("LOSERUSDT")
            winner_pos = top_line.find("WINNERUSDT")
            self.assertGreaterEqual(loser_pos, 0,
                f"LOSERUSDT missing from top: {top_line}")
            self.assertGreaterEqual(winner_pos, 0,
                f"WINNERUSDT missing from top: {top_line}")
            self.assertLess(loser_pos, winner_pos,
                "biggest absolute contributor (LOSERUSDT $9k) must "
                f"appear before WINNERUSDT $5k. Got: {top_line!r}")


class PartialCanonD3Test(unittest.TestCase):
    """D3 resolution pin (2026-05-12 — see
    results/partial_canon_resolution_2026-05-12.md): dashboard must count
    only TERMINAL closes toward 'Trades closed' and 'wins'. PARTIAL
    closes are reported in a separate line below the trades count when
    nonzero, never silently rolled into the gate-aligned total.

    Today (live config = single 6:1 RR, no B2/multi-TP) PARTIAL emission
    is zero so the behavior is invisible. This test exercises the latent
    code path by injecting fixture PARTIAL events directly. Without these
    pins, a future re-enable of B2/multi-TP would silently inflate the
    dashboard's trade count above the formal gate."""

    def _write_partial(self, jdir: Path, symbol: str, ts: str, pnl: float,
                       cohort: str = "live") -> None:
        """Emit a PARTIAL close event WITHOUT a paired open. write_close
        in this file emits open+close pairs; PARTIAL closes happen
        mid-position so they appear without their own paired open."""
        if cohort == "live":
            target = jdir
        else:
            target = jdir / cohort
        target.mkdir(parents=True, exist_ok=True)
        ym = ts[:7]
        path = target / f"{symbol}-{ym}.jsonl"
        close_evt = json.dumps({
            "event": "close", "symbol": symbol, "ts": ts,
            "side": "LONG", "entry": 100, "exit": 105,
            "outcome": "PARTIAL", "pnl_usd": pnl,
            "fee_usd": 50.0, "slip_usd": 0.0, "notional_usd": 50000.0,
        })
        with path.open("a") as f:
            f.write(close_evt + "\n")

    def test_partial_closes_excluded_from_trade_count(self):
        """Fixture: 2 TARGET + 1 STOP + 2 PARTIAL → trades=3, wins=2,
        partials reported separately. Pre-D3 the dashboard would have
        reported trades=5 with PARTIAL counted as wins (overstating
        both count + WR)."""
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            # 2 terminals TARGET
            write_close(d, "BTCUSDT", "2026-05-08T01:00:00Z", +100.0,
                        outcome="TARGET", slip=0.0)
            write_close(d, "ETHUSDT", "2026-05-08T02:00:00Z", +200.0,
                        outcome="TARGET", slip=0.0)
            # 1 terminal STOP
            write_close(d, "BNBUSDT", "2026-05-08T03:00:00Z", -100.0,
                        outcome="STOP")
            # 2 PARTIAL closes — no paired open (mid-position scale-outs)
            self._write_partial(d, "SOLUSDT", "2026-05-08T04:00:00Z", +50.0)
            self._write_partial(d, "SOLUSDT", "2026-05-08T05:00:00Z", +30.0)
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0, f"stderr:\n{err}")
            # Trades count must be 3 (terminals only), NOT 5.
            self.assertIn("Trades closed:          3", out,
                "Trades count must equal terminal closes only (3), not "
                "all closes (5). Pre-D3 the dashboard counted PARTIAL "
                "as a trade — inflating both count and WR.")
            # Partial-closes notice must appear (2 partials).
            self.assertIn("partial closes", out,
                "Partial count must surface when nonzero")
            self.assertIn("(+ 2 partial closes", out,
                f"Expected '(+ 2 partial closes' indicator in output:\n{out}")

    def test_no_partial_line_when_zero(self):
        """Confirm the partials notice is suppressed when no PARTIAL events.
        Pre-D3 there was no notice at all; D3 must not introduce a new
        noisy line in the default no-PARTIAL state.

        Anchors on the specific D3 marker ('(+ N partial closes;'), not
        the generic phrase 'partial closes' which already appears in the
        Notes section of the dashboard."""
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "BTCUSDT", "2026-05-08T01:00:00Z", +100.0,
                        outcome="TARGET", slip=0.0)
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0, f"stderr:\n{err}")
            self.assertNotIn("(+ ", out,
                "D3 partial-count line must NOT appear when partials=0 — "
                "would clutter the steady-state dashboard")
            self.assertNotIn("partial closes; not counted in trades/wins", out,
                "D3 partial-count notice must be suppressed when partials=0")

    def test_partial_does_not_count_as_win(self):
        """Direct WR pin: 1 TARGET + 3 PARTIAL → WR = 100% (1/1 of
        terminals), NOT 100% (4/4 of all closes including partials).
        Pre-D3 PARTIAL was counted as a win → WR overstated whenever
        terminals were a mix and PARTIAL was on."""
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "BTCUSDT", "2026-05-08T01:00:00Z", +200.0,
                        outcome="TARGET", slip=0.0)
            # Same position scaled out 3 times before terminal
            self._write_partial(d, "BTCUSDT", "2026-05-08T00:30:00Z", +50.0)
            self._write_partial(d, "BTCUSDT", "2026-05-08T00:45:00Z", +30.0)
            self._write_partial(d, "BTCUSDT", "2026-05-08T00:50:00Z", +20.0)
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0, f"stderr:\n{err}")
            # WR is "1 / 100.0%" (1 win out of 1 terminal trade) — the
            # exact format is "Wins / WR:              1 / 100.0%" so
            # we look for "1 / 100.0%" substring.
            self.assertIn("1 / 100.0%", out,
                f"WR should be 100% (1 of 1 terminals = win), not "
                f"diluted by PARTIAL count. Got output:\n{out}")


class HodlHelperValidationTest(unittest.TestCase):
    """Helper-output validation regression. Replaces $HODL_HELPER with a
    fake script that emits the right number of fields but with garbage
    numerics — must route to PENDING, not silent PASS."""

    def _run_with_fake_helper(self, helper_body: str) -> tuple[int, str, str]:
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "XLMUSDT", "2026-05-08T08:31:59Z", -1306.0)
            # Fake helper sits inside the journal tmpdir; we path it via
            # extra_env that the script doesn't read directly. Instead,
            # inject by replacing scripts/btc_hodl_benchmark.py path is
            # not feasible — the script computes HODL_HELPER from its own
            # dirname. So we drop the fake into a dedicated dir and patch
            # the script's PATH... actually, simpler: write the fake to
            # the same dirname the script computes (scripts/) is too
            # invasive. We instead leverage the fact that the script's
            # helper invocation uses `[[ -x "$HODL_HELPER" ]]`. By
            # temporarily renaming the real helper, we exercise the
            # missing-helper path; that's a sibling concern but verifies
            # the helper_failed=1 routing.
            # For full helper-malformed-output testing we'd need to fork
            # the script — out of scope for this regression. Instead this
            # class verifies the missing-helper routing already in place.
            real_helper = REPO / "scripts" / "btc_hodl_benchmark.py"
            backup = REPO / "scripts" / "btc_hodl_benchmark.py.testbak"
            had_real = real_helper.exists()
            if had_real:
                real_helper.rename(backup)
            try:
                fake_path = REPO / "scripts" / "btc_hodl_benchmark.py"
                fake_path.write_text("#!/bin/bash\n" + helper_body + "\n")
                fake_path.chmod(0o755)
                code, out, err = run_script(str(d))
            finally:
                if fake_path.exists():
                    fake_path.unlink()
                if had_real:
                    backup.rename(real_helper)
            return code, out, err

    def test_helper_emits_garbage_numerics_routes_to_pending(self):
        # Helper outputs 7 tab-separated fields, but numerics are non-
        # parseable strings. Without the validation fix, the awk math
        # downstream would coerce "banana" → 0 and report PASS.
        helper_body = (
            'printf "%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n" '
            '"banana" "split" "icecream" "sundae" "cherry" "yum" ""'
        )
        code, out, err = self._run_with_fake_helper(helper_body)
        self.assertEqual(code, 0,
            f"expected exit 0 (rendered, but PENDING), got {code}\n{err}")
        # The HODL line must show PENDING (validation rejected garbage)
        # rather than render the garbage values + PASS.
        hodl_line = next((ln for ln in out.splitlines()
                          if "BTC-HODL" in ln), None)
        self.assertIsNotNone(hodl_line, f"no BTC-HODL line in:\n{out}")
        self.assertIn("PENDING", hodl_line,
            f"garbage helper output must route to PENDING, got: {hodl_line!r}")


class SlipThresholdSeparationTest(unittest.TestCase):
    """Regression for the lens-as-self-correction fix that separated
    deploy-readiness slip gate (MAX_SLIP_BPS=20bp) from advisory-kill
    classification (KILL_MAX_SLIP_BP=25bp).

    Sequence of bugs caught:
        Pre-T13c: dashboard used KILL_MAX_SLIP_BP=25 in its overall PASS
                  verdict. At slip=22bp it showed PASS while formal gate
                  showed FAIL. Operator-misleading.
        T13c:     rebound s_slip to MAX_SLIP_BPS=20bp. Fixed the PASS
                  verdict alignment but broke kill-classification —
                  slip=22bp then incorrectly fired "KILL" message despite
                  not being in the >25bp kill region.
        T13d (this fix):
                  introduced s_slip_kill (against 25bp) for the kill-
                  classification path, kept s_slip (against 20bp) for
                  the deploy-readiness path. Both semantics preserved.

    Pin all four bands (post-D4 taxonomy 2026-05-12):
        slip < 20bp        → s_slip=PASS,    s_slip_kill=PASS, → DEPLOY-READY (if other gates pass)
        slip in 20-25bp    → s_slip=FAIL,    s_slip_kill=PASS, → "DEPLOY-FAIL — realized slip in 20-25bp band ..."
        slip > 25bp        → s_slip=FAIL,    s_slip_kill=FAIL, → "KILL — advisory: realized slip exceeds 25bp historical kill edge"
        no losers          → s_slip=PENDING, s_slip_kill=PENDING

    D4 (2026-05-12) split KILL vs DEPLOY-FAIL taxonomy: KILL is now
    reserved for CLAUDE.md kill criteria explicitly (slip > 25bp,
    consecutive 30d HODL underperformance). Other deploy-criterion FAIL
    states route to DEPLOY-FAIL so the operator's mental model + the
    auto_kill_execution flow alignment is preserved.
    """

    def _setup_journal(self, jdir: Path, slip_bps_target: float) -> None:
        """Write ≥150 closes (to clear MIN_TRADES gate) with synthetic
        slip targeting the requested bps value. slip_bps = slip_usd /
        notional × 10000 (losers only). Use STOP outcome for all so
        every trade contributes to slip_usd_losers."""
        notional = 100000.0
        slip_usd = slip_bps_target * notional / 10000.0
        from datetime import datetime, timedelta
        # 150 trades spread over 75 days to clear MIN_DAYS too.
        base = datetime(2026, 1, 1)
        for i in range(160):
            ts = (base + timedelta(hours=i * 12)).strftime("%Y-%m-%dT%H:%M:%SZ")
            # PnL slightly positive to ensure net_positive PASS path.
            write_close(jdir, "BTCUSDT", ts, pnl=10.0, outcome="STOP",
                        fee=120.0, slip=slip_usd, notional=notional)

    def _extract_overall(self, out: str) -> str:
        for ln in out.splitlines():
            if "Overall:" in ln or "DEPLOY-READY" in ln or "KILL" in ln or "WAITING" in ln:
                if "Overall:" in ln:
                    return ln.split("Overall:", 1)[1].strip()
        return ""

    def test_slip_in_deploy_fail_band_does_not_fire_kill(self):
        """slip = 22bp: above deploy threshold (20bp), below kill threshold
        (25bp). Must NOT fire any "KILL —" message (post-D4 taxonomy
        reserves KILL for CLAUDE.md kill criteria) — should route to
        the DEPLOY-FAIL band-specific message."""
        with tempfile.TemporaryDirectory() as tmp:
            self._setup_journal(Path(tmp), slip_bps_target=22.0)
            code, out, _ = run_script(tmp)
            # The OVERALL verdict line. Slip in band must NOT fire KILL.
            # Look for the verdict line directly to avoid false-matching
            # the "Notes" section.
            verdict_line = next(
                (ln for ln in out.splitlines() if ">>> VERDICT:" in ln),
                "")
            self.assertNotIn("KILL —", verdict_line,
                f"slip=22bp must not fire any KILL verdict (D4 taxonomy: "
                f"KILL reserved for CLAUDE.md kill criteria):\n{verdict_line}")
            self.assertIn("DEPLOY-FAIL — realized slip in 20-25bp band",
                verdict_line,
                f"slip=22bp must route to the deploy-fail band-specific "
                f"message; got: {verdict_line!r}")

    def test_slip_above_kill_threshold_fires_advisory_kill(self):
        """slip = 26bp: above kill threshold (25bp). MUST fire the
        "KILL — advisory: realized slip exceeds 25bp historical kill edge"
        message — this IS a CLAUDE.md kill criterion."""
        with tempfile.TemporaryDirectory() as tmp:
            self._setup_journal(Path(tmp), slip_bps_target=26.0)
            code, out, _ = run_script(tmp)
            verdict_line = next(
                (ln for ln in out.splitlines() if ">>> VERDICT:" in ln),
                "")
            self.assertIn("KILL — advisory: realized slip exceeds 25bp",
                verdict_line,
                f"slip=26bp MUST fire the slip-cliff kill verdict (D4 "
                f"keeps this as KILL — matches CLAUDE.md kill criterion):\n"
                f"{verdict_line}")

    def test_fee_fail_routes_to_deploy_fail_not_kill(self):
        """D4 taxonomy pin: realized fee exceeding the 12bp deploy
        threshold must route to DEPLOY-FAIL, NOT KILL. CLAUDE.md has
        no fee kill criterion; pre-D4 the dashboard mislabeled a fee
        FAIL as "KILL — realized cost exceeds kill threshold" which
        would have triggered auto_kill_execution flow inappropriately
        at Layer 2 testnet activation when real Binance fees come in
        at 14bp+ for thin-volume symbols."""
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            from datetime import datetime, timedelta
            base = datetime(2026, 1, 1)
            # 160 closes, all losers, fee=15bp (above 12bp deploy threshold),
            # slip=5bp (well below ALL slip thresholds).
            notional = 100000.0
            fee_15bp_usd = 15.0 * notional / 10000.0  # = 150
            slip_5bp_usd = 5.0 * notional / 10000.0   # = 50
            for i in range(160):
                ts = (base + timedelta(hours=i * 12)).strftime("%Y-%m-%dT%H:%M:%SZ")
                write_close(d, "BTCUSDT", ts, pnl=10.0, outcome="STOP",
                            fee=fee_15bp_usd, slip=slip_5bp_usd,
                            notional=notional)
            code, out, _ = run_script(tmp)
            verdict_line = next(
                (ln for ln in out.splitlines() if ">>> VERDICT:" in ln),
                "")
            self.assertNotIn("KILL —", verdict_line,
                f"fee 15bp must NOT fire KILL — no fee kill criterion in "
                f"CLAUDE.md. Verdict: {verdict_line!r}")
            self.assertIn("DEPLOY-FAIL — realized fee exceeds 12bp deploy threshold",
                verdict_line,
                f"fee 15bp must route to DEPLOY-FAIL with the fee-specific "
                f"message. Got: {verdict_line!r}")

    def test_slip_under_deploy_threshold_allows_deploy_ready(self):
        """slip = 5bp: well under both thresholds. The slip gate alone
        must not block DEPLOY-READY (other gates may; this test just
        verifies slip path doesn't fire FAIL/KILL)."""
        with tempfile.TemporaryDirectory() as tmp:
            self._setup_journal(Path(tmp), slip_bps_target=5.0)
            code, out, _ = run_script(tmp)
            # Find the live cohort's Realized slip line; should show PASS.
            for ln in out.splitlines():
                if "Realized slip bps:" in ln:
                    self.assertIn("PASS", ln,
                        f"slip=5bp must show PASS, got: {ln!r}")
                    return
            self.fail(f"no 'Realized slip bps:' line in output:\n{out}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
