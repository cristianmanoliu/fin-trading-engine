#!/usr/bin/env python3
"""signal_journal_reconcile.py — Signal-emit vs journal-open count reconciler.

DESCRIPTIVE ONLY — no cohort-outcome analysis.
Pre-reg: results/signal_journal_reconcile_decision_rule_2026-05-24.md

Reports per-cohort × per-symbol:
  signal_count  — emissions in signal-context sidecar (pre-executor, post-strategy-filter)
  open_count    — "event":"open" entries in journal (post-executor)
  gap           — signal_count − open_count (expected ≥ 0; see legitimate sources below)
  gap_pct       — gap / signal_count × 100 (or N/A when signal_count = 0)

DOES NOT:
  - Join signal-context records to journal close-events (no per-signal win/loss)
  - Compare cohort WR / PnL / Sharpe
  - Emit PROMOTE / KILL / HOLD verdicts
  - Attribute gap to a specific source (that requires timestamp joins = forbidden)

Exit codes:
  0  CLEAN       — both caches found, table rendered
  1  WARN        — ≥1 cohort has 0 records in either cache
  2  SCHEMA_WARN — journal record missing required field (event/symbol/ts)
  3  INPUT_ERROR — cache directory missing or empty entirely
  4  PARSE_ERROR — malformed JSONL in ≥1 file
"""
from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

# ── Import reusable helpers from sibling ────────────────────────────────────
sys.path.insert(0, str(Path(__file__).parent))
from signal_context_inspect import load_cohort, per_symbol_counts  # noqa: E402

# ── Journal schema constants ─────────────────────────────────────────────────
# Derived by grep from pkg/execution/stub.go:159-187 journalEntry struct.
# Pinned by pkg/execution/journal_schema_test.go::TestJournalEntry_FieldSetIsPinned.
# DO NOT transcribe manually — if Go struct changes, that test fires first.
JOURNAL_REQUIRED_FIELDS = {"event", "symbol", "ts", "side", "entry", "stop", "target", "reason"}
JOURNAL_OPTIONAL_FIELDS = {
    "exit", "pnl_pts", "pnl_usd", "outcome", "mfe_r", "mae_r",
    "gross_usd", "fee_usd", "slip_usd", "funding_usd", "notional_usd",
}

# ── Cohort mapping: signal-context label → journal cache subpath ─────────────
# Signal-context uses flat cohort dirs under signal_context_cache/.
# Journal uses top-level files for live + shadow/<label>/ for shadows.
# Update this dict if VPS directory layout changes.
COHORT_JOURNAL_SUBPATH: dict[str, str] = {
    "live": "",                  # top-level of journal_cache/
    "alt5-15-336": "shadow/alt5-15-336",
    "alt5-15-504": "shadow/alt5-15-504",
    "bb20": "shadow/bb20",
}

ROOT = Path(__file__).parent.parent


def load_journal_opens(journal_dir: Path) -> tuple[list[dict[str, Any]], list[str], list[str]]:
    """Load journal JSONL, return only open events + parse errors + schema errors."""
    all_records: list[dict[str, Any]] = []
    parse_errors: list[str] = []
    schema_errors: list[str] = []

    for jsonl_file in sorted(journal_dir.glob("*.jsonl")):
        for lineno, raw in enumerate(jsonl_file.read_text().splitlines(), 1):
            raw = raw.strip()
            if not raw:
                continue
            try:
                rec = json.loads(raw)
                # Only the non-omitempty fields are required.
                actual_required = {"event", "symbol", "ts"}
                actual_missing = actual_required - set(rec.keys())
                if actual_missing:
                    schema_errors.append(
                        f"{jsonl_file.name}:{lineno}: missing {sorted(actual_missing)}"
                    )
                all_records.append(rec)
            except json.JSONDecodeError as exc:
                parse_errors.append(f"{jsonl_file.name}:{lineno}: {exc}")

    open_events = [r for r in all_records if r.get("event") == "open"]
    return open_events, parse_errors, schema_errors


def gap_pct_str(gap: int, signal_count: int) -> str:
    if signal_count == 0:
        return "N/A"
    return f"{gap / signal_count * 100:.1f}%"


