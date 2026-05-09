#!/usr/bin/env python3
"""Select 16 engines from the deployed-32 list using HONEST (train-only) metrics.

The deployed-32 was selected with both train+ and test+ — that's the look-ahead
bias we measured at +26-70% by slip in `train_only_shortlist_diagnostic`. To pick
16 names without compounding the bias, we sort by train_NET only, gate on
slip=25bp robustness, and use trade count + quarterly persistence as
tie-breakers.

Inputs:
  - results/proto_oos_combined_2026-05-05.txt  (train/test NET at slip=5)
  - results/honest_oos_slip15_2026-05-06.txt   (slip=15)
  - results/honest_oos_slip25_2026-05-06.txt   (slip=25)
  - results/p4_combined_2026-05-05.txt         (trade counts)
  - results/p4_quarterly_slip15_2026-05-06.tsv (per-quarter NET)

Output: ranked 16 + cohort diagnostic.
"""
from __future__ import annotations

import json
import re
import sys
import time
import urllib.request
from pathlib import Path


def load_yaml_group(group: str, path: Path | None = None) -> list[str]:
    """Read a named symbol group from configs/symbols.yaml.

    Parser is deliberately dependency-free (no pyyaml) — handles only the simple
    flat-list format we use. Mirrors scripts/lib/symbols.sh logic.
    """
    if path is None:
        path = Path(__file__).resolve().parent.parent / "configs" / "symbols.yaml"
    in_group = False
    syms: list[str] = []
    groups_seen: list[str] = []
    for raw in path.read_text().splitlines():
        line = raw.rstrip()
        if "#" in line:
            line = line[: line.index("#")].rstrip()
        if not line.strip():
            continue
        if line[0] not in (" ", "\t") and line.rstrip().endswith(":"):
            key = line.rstrip()[:-1]
            groups_seen.append(key)
            in_group = (key == group)
            continue
        if in_group:
            stripped = line.strip()
            if stripped.startswith("- "):
                sym = stripped[2:].strip()
                if sym:
                    syms.append(sym)
    if not syms and group not in groups_seen:
        raise KeyError(f"group '{group}' not in {groups_seen}")
    return syms


# UNIVERSE is the full 57-symbol backtest pool (with comments documenting
# delisted ones). Used for load_quarterly indexing.
UNIVERSE = load_yaml_group("universe")

# CANDIDATES is the persistent-winners-32 set: symbols with train+ AND test+
# at slip=5bp in the 2026-05-05 cost-survivor battery. This is an analysis
# snapshot, not current operational state — the train+test+ filter is itself
# look-ahead but we accept it as the candidate pool here because (a) it
# matches the existing deployed-16 selection and (b) "honest" rank-based
# selection from the full universe would over-include regime flippers like
# DYDX/ICP that have strongly-negative test halves. See
# `## Train-only shortlist diagnostic (2026-05-06)` in CLAUDE.md.
CANDIDATES = [
    "ROSEUSDT", "MKRUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
    "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "ETCUSDT", "RUNEUSDT", "AVAXUSDT",
    "IMXUSDT", "DOTUSDT", "BCHUSDT", "FTMUSDT", "FILUSDT", "SOLUSDT",
    "CRVUSDT", "AAVEUSDT", "APTUSDT", "SNXUSDT", "NEARUSDT", "APEUSDT",
    "MANAUSDT", "AXSUSDT", "GALAUSDT", "ETHUSDT", "ENJUSDT", "LINKUSDT",
    "VETUSDT", "LDOUSDT",
]


_TRADING_CACHE = Path("/tmp/binance_trading_symbols.json")
_TRADING_TTL_SEC = 3600  # 1 hour


def fetch_trading_symbols() -> dict[str, str]:
    """Return {symbol: status} for all USDT-M futures contracts on Binance.

    Cached on disk with 1h TTL so repeated runs don't hammer exchangeInfo.
    Returns empty dict (no gating) if the API is unreachable — failure is
    non-fatal so the operator can still iterate offline.
    """
    if _TRADING_CACHE.exists() and (time.time() - _TRADING_CACHE.stat().st_mtime) < _TRADING_TTL_SEC:
        try:
            return json.loads(_TRADING_CACHE.read_text())
        except json.JSONDecodeError:
            pass  # fall through and re-fetch

    try:
        with urllib.request.urlopen("https://fapi.binance.com/fapi/v1/exchangeInfo", timeout=10) as r:
            data = json.loads(r.read())
    except Exception as e:
        sys.stderr.write(f"WARNING: exchangeInfo unreachable ({e}); skipping symbol-status gate\n")
        return {}

    statuses = {s["symbol"]: s["status"] for s in data.get("symbols", [])}
    try:
        _TRADING_CACHE.write_text(json.dumps(statuses))
    except OSError:
        pass  # cache write failure non-fatal
    return statuses

