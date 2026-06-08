#!/usr/bin/env python3
"""pnl_snapshot.py — realized + unrealized P/L per cohort from journal cache.

Realized  = sum of close-event pnl_usd (net, incl fees+slip).
Unrealized= open positions (open w/o matching close) marked to current Binance
            mark price: SHORT pnl = (entry-mark)*units; units = notional/entry,
            notional ~ stake/stop_dist. We reconstruct units from the journal's
            implied sizing: the close events show notional_usd; for still-open we
            approximate units via $1000 stake / stop_distance (the live sizing rule)
            — same as the engine. Reported as indicative (mark-to-market).

Reads results/journal_cache/ (live = top-level *.jsonl; shadows = shadow/<name>/).
Fetches current mark prices from fapi.binance.com (one batch call).

Read-only. No VPS/engine interaction.
"""
import json, os, sys, glob, urllib.request
from collections import defaultdict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CACHE = os.path.join(ROOT, "results", "journal_cache")
STAKE = 1000.0  # live --stake_usd

def load_events(files):
    evs = []
    for f in files:
        try:
            with open(f) as fh:
                for ln in fh:
                    ln = ln.strip()
                    if not ln:
                        continue
                    try:
                        evs.append(json.loads(ln))
                    except Exception:
                        pass
        except FileNotFoundError:
            pass
    return evs

def cohort_pnl(events):
    """Return (realized_net, realized_gross, fees, slip, n_closed, open_positions)."""
    realized = realized_gross = fees = slip = 0.0
    n_closed = 0
    # Track opens per (symbol) — match FIFO with closes. Live runs one position
    # per symbol at a time, so symbol key is sufficient.
    open_stack = defaultdict(list)
    opens_seen = 0
    for e in sorted(events, key=lambda x: x.get("ts", "")):
        ev = e.get("event")
        sym = e.get("symbol")
        if ev == "open":
            opens_seen += 1
            open_stack[sym].append(e)
        elif ev == "close":
            n_closed += 1
            realized += e.get("pnl_usd", 0.0)
            realized_gross += e.get("gross_usd", 0.0)
            fees += e.get("fee_usd", 0.0)
            slip += e.get("slip_usd", 0.0)
            if open_stack[sym]:
                open_stack[sym].pop(0)
    # leftover opens = still-open positions
    open_positions = [o for lst in open_stack.values() for o in lst]
    return realized, realized_gross, fees, slip, n_closed, open_positions

def fetch_marks(symbols):
    """Batch fetch mark prices. Returns {symbol: price}."""
    if not symbols:
        return {}
    out = {}
    try:
        url = "https://fapi.binance.com/fapi/v1/premiumIndex"
        req = urllib.request.Request(url, headers={"User-Agent": "pnl-snapshot"})
        with urllib.request.urlopen(req, timeout=20) as r:
            data = json.load(r)
        marks = {d["symbol"]: float(d["markPrice"]) for d in data}
        for s in symbols:
            if s in marks:
                out[s] = marks[s]
    except Exception as ex:
        print(f"  (mark fetch failed: {ex} — unrealized shown as N/A)", file=sys.stderr)
    return out

def unrealized(open_positions, marks):
    """Mark-to-market open shorts. units ~ STAKE / stop_dist (live sizing)."""
    total = 0.0
    rows = []
    for o in open_positions:
        sym = o["symbol"]; entry = o["entry"]; stop = o["stop"]; side = o["side"]
        mark = marks.get(sym)
        if mark is None or entry <= 0:
            rows.append((sym, side, entry, None, None))
            continue
        stop_dist = abs(stop - entry)
        units = STAKE / stop_dist if stop_dist > 0 else 0.0
        if side == "SHORT":
            pnl = (entry - mark) * units
        else:
            pnl = (mark - entry) * units
        total += pnl
        rows.append((sym, side, entry, mark, pnl))
    return total, rows

def report(label, files):
    evs = load_events(files)
    if not evs:
        print(f"{label:16s}  (no journal data)")
        return
    rz, rzg, fee, slp, nc, opens = cohort_pnl(evs)
    syms = sorted({o["symbol"] for o in opens})
    marks = fetch_marks(syms) if syms else {}
    unrz, urows = unrealized(opens, marks)
    print(f"\n{label}")
    print(f"  realized NET : ${rz:>12,.2f}   ({nc} closed trades)")
    print(f"    gross      : ${rzg:>12,.2f}")
    print(f"    fees       : ${fee:>12,.2f}")
    print(f"    slippage   : ${slp:>12,.2f}")
    print(f"  unrealized   : ${unrz:>12,.2f}   ({len(opens)} open)")
    print(f"  TOTAL (R+U)  : ${rz+unrz:>12,.2f}")
    for sym, side, entry, mark, pnl in urows:
        if pnl is None:
            print(f"      open: {sym} {side} entry={entry} mark=N/A")
        else:
            print(f"      open: {sym} {side} entry={entry} mark={mark} → ${pnl:,.2f}")

# Live = top-level *.jsonl
report("LIVE (9/21 EMA, deployed)", glob.glob(os.path.join(CACHE, "*.jsonl")))

# Shadows
for d in sorted(glob.glob(os.path.join(CACHE, "shadow", "*"))):
    if os.path.isdir(d):
        name = os.path.basename(d)
        report(f"SHADOW {name}", glob.glob(os.path.join(d, "*.jsonl")))
