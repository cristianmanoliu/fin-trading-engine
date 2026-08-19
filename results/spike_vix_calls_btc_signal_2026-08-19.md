# Spike: BTC EMA-cross → VIX calls

**Status:** Spike finding, 2026-08-19. Throwaway code, no pre-registration, no
DSR budget spent. This is a feasibility answer, not a trading decision.

## Question

The fin-trading-engine's EMA 9/21 bearish cross is a real signal (corr −0.588
vs BTC, structurally long-crash) that cannot clear costs on perp futures (fees
on notional × implied leverage = 114% of gross). Can the signal be expressed on
a different instrument class where the cost geometry doesn't kill it?

## What was tested

Three instrument expressions of the crash-capture idea, all using daily data
from 2015-01 to 2026-08:

| Expression | Signal source | Instrument | Result |
|---|---|---|---|
| EMA-cross on equities → equity puts | SPY/QQQ own EMA | SPY/QQQ ATM puts | **DEAD.** SPY t=−3.06, QQQ t=−3.37. ~80% of crosses are false alarms; premium bleed kills it. Crashes too infrequent on equities. |
| BTC EMA-cross → SPY puts | BTC EMA | SPY ATM puts | **DEAD.** t=−1.42, tail-carried. BTC crashes don't reliably drag equities down. |
| BTC EMA-cross → VIX calls | BTC EMA | VIX ATM calls | **INTERESTING.** t=+1.64, survives drop-top-5%, beats random entry. Details below. |

## VIX-call result

**Setup:** BTC daily EMA 9/21 bearish cross → buy 1 ATM VIX call (21-day
expiry). Exit on BTC bullish cross or expiry, whichever comes first. Premium
modeled as `0.4 × VIX × IV_vix × √(DTE/252)` (Black-Scholes ATM
approximation). IBKR fee $0.65/contract each way.

**Core result (IV=0.80, max_hold=21d):**

| Metric | Value |
|---|---|
| Trades | 50 (11 years) |
| Win rate | 36% |
| Mean P&L | +0.726 R |
| Median P&L | −0.718 R |
| t-statistic | +1.64 |
| Total P&L | +$4,279 |
| Drop-top-5% mean | +0.255 R |
| Sharpe (ann.) | 0.47 |
| Fee as % of risk | 0.28% |
| Positive years | 8 of 12 |

**By year:**

| Year | n | Mean R | Total $ |
|---|---|---|---|
| 2015 | 4 | +1.540 | +$762 |
| 2016 | 2 | −0.712 | −$299 |
| 2017 | 4 | −0.858 | −$360 |
| 2018 | 6 | +2.734 | +$1,506 |
| 2019 | 3 | +2.489 | +$844 |
| 2020 | 4 | +0.873 | +$1,341 |
| 2021 | 4 | −0.085 | −$200 |
| 2022 | 5 | +0.751 | +$660 |
| 2023 | 3 | +0.182 | +$119 |
| 2024 | 6 | +0.073 | −$257 |
| 2025 | 5 | +0.733 | +$435 |
| 2026 | 4 | −0.112 | −$272 |

## Sensitivity tests

### IV-of-VIX (premium cost sensitivity)

| IV assumption | Mean R | t-stat | Drop-top-5% | Verdict |
|---|---|---|---|---|
| 0.60 | +1.301 | +2.20 | +0.673 OK | Survives |
| 0.70 | +0.972 | +1.92 | +0.434 OK | Survives |
| 0.80 | +0.726 | +1.64 | +0.255 OK | Survives |
| 0.90 | +0.534 | +1.35 | +0.115 OK | Marginal |
| 1.00 | +0.380 | +1.07 | +0.004 OK | Marginal |
| 1.20 | +0.150 | +0.51 | −0.163 TAIL | Dead |

Edge degrades smoothly with premium cost. Survives up to IV≈1.00, breaks at
1.20. Real IV-of-VIX is typically 0.60–1.00, spiking above 1.00 during stress
— which is when the signal fires. This is the main risk: you may be buying
expensive vol when you need to.

### Hold period