def main() -> int:
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--signal-cache-dir",
        default=str(ROOT / "results" / "signal_context_cache"),
        help="local signal-context cache directory",
    )
    parser.add_argument(
        "--journal-cache-dir",
        default=str(ROOT / "results" / "journal_cache"),
        help="local journal cache directory",
    )
    parser.add_argument("--json", action="store_true", help="emit machine-readable JSON")
    args = parser.parse_args()

    signal_cache = Path(args.signal_cache_dir)
    journal_cache = Path(args.journal_cache_dir)

    # ── INPUT_ERROR guard (closes missing-input → silent-success fail-open) ──
    missing = []
    if not signal_cache.is_dir():
        missing.append(f"signal cache: {signal_cache}")
    if not journal_cache.is_dir():
        missing.append(f"journal cache: {journal_cache}")
    if missing:
        for m in missing:
            print(f"INPUT_ERROR: directory not found: {m}", file=sys.stderr)
        if not signal_cache.is_dir():
            print("Run: bash scripts/signal_context_fetch.sh", file=sys.stderr)
        if not journal_cache.is_dir():
            print("Run: bash scripts/journal_fetch.sh", file=sys.stderr)
        return 3

    signal_cohort_dirs = sorted(d for d in signal_cache.iterdir() if d.is_dir())
    if not signal_cohort_dirs:
        print(f"INPUT_ERROR: signal cache empty: {signal_cache}", file=sys.stderr)
        print("Run: bash scripts/signal_context_fetch.sh", file=sys.stderr)
        return 3

    # ── Reconcile each cohort ─────────────────────────────────────────────────
    results: dict[str, dict] = {}
    all_parse_errors: list[str] = []
    all_schema_errors: list[str] = []
    any_empty_cohort = False

    for signal_dir in signal_cohort_dirs:
        label = signal_dir.name

        # Load signal-context records via shared helper
        sig_records, sig_parse_errs = load_cohort(signal_dir)
        all_parse_errors.extend(f"[{label}/signal] {e}" for e in sig_parse_errs)

        # Locate journal cohort subdir
        subpath = COHORT_JOURNAL_SUBPATH.get(label, f"shadow/{label}")
        jrnl_dir = journal_cache / subpath if subpath else journal_cache

        if not jrnl_dir.is_dir():
            # Journal dir absent for this cohort → WARN (not INPUT_ERROR; other cohorts may be fine)
            results[label] = {
                "sig_count": len(sig_records),
                "open_count": 0,
                "symbols": {},
                "journal_dir_missing": True,
            }
            any_empty_cohort = True
            continue

        jrnl_opens, jrnl_parse_errs, jrnl_schema_errs = load_journal_opens(jrnl_dir)
        all_parse_errors.extend(f"[{label}/journal] {e}" for e in jrnl_parse_errs)
        all_schema_errors.extend(f"[{label}/journal] {e}" for e in jrnl_schema_errs)

        sig_by_sym = per_symbol_counts(sig_records)
        jrnl_by_sym = per_symbol_counts(jrnl_opens)

        all_syms = sorted(set(sig_by_sym) | set(jrnl_by_sym))
        symbol_rows = {}
        for sym in all_syms:
            sc = sig_by_sym.get(sym, 0)
            oc = jrnl_by_sym.get(sym, 0)
            symbol_rows[sym] = {"signal_count": sc, "open_count": oc, "gap": sc - oc}

        if len(sig_records) == 0 or len(jrnl_opens) == 0:
            any_empty_cohort = True

        results[label] = {
            "sig_count": len(sig_records),
            "open_count": len(jrnl_opens),
            "symbols": symbol_rows,
            "journal_dir_missing": False,
        }

    # ── Render ────────────────────────────────────────────────────────────────
    if args.json:
        print(json.dumps(results, indent=2))
    else:
        _render_human(results)

    if all_parse_errors:
        print("\nPARSE_ERROR: malformed JSONL detected:", file=sys.stderr)
        for e in all_parse_errors[:10]:
            print(f"  {e}", file=sys.stderr)
        return 4

    if all_schema_errors:
        print("\nSCHEMA_WARN: journal records missing required fields:", file=sys.stderr)
        for e in all_schema_errors[:10]:
            print(f"  {e}", file=sys.stderr)
        return 2

    if any_empty_cohort:
        print("\nWARN: ≥1 cohort has 0 records in signal or journal cache.", file=sys.stderr)
        return 1

    return 0


def _render_human(results: dict[str, dict]) -> None:
    print()
    print("══════════════════════════════════════════════════════════════════")
    print("  Signal-Context ↔ Journal Reconciler")
    print("  Pre-reg: results/signal_journal_reconcile_decision_rule_2026-05-24.md")
    print("  DESCRIPTIVE ONLY — no cohort-outcome analysis")
    print("══════════════════════════════════════════════════════════════════")
    print()
    print("  Legitimate gap sources (this tool does NOT attribute):")
    print("    1. Stub.OnSignal position-already-open rejection (dominant, by design)")
    print("    2. Journal write failure (should be 0; if >0, Telegram CRITICAL already fired)")
    print("    3. Startup recovery (startup-only; N/A in steady state)")
    print()
    print("  gap = signal_count − open_count  (expected ≥ 0)")
    print("  Operator MUST NOT act on gap counts during MONITORING (pre-reg locked).")
    print()

    total_sig = sum(v["sig_count"] for v in results.values())
    total_open = sum(v["open_count"] for v in results.values())
    total_gap = total_sig - total_open

    print(f"  Total  signal={total_sig}  open={total_open}  gap={total_gap}"
          f"  ({gap_pct_str(total_gap, total_sig)})\n")

    for label, data in results.items():
        sc = data["sig_count"]
        oc = data["open_count"]
        gap = sc - oc
        missing = data.get("journal_dir_missing", False)

        status = "JOURNAL DIR MISSING" if missing else f"signal={sc}  open={oc}  gap={gap}  ({gap_pct_str(gap, sc)})"
        print(f"  ── {label}: {status} ──")

        if missing or not data["symbols"]:
            if missing:
                print("    (journal cache subdirectory not found — run journal_fetch.sh)")
            else:
                print("    (no records)")
            print()
            continue

        # Per-symbol table
        hdr = f"    {'Symbol':<22} {'Signals':>8} {'Opens':>8} {'Gap':>6} {'Gap%':>7}"
        print(hdr)
        print(f"    {'──────':<22} {'───────':>8} {'─────':>8} {'───':>6} {'────':>7}")
        for sym, row in sorted(data["symbols"].items()):
            s = row["signal_count"]
            o = row["open_count"]
            g = row["gap"]
            print(f"    {sym:<22} {s:>8} {o:>8} {g:>6} {gap_pct_str(g, s):>7}")
        print()

    print("══════════════════════════════════════════════════════════════════")
    print("  NOTE: this tool reports gap counts only.")
    print("  Cohort-outcome analysis (signal-context × journal closes) is")
    print("  deferred until forward-paper resolution per the pre-reg above.")
    print("══════════════════════════════════════════════════════════════════")
    print()


if __name__ == "__main__":
    sys.exit(main())