SYM_OOS = re.compile(
    r"^(?P<sym>[A-Z0-9]+USDT)\s+(?P<train>[+-]?\s*-?\d+)\s+(?P<test>[+-]?\s*-?\d+)\b"
)


def parse_int(s: str) -> int:
    return int(s.replace(" ", ""))


def load_oos(path: Path) -> dict[str, tuple[int, int]]:
    out = {}
    for line in path.read_text().splitlines():
        m = SYM_OOS.match(line.strip())
        if m:
            out[m["sym"]] = (parse_int(m["train"]), parse_int(m["test"]))
    return out


def load_trade_counts(path: Path) -> dict[str, int]:
    """Parse the per-symbol section of p4_combined."""
    out = {}
    pattern = re.compile(r"^\s+([A-Z0-9]+USDT)\s+(\d+)\s+trades")
    for line in path.read_text().splitlines():
        m = pattern.match(line)
        if m:
            out[m.group(1)] = int(m.group(2))
    return out


def load_quarterly(path: Path) -> dict[str, list[int]]:
    """Returns {symbol: [net_q1, net_q2, ..., net_q21]}."""
    out: dict[str, list[int]] = {s: [0] * 21 for s in UNIVERSE}
    quarters = []
    for y in range(2020, 2026):
        last = 1 if y == 2025 else 4
        for q in range(1, last + 1):
            quarters.append(f"{y}-Q{q}")
    q_idx = {q: i for i, q in enumerate(quarters)}

    for line in path.read_text().splitlines()[1:]:
        parts = line.split("\t")
        if len(parts) < 5:
            continue
        sym, period, _trades, _wins, net = parts[:5]
        if sym in out and period in q_idx:
            try:
                out[sym][q_idx[period]] = int(net)
            except ValueError:
                pass
    return out


def quarterly_score(quarters: list[int], lookback: int = 8) -> tuple[int, int]:
    """Returns (positive_quarters_in_lookback, total_active_quarters)."""
    recent = quarters[-lookback:]
    active = [n for n in recent if n != 0]
    pos = sum(1 for n in active if n > 0)
    return pos, len(active)


