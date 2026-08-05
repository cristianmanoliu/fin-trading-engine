# Verdict — maker-entry execution (Option A): **NO-GO**

Pre-registration: `results/maker_execution_prereg_2026-08-05.md` (locked
2026-08-05, before the 5y journal was generated).

**Result: NO-GO on criteria 2 and 3. Criterion 1 passes. The strategy class is
closed for good, per the design doc's own language.**

---

## 1. Sample

5-year continuous backtest, deployed-16 symbols, live config unchanged
(4H EMA9×21, short-only, target_rr 6.0, max-hold 504h, fee 10bp, slip 5bp,
`--exact-fills --include-boundary --pessimistic-ambiguous`).

**2,882 trades paired** (630 winners / 2,252 losers), 2020-01-20 → 2026-07-31.
1 trade excluded for missing price data. This is **21× the forward-paper book**,
which was underpowered at n=134 (24 winners) and produced a window-dependent
non-answer — see pre-reg §1.

## 2. The three locked criteria

### Criterion 1 — fill rate > 70% — **PASS**

77.3% (2,227 / 2,882) at the locked 5m window. Fill latency p50=1m, p90=3m.

### Criterion 2 — winner fill rate not materially below loser fill rate — **FAIL**

Locked threshold: gap > −5pp.

| window | winner fill | loser fill | gap | z |
|---|---|---|---|---|
| **5m (locked)** | **71.7%** | **78.8%** | **−7.1pp** | **−3.74** |
| 15m | 81.3% | 88.1% | −6.9pp | −4.48 |
| 30m | 86.0% | 92.4% | −6.3pp | −4.90 |
| 60m | 88.4% | 94.7% | −6.3pp | −5.56 |

The gap breaches the threshold at the locked window and at **every sensitivity
window**, with the same sign and similar magnitude. z = −3.74 to −5.56.

**The mechanism is not subtle.** For a SHORT, a resting sell-limit at the signal
price fails to fill only when price drops immediately and never returns — which
is precisely what a winning short looks like. Maker entry systematically misses
the trades it most needs to be in.

### Criterion 3 — maker net beats actual, including after drop-top-5% — **FAIL**

| window | maker net | actual net | delta | drop-top-5%: maker | actual |
|---|---|---|---|---|---|
| **5m (locked)** | $435,842 | $774,211 | **−$338,369** | **−$228,073** | −$86,229 |
| 15m | $530,861 | $774,211 | −$243,350 | −$210,916 | −$86,229 |
| 30m | $547,439 | $774,211 | −$226,772 | −$236,176 | −$86,229 |
| 60m | $555,499 | $774,211 | −$218,712 | −$246,050 | −$86,229 |

Maker execution is worse by **$219k–$338k** at every window, and the drop-top-5%
book gets *worse*, not better — from −$86k to −$211k…−$246k.

The 655 unfilled trades at the locked window carried **+$424,275 of gross P&L**.
The entry-fee saving (3bp on ~$70k median notional ≈ $21/trade) does not come
close to paying for that.

## 3. Why the forward-paper pilot looked positive, and was wrong

Recorded because it is the instructive part.

On the 134-trade book, a 15m window showed **+$14,728 "improvement"**. That
number was an artifact:

- Gross P&L changed with the window — impossible for a pure execution change,
  since maker entry alters the fee, not the price. The variation was **trade
  selection**, not execution.
- At 5m the misses were the six largest winners (+6.08R, +6.01R, +6.01R,
  +6.02R, +4.95R, +2.94R). At 15m the misses were eleven trades all at −1.0R.
  Whichever window happened to skip losers looked brilliant.
- With 24 winners, a ±10pp fill-rate gap is ±2 trades.

At n=2,882 the noise resolves and the sign is stable: **−6.3 to −7.1pp, always
against.** The small-sample result had the right mechanism (5m correctly caught
the winners being missed) and the wrong conclusion drawn from the wrong window.

A second bug in the first pass, fixed before any result was recorded: the
signal bar's own high *is* the entry price — the tick that fires the 4H cross —
so including that bar reported a fraudulent 99.3% fill at p50 = 0 minutes. The
replay now starts strictly at +1m.

## 4. What this closes

Option A was the design doc's **highest-EV** v2 candidate and its stated
decision point: *"if maker fill rate is >70% and missed trades are not
systematically the winners, build it. Otherwise the class is closed for good."*

Fill rate passes. Missed trades **are** systematically the winners. Under the
locked rule, **the class is closed.**

This also disposes of the §1 break-even finding as an actionable route. Break-even
was 8bp and the run executed at 10bp; maker entry buys ~3bp on the entry leg,
which is arithmetically enough. But it cannot be collected — the 3bp is
conditional on being filled, and the fills are adversely selected. **The 2bp gap
is real and remains unbridgeable by this route.**

## 5. Scope

This says nothing about a cheaper *taker* venue. A venue at ≤8bp round-trip
taker still flips the sign by the §1 arithmetic, with no adverse selection,
because a market order always fills. That remains untested and is unaffected by
this verdict — but it is a venue-access question, not a strategy question, and
venue access is blocked (`project_binance_futures_region_block`).

## 6. Reproduction

```bash
# 1. generate the 5y journal (16 symbols, live config)
#    scratchpad/maker/run_one.sh per symbol, --journal-dir
# 2. replay
python3 scripts/maker_fill_replay.py --journal-dir <dir> --window-min 5
```

`scripts/maker_fill_replay.py` reproduces the forward-paper close-out book
exactly (134 paired trades + 2 abandoned) as a correctness check on its
chronological stack-replay pairing.
