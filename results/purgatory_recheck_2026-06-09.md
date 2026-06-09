# "Purgatory Method" (Reddit 0DTE) — entry-signal recheck on perps (pre-reg + verdict, 2026-06-09)

**Status:** LOCKED BEFORE run. Pure research. Tests an externally-sourced strategy idea.

## Source

Reddit r/0DTE-style post (u/Suspicious_Ninja4424): on the **4-minute** TF for **0DTE
options** on equities (SPY/TSLA/AVGO/NVDA): (5 EMA, 9 EMA) + (VWAP, 30 EMA). Calls = 5>9
EMA crossing above BOTH VWAP and 30EMA, exit after 2 candles. Puts = 5<9 crossing below
both, "let them close." Stop = close below 8 SMA for 2 candles. Evidence: anecdotes ("made
$1300 on NVDA puts in 10min"), no sample size, no loss accounting.

## What we CAN and CANNOT test

Our engine is **crypto perp futures** (linear PnL, funding, fixed-RR/wick-stop exit). It has
**no options-pricing / Greeks (gamma/theta/IV) machinery** and no 4min-equities data. So:

- **CANNOT test:** the 0DTE-options wrapper (where most of the edge/risk lives — theta decay,
  IV crush, gamma on an 8-minute hold), the "2-candle scalp / let puts close" asymmetric exit,
  or 4min equities.
- **CAN test:** whether the **entry signal** (5/9 EMA cross gated by price on the same side of
  BOTH VWAP and EMA30) has directional edge on our 57 crypto perps, using our standard
  fixed-RR/wick-stop exit + production cost model. Closest TF analog to 4min = **5m**.

**This is a necessary-not-sufficient screen.** If the entry signal has no edge even here, the
idea is dead for us. If it does, it remains INCONCLUSIVE for the actual options strategy.

## Implementation

New `--purgatory-mode` (pkg/strategy/entry.go `checkPurgatory`): 5/9 cross (reuses ema9/ema21
via `--ema-fast-period 5 --ema-slow-period 9`) + same-TF EMA30 + session VWAP gate. 3 unit
tests (`TestPurgatory_*`). Harness: `PURGATORY_MODE` in walk_forward.sh / p4_fresh_oos.sh.

## Grid (LOCKED) — 4 cells × 3 walk-forward windows × 57 symbols, slip=5/fee=10/mh504

| # | Role | TF | Side | Rationale |
|---|---|---|---|---|
| 0 | BASELINE | 4H | short | LIVE config reference |
| 1 | PURG-5m-both | 5m | both | faithful to Reddit (calls+puts), closest TF |
| 2 | PURG-5m-short | 5m | short | our class (short-only) for comparison |
| 3 | PURG-4H-short | 4H | short | cost-contrast (4H wick wider → lower implicit leverage) |

## Pre-registered prediction

**Strong prior: NEGATIVE, fee-death on 5m.** The CLAUDE.md cost geometry (Option C
falsification) says fast-TF + tight wick stops force ~500× implicit leverage → round-trip
taker fees eat the edge. A 1-month smoke (BTC 2022, 5m, both-sides) already showed 149 trades,
WR 15.4%, **−$45,153 NET** (gross +$12k, fees+slip −$57k). I expect cells 1-2 deeply negative.
Cell 3 (4H) may be less-negative but the gate is a confluence trend filter, a class we already
refuted (`confl_1d_bias_sweep`: −23% to −59%). Falsifiable candidate trigger: any PURG cell
beats baseline ≥10% on ≥2/3 windows.

## Decision rule (LOCKED)

| Result | Action |
|---|---|
| All PURG cells lose / fee-death | File NEGATIVE — confirms perps can't host this 4min-scalp idea. Recommend against building options infra. |
| Any PURG cell beats baseline ≥10% on ≥2/3 | File as entry-signal CANDIDATE (still inconclusive for options; would need its own pre-reg + overfit gate). |

**No deployment regardless.** Research-only; engine has no options path.

---

## Results — completed 2026-06-09

| Cell | TF | Side | W1 | W2 | W3 | Mean | Wins | Trades | vs LIVE |
|---|---|---|---:|---:|---:|---:|:---:|---:|:---:|
| BASELINE | 4H | short | +175,021 | +645,812 | +65,044 | +$295,292 | 3/3 | 5,572 | ref |
| **PURG-5m-both** | 5m | both | −45.8M | −33.9M | −40.3M | **−$40,021,059** | 0/3 | 343,493 | **−13,652%** |
| **PURG-5m-short** | 5m | short | −22.7M | −18.2M | −24.6M | **−$21,817,439** | 0/3 | 213,298 | **−7,488%** |
| PURG-4H-short | 4H | short | +177,999 | +653,666 | +556,811 | +$462,825 | 3/3 | 6,698 | +57% |

### Verdict — the ACTUAL strategy (4min/5m) is fee-death; the only positive cell is a different strategy

**The faithful Purgatory Method (5m, the TF analog of the Reddit 4min) is annihilated:
−$40M both-sides / −$22M short-only, 0/3 windows, 343k/213k trades.** This is the Option-C
falsification at maximum amplitude — the exact cost geometry CLAUDE.md documents: fast-TF
tight wick stops → ~500× implicit leverage → round-trip taker fees + slippage incinerate the
entire notional. A 1-month smoke had already flashed it (BTC 2022 5m both-sides −$45k); the
full 57-sym/5y confirms catastrophe. **On any instrument we can actually trade, the Purgatory
Method as specified is a money shredder.** (The Reddit anecdotes are 0DTE-options on equities,
where the cost structure is totally different — but that is precisely the part our engine
can't model, and the part where theta/IV/gamma on an 8-minute hold dominate.)

**The one positive cell (PURG-4H-short, +57%) is NOT the Purgatory Method** — it is the
VWAP+EMA30 confluence gate moved to 4H, where the wick is wide enough that fees stop
dominating. And it is the **4th "beats baseline" short trigger today with the identical
tell:** its W3 (2025-26 bear) = +$556k vs the live EMA's +$65k — the same single-bear-window
windfall that inflated RSI (+$667k W3), Momentum (+$882k), and MACD (+$768k). It over-fires
(6,698 trades, +20% cost surface), and a confluence trend filter is a class already refuted at
slower confluence (`confl_1d_bias_sweep` −23% to −59%). It is the same crash-dependent
always-short bet re-expressed, not new edge — it would fail the clean overfit gate (DSR 0.658)
exactly as the others do, and it is not a diversifier.

### Decision rule applied

Faithful cells (1,2) = catastrophic fee-death → **the Purgatory Method does not survive on
perps. Recommend AGAINST building options infrastructure to chase a Reddit anecdote.** The
positive 4H cell is filed (like RSI/Momentum/MACD) as a non-deployable entry-signal curiosity,
NOT pursued: charter freeze + FRAGILE overfit + same-W3-windfall + non-diversifying.

### What this is and isn't

- **IS:** proof the entry signal at its native fast TF is fee-death on perps; and a 4th
  confirmation that aggressive short triggers all ride the same 2025-26 bear window.
- **IS NOT:** a test of the actual 0DTE-options strategy (theta/IV/gamma/2-candle-scalp exit
  unmodeled — would require an options-pricing subsystem the engine does not have). If anyone
  ever wants to chase the options version, that is a from-scratch new instrument class, not an
  extension of this perp-futures engine. Not recommended off this evidence.

### Net

No deployable edge. The Purgatory Method joins the closed research backlog as **refuted on
perps (fee-death)**. Forward-paper remains the only arbiter; backtest search stays exhausted.
