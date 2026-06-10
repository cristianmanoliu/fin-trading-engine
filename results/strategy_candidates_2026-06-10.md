# 10 backtestable strategy candidates for a REAL edge — spec for Opus implementation (2026-06-10)

**Author:** Claude (claude-opus-4-8). **Status:** design spec, NOT yet implemented or tested.
**Audience:** the Opus agent who will implement + backtest these.

## The governing constraint (read this first — it is why the list looks like it does)

Every strategy that failed this project (RSI/MACD/Momentum/Purgatory/EMA-grid + the directional
config search) shared ONE root cause: **it was a different function of the same OHLCV price
series.** The overfit gate proved it numerically — 37 price-based configs collapse to a
**participation ratio ≈ 1.9** (≈2 independent bets), and adding candidates drove DSR 0.658→0.0010.
Even the cross-sectional axis (`results/cross_sectional_verdict_2026-06-10.md`) decayed to dead
after 2021.

**Conclusion: more price-only indicators cannot produce edge. They are correlated copies.** The
only way to a genuinely new, low-correlation return stream is a **new orthogonal signal** — a
data axis the price backtest cannot see. Each candidate below is selected for orthogonality to
"price moved on 4H," NOT for indicator novelty.

### The non-negotiable acceptance gate (apply to EVERY candidate)
A candidate is a REAL edge only if it clears ALL of:
1. **3-window walk-forward** positive (the existing `scripts/walk_forward.sh` discipline), AND
2. **Overfit gate** (`scripts/backtest_overfit_analysis.py`): does it RAISE family DSR, or just
   widen the haircut? Add it to the matrix and check DSR doesn't collapse, AND
3. **Correlation-to-LIVE < 0.5** (`scripts/candidate_correlation.py`) — a copy of the live short
   bet is worthless even if positive, AND
4. **Per-year decomposition** is not concentrated in one era/regime (the failure mode of BOTH the
   directional class (2022) AND cross-sectional (2020-21)). A real edge works across regimes, AND
5. **Realistic costs** — fee=10bp + slip=5bp minimum; for anything touching the alt SHORT leg or
   sub-5m TF, model borrow + higher slippage (Option-C fee-death killed Purgatory at −$40M).

If a candidate is positive but fails 3 or 4, it is a regime artifact (like everything so far).
Document it as such and move on. **Negative results are results — store every one.**

---

## DATA AXIS MAP (what each candidate needs)

| Axis | Have it? | Source | Backtestable? |
|---|---|---|---|
| OHLCV 1m | ✅ `data/*.csv` | local | yes (exhausted) |
| Funding rates 8h | ✅ `data/funding/*.csv` | local | yes (partly explored) |
| Open interest | ❌ | `data.binance.vision/data/futures/um/daily/metrics/` | **yes — bulk monthly zips** |
| Long/short ratios (top trader + global) | ❌ | same metrics archive | **yes** |
| Taker buy/sell volume | ❌ | same metrics archive (+ klines have it) | **yes** |
| Spot price (for basis) | ❌ | `data.binance.vision/data/spot/...klines` | **yes** |
| Liquidations | ⚠️ | `data.binance.vision/.../liquidationSnapshot` (sparse) | partial |
| Order-book depth | ❌ | not in historical archive (live-only) | NO — skip |

**The unlock for candidates 1-6: build `scripts/download_metrics.sh`** — fetch the
`futures/um/daily/metrics/<SYMBOL>/` zips (OI, L/S ratios, taker flow) for the deployed-16 (or
all 57), unzip to `data/metrics/<SYMBOL>.csv`. This is the single highest-leverage build; it
unlocks the 5 most-orthogonal candidates. ~1 day of work, mirrors the OHLCV download pattern.

---

## THE 10 CANDIDATES (ranked: orthogonality × edge-plausibility × feasibility)

### Tier A — orthogonal, plausible, backtestable after the metrics fetcher

#### 1. Open-Interest divergence (price up + OI down = weak rally → fade) ⭐ TOP PICK
- **Thesis:** Price rising on FALLING open interest = short-covering, not new longs → rally is
  fuel-less and mean-reverts. Price falling on RISING OI = new shorts piling in → continuation.
  This is a genuine microstructure signal orthogonal to price alone.
- **Signal:** at each 4H close, compute Δprice and ΔOI over the bar. Long when price↓ + OI↓
  (capitulation, shorts exhausted); short when price↑ + OI↓ (weak rally). The price×OI sign
  combination is the entry, NOT price direction alone.
- **Data:** OI from metrics archive. **Backtestable.**
- **Why it could be real:** OI is not derivable from price — it's positioning. Documented effect
  in futures literature. Low expected correlation to a pure-price EMA cross.