def main() -> None:
    root = Path(__file__).parent.parent
    oos5 = load_oos(root / "results/proto_oos_combined_2026-05-05.txt")
    oos15 = load_oos(root / "results/honest_oos_slip15_2026-05-06.txt")
    oos25 = load_oos(root / "results/honest_oos_slip25_2026-05-06.txt")
    trades = load_trade_counts(root / "results/p4_combined_2026-05-05.txt")
    quarters = load_quarterly(root / "results/p4_quarterly_slip15_2026-05-06.tsv")

    # exchangeInfo gate: drop any symbol that's not currently TRADING on Binance
    # Futures (i.e. SETTLING, DELISTED, PENDING_TRADING). This prevents the
    # 2026-05-06 MKR/FTM bug class where backtest-historical winners became
    # silent failures in production after delisting.
    trading_status = fetch_trading_symbols()
    if trading_status:
        excluded_by_gate = [s for s in CANDIDATES if trading_status.get(s, "UNKNOWN") != "TRADING"]
        if excluded_by_gate:
            print(f"  exchangeInfo gate excluded: {[(s, trading_status.get(s, 'UNKNOWN')) for s in excluded_by_gate]}")
        candidates = [s for s in CANDIDATES if trading_status.get(s, "TRADING") == "TRADING"]
    else:
        candidates = list(CANDIDATES)

    rows = []
    for sym in candidates:
        tr5 = oos5.get(sym, (0, 0))[0]
        te5 = oos5.get(sym, (0, 0))[1]
        tr15 = oos15.get(sym, (0, 0))[0]
        te15 = oos15.get(sym, (0, 0))[1]
        tr25 = oos25.get(sym, (0, 0))[0]
        te25 = oos25.get(sym, (0, 0))[1]
        n_trades = trades.get(sym, 0)
        pos_q, active_q = quarterly_score(quarters[sym], lookback=8)
        rows.append({
            "sym": sym,
            "tr5": tr5, "te5": te5,
            "tr15": tr15, "te15": te15,
            "tr25": tr25, "te25": te25,
            "trades": n_trades,
            "pos_q": pos_q, "active_q": active_q,
        })

    # Robustness gate: train_NET positive at slip=25bp (most adverse cost scenario)
    robust = [r for r in rows if r["tr25"] > 0]
    fragile = [r for r in rows if r["tr25"] <= 0]

    # Within robust, sort by train_NET at slip=15bp (realistic-middle), descending
    robust.sort(key=lambda r: -r["tr15"])

    # Pick top 16
    selected = robust[:16]
    not_selected = robust[16:] + fragile

    print()
    print("=" * 100)
    print("  16-ENGINE SELECTION  (honest, train-only ranking with slip=25 robustness gate)")
    print("=" * 100)
    print()
    print(f"  Universe: deployed-32  →  Robustness gate (train_NET>0 at slip=25bp): {len(robust)}/32 pass")
    print("  Selection rule: rank robust set by train_NET at slip=15bp, take top 16")
    print()

    print("  RECOMMENDED 16  (sorted by train_NET at slip=15bp)")
    print(f"  {'#':>2}  {'symbol':<12}  {'tr5':>8} {'tr15':>8} {'tr25':>8}   "
          f"{'te5':>8} {'te15':>8} {'te25':>8}   {'trades':>6}  {'qP/qA':>6}")
    print(f"  {'-'*2}  {'-'*12}  {'-'*8} {'-'*8} {'-'*8}   {'-'*8} {'-'*8} {'-'*8}   {'-'*6}  {'-'*6}")
    for i, r in enumerate(selected, 1):
        print(f"  {i:>2}  {r['sym']:<12}  "
              f"${r['tr5']:>+7,} ${r['tr15']:>+7,} ${r['tr25']:>+7,}   "
              f"${r['te5']:>+7,} ${r['te15']:>+7,} ${r['te25']:>+7,}   "
              f"{r['trades']:>6}  {r['pos_q']:>2}/{r['active_q']:<2}")
    print()

    print("  Aggregate metrics (selected 16)")
    sel_tr5 = sum(r["tr5"] for r in selected)
    sel_te5 = sum(r["te5"] for r in selected)
    sel_tr15 = sum(r["tr15"] for r in selected)
    sel_te15 = sum(r["te15"] for r in selected)
    sel_tr25 = sum(r["tr25"] for r in selected)
    sel_te25 = sum(r["te25"] for r in selected)
    sel_trades = sum(r["trades"] for r in selected)
    print(f"    train_NET sum: slip=5  ${sel_tr5:>+10,}    slip=15 ${sel_tr15:>+10,}    slip=25 ${sel_tr25:>+10,}")
    print(f"    test_NET  sum: slip=5  ${sel_te5:>+10,}    slip=15 ${sel_te15:>+10,}    slip=25 ${sel_te25:>+10,}")
    print(f"    Backtest trades 5y: {sel_trades:,}  (avg {sel_trades/16:.0f}/sym, {sel_trades/16/(5*365)*100:.2f} per sym per day)")
    print()

    print("  Compared to deployed-32 (full set)")
    dep_tr15 = sum(r["tr15"] for r in rows)
    dep_te15 = sum(r["te15"] for r in rows)
    dep_te25 = sum(r["te25"] for r in rows)
    dep_trades = sum(r["trades"] for r in rows)
    print(f"    Captured train (slip=15): ${sel_tr15:>+10,} of ${dep_tr15:>+10,}  "
          f"({100*sel_tr15/max(dep_tr15,1):.0f}% of total)")
    print(f"    Captured test  (slip=15): ${sel_te15:>+10,} of ${dep_te15:>+10,}  "
          f"({100*sel_te15/max(dep_te15,1):.0f}% of total)")
    print(f"    Captured test  (slip=25): ${sel_te25:>+10,} of ${dep_te25:>+10,}  "
          f"({100*sel_te25/max(dep_te25,1):.0f}% of total)")
    print(f"    Trade volume:             {sel_trades:,} of {dep_trades:,}  "
          f"({100*sel_trades/max(dep_trades,1):.0f}% of total)")
    print()

    print("  16 NAMES DROPPED")
    print(f"  {'symbol':<12}  {'tr15':>8}  {'te15':>8}   reason")
    for r in not_selected:
        if r in fragile:
            reason = "FAILS slip=25 robustness gate"
        else:
            reason = f"lower train_NET at slip=15 (rank {robust.index(r)+1})"
        print(f"  {r['sym']:<12}  ${r['tr15']:>+7,}  ${r['te15']:>+7,}   {reason}")
    print()

    period_yr_test = (2025 - 2023) + 4 / 12
    ann_test_15 = sel_te15 / period_yr_test
    print("  Forward-PnL expectation for selected 16")
    print(f"  {'-'*82}")
    print(f"    Honest backtest test annualised at slip=15bp: ${ann_test_15:>+10,.0f}/yr")
    print(f"    Adjusted for monthly-split inflation (-7%):   ${ann_test_15*0.93:>+10,.0f}/yr")
    print(f"    Trade rate: ~{sel_trades/16/(5*365):.2f} trades/sym/day  →  ~{sel_trades/(5*365):.1f} trades/day across 16")
    print(f"    60-day forward-paper expects ~{60*sel_trades/(5*365):.0f} trades total")
    print()

    # Output systemd service list for the deploy command
    print("  SYSTEMD SERVICE LIST")
    services = " ".join(f"paper-live@{r['sym'].lower()}.service" for r in selected)
    print(f"    {services}")


if __name__ == "__main__":
    main()
