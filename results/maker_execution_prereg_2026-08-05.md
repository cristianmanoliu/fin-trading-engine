# Pre-registration — maker-entry execution (Option A)

**Locked 2026-08-05, before generating the 5y backtest journal.**

Follows `results/v2_lessons_and_design_2026-08-04.md` §3 Option A. This document
fixes the hypothesis, the parameters, and the decision thresholds before any
result is observed. It exists because the first pass at this question (on the
134-trade forward-paper book, same day) produced a +$14.7k "improvement" that
was an artifact of choosing the fill window after seeing the P&L.

---

## 1. Why the forward-paper book cannot answer this

Recorded so the pilot run is not silently reused as evidence.

The forward-paper archive has 134 trades, 24 of them winners. Replaying maker
fills against it gave:

| fill window | fill rate | net vs actual |
|---|---|---|
| 5m | 81.3% | **−$8,121** |
| 15m | 91.8% | +$14,728 |
| 30m | 94.8% | +$10,411 |
| 60m | 96.3% | +$7,983 |

A $23k swing driven by a free parameter. The mechanism is visible in *which*
trades go unfilled: at 5m the misses are the six largest winners (+6.08R,
+6.01R, +6.01R, +6.02R, +4.95R, +2.94R); at 15m the misses are eleven trades
all at −1.0R. Gross P&L moves with the window, which maker execution cannot
cause — so the effect is trade selection, not execution.

Direct adverse-selection measurement on that book: winners' mean fill latency
3.3m vs losers' 16.1m, but the fill-rate gap flips sign (−7.7pp at 5m, +10.0pp
at 15m) on n=24 winners. Two trades move it. **Underpowered — no verdict.**

## 2. Hypothesis

> Resting a maker sell-limit at the 4H signal price, instead of entering
> short at market, improves net P&L after accounting for the trades that
> never fill.

**Null:** the entry-fee saving is offset or exceeded by adverse selection —
unfilled trades are disproportionately the winners.

## 3. Parameters — fixed now, not tunable after

| Parameter | Value | Why this value |
|---|---|---|
| Fill window | **5 minutes** | The pessimistic end. Chosen because it is the only window that is *not* a free parameter: it is the shortest interval that a real resting order plausibly survives, and on the pilot it was the window that made maker execution look **worst**. Choosing the worst-case option removes the degree of freedom. |
| Fill rule | 1m `high >= entry`, bars strictly **after** the signal bar | The signal bar's own high IS the entry price (the tick that fired the cross). Including it fills 100% by construction — this was a real bug in the first pass. |
| Queue model | touch = fill | Optimistic, deliberately. If the answer is NO under optimistic fills it is NO under realistic ones. |
| Maker entry fee | 2 bp | Entry leg only. Exit remains taker (a stop or target is a market/stop-market order). Actual run was 5 bp per side. |
| Exit fee | 5 bp taker | Unchanged from the live run. |
| Stop slippage | 5 bp | Unchanged (`--stop-slippage-bps 5`). |
| Strategy config | live config, unchanged | `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504 --fee-bps 10 --stop-slippage-bps 5`, EMA 9/21. No re-tuning. |
| Sample | 5y continuous, deployed-16 symbols | `--exact-fills --include-boundary --pessimistic-ambiguous`. Continuous, not month-segmented (segmented inflates ~5%). |

**Sensitivity reporting:** results at 15m and 30m windows will be reported
alongside, clearly labelled as sensitivity, and **cannot** be substituted for
the 5m result in the verdict. If the verdict at 5m and at 30m disagree, the
finding is reported as window-dependent and therefore NOT actionable.

## 4. Decision thresholds — locked

Verdict is mechanical. All three must hold for GO:

1. **Fill rate > 70%** at the 5m window.
2. **Winner fill rate is not materially below loser fill rate.** Threshold:
   `winner_fill_rate - loser_fill_rate > -5pp`. A larger negative gap means the
   strategy systematically misses its winners — the adverse-selection failure.
   Reported with a two-proportion z-test; the point estimate governs the
   verdict, the p-value is context.
3. **Net P&L of the filled-only book at maker fees exceeds the actual all-taker
   book**, and does so **after dropping the top 5% of trades** (L4 honesty
   check). Aggregate-only improvement does not count.

Any one failing → **NO-GO, the strategy class is closed for good** (the design
doc's own language).

## 5. What a GO would and would not license

A GO licenses **one** thing: building a maker execution layer and re-validating.
It does **not** revive v1, does not imply real-money deployment, and does not
change the venue-access blocker. Break-even was 8 bp; maker execution buys
roughly 3 bp on the entry leg (5→2). That is enough to cross break-even and not
obviously more.

## 6. Analysis code

`scripts/maker_fill_replay.py`, written before this pre-registration and
verified to reproduce the forward-paper close-out book exactly (134 paired
trades, 2 abandoned, matching `forward_paper_closeout_2026-08-04.md`). Trade
pairing is chronological stack replay per symbol, not count-based.

Data: 1m klines from `data.binance.vision`, already local for 2020-01 → 2026-04;
2026-05 → 2026-08 fetched 2026-08-05 for the pilot.