- **Implement:** new `OIMode` in `pkg/strategy/entry.go` (mirror `checkFundingCross` structure —
  it already reads an external series via accessor). Wire OI CSV reader like funding.
- **Falsification risk:** OI data quality on illiquid alts; the effect may only exist on BTC/ETH.

#### 2. Long/Short ratio extreme fade (crowd positioning contrarian) ⭐
- **Thesis:** When the global retail long/short account ratio hits an extreme (e.g. >75% long),
  the crowd is offside → fade them (short). Classic "smart money vs dumb money." The TOP-trader
  ratio vs GLOBAL ratio divergence is even stronger (top traders right, retail wrong).
- **Signal:** short when global-L/S-ratio > Xth percentile (crowd euphoric); long when < (1-X)th.
  Bonus: gate by top-trader ratio moving OPPOSITE the crowd (the high-conviction version).
- **Data:** `globalLongShortAccountRatio` + `topLongShortPositionRatio` from metrics archive.
- **Why orthogonal:** positioning data, not price. Should be uncorrelated with EMA cross.
- **Implement:** `LSRatioMode`, reads the ratio CSV. Percentile-threshold entry.
- **Falsification risk:** widely-known signal → likely arbitraged; test if ANY edge survives cost.

#### 3. Funding-rate CARRY harvest (collect funding, not trade on it) ⭐
- **Thesis:** DIFFERENT from the rejected funding-CROSS signal. Here funding IS the edge: when
  funding is persistently positive (longs pay shorts), hold a SHORT perp + (ideally) long spot to
  be delta-neutral and HARVEST the funding payment. The edge is the carry, not price direction.
- **Signal:** rank symbols by trailing funding rate; short the top-funding perps (collect),
  optionally delta-hedge with spot. Rebalance on funding sign persistence.
- **Data:** ✅ funding CSVs already local. Spot for the hedge leg from data.binance.vision.
- **Why orthogonal:** pure carry — return is the funding stream, structurally uncorrelated with
  price momentum. This is THE classic crypto market-neutral trade.
- **Implement:** needs a delta-neutral two-leg backtest harness (perp short + spot long). The
  engine is single-leg now — this is the biggest harness change but the most promising.
- **Falsification risk:** funding net-aggregate over 5y is ~zero (per CLAUDE.md); the edge is in
  the PERSISTENCE/selection, and costs (spot+perp round-trips, rebalancing) may eat it. The honest
  test must include the hedge-leg cost.

#### 4. Taker-flow imbalance momentum (aggressive-buyer follow) 
- **Thesis:** Sustained taker BUY volume >> taker SELL volume = aggressive accumulation that
  precedes price → follow it. Orthogonal to price because it's WHO is trading (market vs limit),
  not the price path.
- **Signal:** long when rolling taker-buy/sell ratio > threshold AND rising; mirror short.
- **Data:** `takerlongshortRatio` from metrics archive (also in klines taker-buy-volume field).
- **Implement:** `TakerFlowMode`. Klines already carry taker-buy-base-volume (field 9) — may not
  even need the metrics fetcher for a first pass.
- **Falsification risk:** taker flow is fast/noisy; likely needs sub-hour TF where fees bite.

#### 5. Basis / spot-perp dislocation (premium mean-reversion)
- **Thesis:** When perp trades at a large premium/discount to spot (basis), it mean-reverts. Large
  positive basis = perp over-heated → short perp/long spot; converges. Orthogonal — it's the
  perp-vs-spot SPREAD, not either price alone.
- **Signal:** compute basis = (perp - spot)/spot at each bar; fade extremes.
- **Data:** perp OHLCV (have) + spot OHLCV (data.binance.vision spot archive). Need spot download.
- **Implement:** `BasisMode`, reads a parallel spot CSV. Two-series entry.
- **Falsification risk:** basis is tiny + arbitraged on majors; edge may only exist on alts in
  stress, and the convergence trade needs both legs (cost).

### Tier B — orthogonal but harder/lower-confidence

#### 6. OI-confirmed breakout (price breakout ONLY when OI expands)
- **Thesis:** A price breakout backed by RISING OI (new money) is real; one on flat/falling OI is
  a fakeout. Use OI as a CONFIRMATION FILTER on a breakout, not a standalone signal.
- **Signal:** existing breakout/EMA-cross entry, but ONLY taken if ΔOI > threshold over the
  breakout bar. This is a filter that COULD raise the live config's quality if OI adds info.