| Max hold | Mean R | t-stat | Drop-top-5% | Verdict |
|---|---|---|---|---|
| 5d | +0.173 | +0.58 | −0.115 TAIL | Dead |
| 10d | +1.412 | +2.15 | +0.670 OK | **Best** |
| 15d | +1.246 | +1.58 | +0.283 OK | Survives |
| 21d | +0.726 | +1.64 | +0.255 OK | Survives |
| 30d | +0.137 | +0.50 | −0.151 TAIL | Dead |
| 42d | +0.355 | +0.85 | −0.120 TAIL | Dead |

Vol spike is front-loaded. 10d is the best hold period (t=+2.15, 42% WR). But
10d is now a **fitted parameter** — it was not pre-registered, so it carries
selection risk.

### Controls

| Test | Mean R | t-stat | Drop-top-5% |
|---|---|---|---|
| **BTC signal → VIX calls** | +0.726 | +1.64 | +0.255 OK |
| SPY signal → VIX calls | −0.295 | −0.98 | −0.644 TAIL |
| Random entry → VIX calls (1000 sims) | +0.247 ± 0.453 | — | — |

- SPY's own EMA cross into VIX calls is dead (−0.295R). The edge is **not** in
  VIX calls generally — it is in the **BTC timing**.
- Random entry: 15.2% of random-entry sims match or exceed BTC-timed entry.
  Suggestive but not conclusive (want <5%).

## Why this is the first thing that worked

The perp-futures cost geometry is `(fee + slip) × L × 1e-4` where L = 1 /
stop_width. On wick stops L ≈ 67×; fees are 114% of gross. No signal
improvement can fix that — cost is deterministic, gross is a random variable
with 15× wider CI.

Options eliminate the cost trap structurally: max loss = premium, L = 1×, fee
is 0.28% of risk. The question becomes purely "is there a signal?" — and the
BTC EMA cross appears to carry cross-asset vol-timing information that (a)
doesn't exist in equities' own EMA crosses, and (b) is better expressed as
long-vol (VIX calls) than short-equity (SPY puts).

The mechanism: BTC is a global risk-appetite thermometer with higher vol and
more frequent drawdowns than equities. A bearish EMA cross on BTC indicates
risk-off conditions that manifest as VIX spikes — not necessarily equity price
drops (explaining why SPY puts fail but VIX calls work).

## What this does NOT establish

1. **Statistical significance.** t=1.64 is p≈0.05 one-tailed. 50 trades is
   below the 63-trade power floor for ±10pp CI. This is suggestive, not
   conclusive.
2. **Real premium costs.** The Black-Scholes ATM approximation is rough. Real
   VIX option premiums depend on term structure, skew, and bid-ask spreads
   (which widen during stress — exactly when the signal fires).
3. **VIX settlement mechanics.** VIX options are European-style, settle to VIX
   SOQ (Special Opening Quotation), not the VIX spot. The payoff model here
   uses spot VIX, which overstates liquidity and simplifies settlement.
4. **Robustness of hold period.** 10d being "best" is a fitted result. Only
   21d was tested first; the sensitivity sweep was exploratory.
5. **Execution feasibility.** IBKR VIX option access from Romania needs
   verification (venue-access = Step 1 of the method).

## Recommendation

**This merits a new project** — not a continuation of fin-trading-engine.
Different instrument, different venue, different codebase. Before any code:

1. **Venue access (Step 1):** Confirm IBKR allows VIX option trading from a
   Romanian account. Do this first — it's an afternoon and kills the idea if
   it fails.
2. **Real option data:** Obtain historical VIX option chains (CBOE or IBKR) to
   validate premium assumptions against actual bid/ask at the entry timestamps.
3. **Pre-register:** Lock hold period (10d or 21d — pick before looking at
   real-data results), IV threshold (don't buy when IV-of-VIX > X), and
   accept/reject criteria. One trial, no parameter shopping.
4. **Power budget:** At ~4.2 trades/year, 63 trades = ~15 years of data. The
   2015-2026 sample is 11 years. Consider whether a 2012-start (adding ~3
   years) is available or whether live forward-testing is needed.

## Spike artifacts (throwaway)

All in session scratchpad, not committed:
- `spike_equity_crash.py` — SPY/QQQ EMA-cross put backtest (dead)
- `spike_vix_and_crossasset.py` — VIX-call and cross-asset spikes
- `spike_vix_sensitivity.py` — IV/hold-period/control tests