- **Data:** OI metrics. **Implement:** add an OI-gate to `checkEMACrossover` (like the side filter).
- **Note:** this is the ONE candidate that could improve the LIVE config rather than replace it —
  worth testing as a gate (but beware: confluence filters already failed at −23/−59%; OI must add
  ORTHOGONAL info, not just another price-correlated gate).

#### 7. Liquidation-cascade reversal (buy the forced-seller capitulation)
- **Thesis:** Large liquidation clusters mark forced-seller exhaustion → sharp reversals. Fade the
  cascade direction (long after a long-liquidation cascade).
- **Signal:** spike in liquidation volume + price gap → enter counter-trend.
- **Data:** liquidationSnapshot archive (SPARSE — this is the feasibility risk). May need to proxy
  liquidations from extreme 1m wick + volume spikes if the data is too thin.
- **Falsification risk:** data sparsity. Lower confidence than 1-5.

#### 8. Cross-sectional carry (funding-rank long-short, dollar-neutral)
- **Thesis:** Combine the cross-sectional FRAME (which the L/S test built) with the CARRY signal
  (#3) instead of momentum: long the most-negative-funding alts, short the most-positive-funding
  alts, dollar-neutral. Earns the funding SPREAD across the universe — orthogonal to both price
  momentum AND directional.
- **Why it's better than the dead cross-sectional momentum:** the momentum version decayed after
  2021; the FUNDING version is a different driver (carry) that may persist.
- **Data:** ✅ funding CSVs local. **Implement:** extend `cross_sectional_ls.py` to rank by funding
  not trailing return. CHEAP to test — reuse the existing harness, swap the ranking signal.
- **DO THIS ONE EARLY — it's the lowest-cost test of a genuinely new driver (carry × cross-section).**

### Tier C — speculative / lower priority

#### 9. OI + funding regime composite (positioning-stress meta-signal)
- **Thesis:** Combine OI extreme + funding extreme + L/S extreme into a single "positioning stress"
  score; trade reversals when all three align (max crowd offside). Only worth building AFTER 1-3
  individually show signal — a composite of three dead signals is still dead (cf. the regime-gate
  thread, F=0/9).
- **Implement:** only after ≥1 of #1/#2/#3 passes the gate. Composite of survivors only.

#### 10. Volatility-targeted position sizing overlay (risk-management edge, not signal edge)
- **Thesis:** NOT a new entry signal — a SIZING overlay on the live config. Scale position inversely
  to realized vol (smaller in high-vol, larger in low-vol) to improve risk-adjusted return / Sharpe
  even if mean NET is similar. This targets the Sharpe/DSR directly (the metric that's failing),
  not raw NET.
- **Data:** ✅ price only (vol from existing realizedVol30d in the detector).
- **Why it's last but not worthless:** it can't find NEW edge (it's price-only), but it could make
  the EXISTING edge survive the overfit gate by improving its Sharpe — the one thing that's actually
  failing (DSR 0.658). Cheap to test on the live config. Low correlation requirement N/A (it's the
  same bet, better sized).

---

## Recommended implementation ORDER (highest EV first)

1. **#8 Cross-sectional carry** — cheapest (reuse `cross_sectional_ls.py`, swap signal to funding),
   tests a genuinely new driver, no new data fetch. Do this FIRST — same-day result.
2. **Build `scripts/download_metrics.sh`** — unlocks #1, #2, #4, #6 (OI + L/S + taker). Highest-
   leverage infra. ~1 day.
3. **#1 OI divergence** + **#2 L/S extreme fade** — the two most-orthogonal, most-plausible signals.
4. **#3 Funding carry harvest** — highest-ceiling but needs the two-leg delta-neutral harness.
5. The rest as time/results warrant; #9 only after a Tier-A survivor exists.

## What success looks like (and the honest prior)
A real edge = clears all 5 gates, especially **corr-to-LIVE < 0.5** and **not regime-concentrated**.
Honest prior from this session: most will fail (positioning signals are widely known/arbitraged;
carry is real but thin net of costs). But these are the ONLY candidates not already proven to be
price-correlated copies. If ANY of #1/#2/#3/#8 survives, it's the first genuine diversifier and
worth a shadow. If all fail, the conclusion hardens: no edge in Binance-derivable data, and the
project is correctly in operate-and-wait on the deployed config.

## Cross-references
- Why price-only search is closed: `results/overfit_expansion_2026-06-10.md` (DSR→0, PR≈1.9)
- Cross-sectional momentum is dead: `results/cross_sectional_verdict_2026-06-10.md`
- Phantom-fix + the 4 correlated mirages: `results/alt_signals_phantom_corrected_verdict_2026-06-09.md`
- The acceptance-gate tooling: `scripts/walk_forward.sh`, `backtest_overfit_analysis.py`,
  `candidate_correlation.py`
