# Strategy ideas backlog — pre-registered for milestone 2

**Status:** LOCKED 2026-05-08, mid-day. No data has been run on these ideas. Each entry is a pre-registered decision rule: the verdict criterion is fixed BEFORE any backtest, so when milestone 2 picks one up the rule applies mechanically.

**Discipline contract:**
- Do NOT execute any of these before milestone 2 begins. Casually running them consumes statistical degrees of freedom that this pre-registration is supposed to bank.
- When milestone 2 starts, pick a SUBSET (probably 3-5 of the 8) per the prioritization rubric below. Execute strictly to the locked criterion.
- The remaining ideas roll to milestone 3, or are explicitly archived as low-priority.
- Family-wise α correction at execution: if N ideas are tested in a milestone, each individual α = α_family / N. With α_family = 0.05 and N = 5, individual α = 0.01.

**Why this matters:** the project's results are trustworthy because every analysis was pre-registered before data was looked at. Drift toward post-hoc strategy selection (search every plausible mechanism, keep the winner) is exactly how published quant research generates spurious results. Locking the rule today binds future-self to honesty.

---

## Prioritization rubric

For each entry below, the four sub-scores (1-5, higher = better) are filled in to support milestone-2 ordering. The composite score is the geometric mean (penalizes any zero — an idea with implementation_complexity=1 isn't compensated by mechanism_plausibility=5).

| Sub-score | Meaning |
|---|---|
| Mechanism plausibility | Strength of the prior — does the mechanism have non-trivial reason to exist? |
| Statistical independence | Is this idea genuinely orthogonal to mechanisms already tested in milestone 1? |
| Implementation complexity (inverted) | 5 = trivial, 1 = months of new infrastructure |
| Data availability (inverted) | 5 = data already in repo, 1 = needs new source |

---

## Idea catalog

### A1. Realized-volatility regime filter

**Hypothesis:** The strategy's per-trade expectation is a function of the symbol's realized-vol regime; restricting trades to a mid-vol quantile improves Sharpe without harming expectation.

**Mechanism:** Low-vol regimes don't generate enough directional moves to clear the 6:1 RR target (signals fire but stop out near entry). High-vol regimes whip stops faster than the strategy can capture target. Mid-vol is the Goldilocks zone where the EMA-cross thesis matches the price-action regime.

**Implementation sketch:**
- Compute 30-day realized vol per symbol, rolling.
- Map to quintile per the symbol's full historical distribution.
- Fire signal only when current quintile ∈ {Q2, Q3, Q4} (drop tails).
- Same entry/exit/cost stack as live (4H EMA9/21, 6:1 RR, side-filter short, 504h max-hold, fee=10bp, slip=5bp).

**Pre-registered verdict criterion:**
- **ADOPT** as filter if: out-of-sample Sharpe ≥ 1.2 × baseline AND annualized NET ≥ 0.85 × baseline (Sharpe gain not bought via expectation loss). Baseline = current live config on the same data.
- **REJECT** if: Sharpe < baseline OR annualized NET < 0.7 × baseline.
- **HOLD** (between thresholds): treat as exploratory finding, do not deploy.

**Why it might NOT work:** Vol regime is autocorrelated. The "improvement" might just be regime-timing — periods of mid-vol coincide with periods of trend, and we're rediscovering trend-following without admitting it. Falsification: if the result disappears when conditioning on bias regime instead of vol regime, the mechanism was confounded.

**Sub-scores:** mechanism=4, independence=4, impl=4, data=5. **Composite ≈ 4.2.**

**Priority:** HIGH

---

### A2. ATR-targeted position sizing

**Hypothesis:** Constant-stake sizing produces inhomogeneous risk per trade across symbols (a $1k stake at p50 BTC stop_dist of 0.18% has different effective risk than at a small-ALT stop_dist of 0.5%); ATR-targeted sizing equalizes risk and improves risk-adjusted return.

**Mechanism:** Currently every trade risks 1 R = stake × (stop_dist / entry). ATR-targeted sizing scales stake so that $-vol per trade is constant. Symbols with tighter stops get larger stakes; wider stops get smaller. Net effect: trade-level outcomes are homogenized, reducing the variance contribution from per-symbol stop-distance variance.

**Implementation sketch:**
- Compute ATR(14) per symbol on the signal timeframe.
- Stake per trade = target_dollar_vol / (ATR × ATR_multiplier).
- Cap stake at max_notional (e.g., 5x median) to prevent runaway leverage on quiet symbols.
- Fee/slip recomputed on actual notional per trade, not flat.

**Pre-registered verdict criterion:**
- **ADOPT** if: Sharpe improves by ≥10% AND realized fee bps stays ≤ 12bp (the deploy gate from current protocol) AND no single symbol exceeds 40% of cumulative PnL contribution (concentration kill criterion).
- **REJECT** if: Sharpe degrades OR realized fees exceed 15bp (sizing-induced fee blowup) OR concentration exceeds 50%.
- **HOLD**: between thresholds, especially if NET improves but fees creep.

**Why it might NOT work:** ATR-tight symbols (low-vol stablecoins, etc.) get massive implicit leverage. Even small adverse moves produce huge $-losses. Falsification: if the per-trade outcome distribution becomes WIDER (not narrower), the homogenization claim is wrong.

**Sub-scores:** mechanism=5, independence=5, impl=3, data=5. **Composite ≈ 4.4.**

**Priority:** HIGH (orthogonal to entry mechanism — could compose with current strategy or with any A1-style filter)

---

### B1. RSI(14) extremum reversal

**Hypothesis:** RSI < 25 generates a long-entry edge and RSI > 75 generates a short-entry edge on 4H bars in altcoin universe — a classic mean-reversion signal that's been validated in TradFi but has unknown crypto behavior under realistic costs.

**Mechanism:** RSI is a standard oscillator measuring price-velocity asymmetry over a window. Extreme readings signal exhaustion. In ranging regimes, exhaustion → reversion is high-probability. In trending regimes, RSI can stay extreme for weeks (so this fails). The question is whether crypto altcoins are net ranging or net trending in our universe.

**Implementation sketch:**
- RSI(14) on 4H closes per symbol.
- Long entry: RSI crosses up through 25 (was ≤ 25, now > 25).
- Short entry: RSI crosses down through 75.
- Stop: 1% beyond entry (no wick stop — RSI signals don't have natural wick context).
- Target: RSI crosses through 50 (mean), or 4H bars elapsed (max-hold).
- Cost stack: same as live (10bp fee, 5bp slip, funding accrual).

**Pre-registered verdict criterion:**
- **ADOPT** as an INDEPENDENT strategy (not a filter) if: 5y backtest produces NET > 0 with Sharpe ≥ 0.8 AND slip-cliff robustness (positive at slip ≤ 25bp).
- **REJECT** if: NET < 0 at slip = 5bp OR Sharpe < 0.5.
- **HOLD**: NET marginal; consider as a shadow strategy alongside current candidate.

**Why it might NOT work:** Crypto trends are persistent (BTC 2017, BTC 2021, alt season runs). Mean-reversion strategies systematically fail in trending regimes. The win-rate would have to be very high to overcome the asymmetric loss in trends. Falsification: if WR < 50% the mean-reversion thesis is broken (mean reversion needs WR > 50% by construction).

**Sub-scores:** mechanism=2, independence=5, impl=3, data=5. **Composite ≈ 3.4.**

**Priority:** MEDIUM (interesting but the mechanism prior is weak in crypto)

---

### B2. Bollinger band squeeze release

**Hypothesis:** Periods of low BB(20) bandwidth (bottom 10th percentile of the symbol's bandwidth distribution) followed by an expansion signal a momentum breakout in the direction of expansion; entering at the breakout produces a positive-expectation trade.

**Mechanism:** Distinct from the existing BB(20, 2.0) shadow strategy (which trades bounces off the bands). This idea trades the SQUEEZE-then-RELEASE pattern: vol contracts, energy builds, vol expands → directional move. The mechanism is well-known in TradFi as the Bollinger squeeze.

**Implementation sketch:**
- BB(20, 2.0) per symbol on 4H.
- Compute bandwidth = (upper - lower) / mid.
- Track 6-month rolling bandwidth distribution per symbol.
- Squeeze trigger: bandwidth ≤ p10 of the distribution.
- Release entry: after squeeze, first 4H close that breaks above upper (long) or below lower (short).
- Stop: opposite band at entry. Target: 6:1 RR.

**Pre-registered verdict criterion:**
- **ADOPT** as INDEPENDENT strategy if: 5y NET > 0 at slip=5bp AND Sharpe ≥ 0.8 AND >40% of universe-57 symbols positive.
- **REJECT** if: NET ≤ 0 at slip=5bp.
- **HOLD**: marginal NET, run as shadow.

**Why it might NOT work:** Squeeze-release has a definitional problem — "bandwidth ≤ p10" is parameter-sensitive. Slightly different definition (p15 vs p5) might produce wildly different results, suggesting the mechanism is mostly noise. Falsification: if results vary by >50% NET when the squeeze threshold shifts ±50% (from p10 to p5 or p15), the parameter is overfit.

**Sub-scores:** mechanism=3, independence=4, impl=3, data=5. **Composite ≈ 3.6.**

**Priority:** MEDIUM

---

### C1. Funding-rate extremum reversal

**Hypothesis:** When funding rate hits the top/bottom 5th percentile of its symbol-specific distribution (extreme positioning by leveraged longs or shorts), trading AGAINST the prevailing direction is positive-expectation. This is distinct from Cat F1 (funding cross — directional move) and Cat F2 (co-cross). This is a THRESHOLD trade on absolute funding extremum.

**Mechanism:** Extreme positive funding → market is heavily long; squeezes are imminent. Extreme negative funding → market is heavily short; bounces are imminent. The thesis: extreme funding signals over-crowded positioning, and reversion is high-probability.

**Implementation sketch:**
- Read funding rate per symbol from `data/funding/{SYMBOL}.csv`.
- Compute symbol-specific 1-year rolling distribution.
- Long entry: funding rate ≤ p5 (extreme negative).
- Short entry: funding rate ≥ p95 (extreme positive).
- Stop: 1.5 × ATR(14) opposite direction. Target: funding returns to median, or 504h max-hold.
- Cost stack: standard 10bp fee + 5bp slip.

**Pre-registered verdict criterion:**
- **ADOPT** as INDEPENDENT strategy if: 5y NET > 0 at slip=5bp AND Sharpe ≥ 0.8 AND result holds when re-run on Bybit cross-exchange data (Cat X-style replication).
- **REJECT** if: NET ≤ 0 OR Cat X replication fails.
- **HOLD**: positive but Cat X marginal.

**Why it might NOT work:** Extreme funding can persist for weeks if the crowded side actually gets squeezed (the shorts cover, funding stays positive, longs ride the move). Mean-reversion timing is the hard problem. Falsification: if the holding-period distribution is bimodal (quick reversions OR long persistence), the strategy needs a regime gate to be tradable.

**Sub-scores:** mechanism=4, independence=5, impl=4, data=5. **Composite ≈ 4.4.**

**Priority:** HIGH

---

### D1. Session filter

**Hypothesis:** Strategy expectation varies by UTC session (Asia 00:00-08, Europe 08-16, US 16-24); restricting trades to specific sessions improves execution quality (realized slip vs modeled) without damaging expectation.

**Mechanism:** Liquidity, taker-flow, and order-flow imbalance vary by session. Crypto's 24h-market is misleadingly named — most volume concentrates in US/Europe overlap (12:00-16:00 UTC). Trading outside that window may face wider effective spreads even if the fee bps are flat.

**Implementation sketch:**
- Tag every signal by UTC hour at the 4H close.
- Group hours into 3 sessions: Asia (00-08), Europe-US-overlap (08-16), US (16-24).
- For each session, compute realized P&L, fee bps, slip bps (on losers).
- Identify the session(s) with lowest realized slip and best Sharpe.
- Restrict live engine to those sessions only.

**Pre-registered verdict criterion:**
- **ADOPT** session filter if: filter improves Sharpe by ≥10% AND reduces realized slip on losers by ≥3bp AND retains ≥60% of trade count (so power isn't blown up).
- **REJECT** if: filter cuts trade count below 60% of unfiltered OR Sharpe degrades.
- **HOLD**: marginal.

**Why it might NOT work:** Our 4H signal-tf doesn't align with sessions; 4H closes happen at 04, 08, 12, 16, 20, 00 UTC, which already crosses session boundaries. The filter might just be selection on a noisy mechanism. Falsification: if random hour-based filters (e.g., "only odd hours") produce similar Sharpe gains, the result is regime selection on noise.

**Sub-scores:** mechanism=2, independence=4, impl=5, data=5. **Composite ≈ 3.5.**

**Priority:** MEDIUM-LOW (cheap to test but mechanism prior is weak)

---

### F1. Multi-timeframe ensemble vote

**Hypothesis:** Running 4H, 1H, and 1D EMA-cross strategies independently and taking a position only when ≥2 timeframes agree on direction reduces false signals enough to improve Sharpe by ≥20%.

**Mechanism:** Confluence across timeframes filters out single-TF noise. A 4H signal that disagrees with 1H is more likely a chop-pattern false-fire than a trend continuation. This is DIFFERENT from D1 (1D-as-confluence-on-top-of-4H), which was refuted in milestone 1. Here all TFs vote independently and the system takes the majority.

**Implementation sketch:**
- Generate signals on 1H, 4H, 1D in parallel for the same symbol.
- Each TF emits {LONG, SHORT, NONE}.
- Combined position = majority of {LONG, SHORT}; NONE if no majority OR all NONE.
- Sized at 1× stake regardless of vote count (don't compound the bet).
- Same RR, fee, slip as live.

**Pre-registered verdict criterion:**
- **ADOPT** if: ensemble Sharpe > best single-TF Sharpe by ≥10% AND realized fee bps ≤ 12 (no concentration in over-trading).
- **REJECT** if: ensemble Sharpe < best single-TF Sharpe.
- **HOLD**: marginal.

**Why it might NOT work:** Confluence requires INDEPENDENT signals. 4H/1H/1D EMA crosses on the same price series are HIGHLY correlated by construction (sharing the underlying trend). The "vote" might just be 4H-with-extra-steps. Falsification: if the ensemble's signal correlation matrix shows pairwise ρ > 0.8, the independence assumption is broken.

**Sub-scores:** mechanism=3, independence=3, impl=3, data=5. **Composite ≈ 3.4.**

**Priority:** LOW-MEDIUM (D1 already showed confluence is hard; ensemble is a reframing but the underlying mechanism risk remains)

---

### G1. Tick-imbalance signal

**Hypothesis:** AggTrade buy-vs-sell imbalance over rolling 60-second windows precedes price moves on a 5-15 minute horizon; using sustained imbalance as an entry trigger generates positive expectation.

**Mechanism:** Order-flow imbalance is one of the strongest signals in microstructure literature. Buy-aggressive flow precedes upward price drift on short horizons; sell-aggressive flow precedes downward. Crypto data is publicly available (we already have aggTrade polling on the engine), so this is testable.

**Implementation sketch:**
- For each 60s window: imbalance = (buy_volume - sell_volume) / total_volume.
- Long entry: imbalance > 0.7 sustained over 3 windows.
- Short entry: imbalance < -0.7 sustained over 3 windows.
- Stop: 1.5 × ATR(60s) opposite direction.
- Target: 3 × stop. Max hold: 30 minutes.
- This is a SCALPING strategy; 100s of trades per symbol per year. Cost stack: 10bp round-trip taker fee dominates.

**Pre-registered verdict criterion:**
- **ADOPT** as INDEPENDENT strategy if: 1y backtest produces NET > 0 at fee=10bp AND Sharpe ≥ 1.2 (high bar for scalping) AND survives slip-cliff stress at slip=10bp on entries (HFT-grade slippage).
- **REJECT** if: NET ≤ 0 at fee=10bp.
- **HOLD**: marginal.

**Why it might NOT work:** HFT desks eat this edge in milliseconds. Our 10s polling latency is ~10,000× slower than competitors; the signal is stale by the time we trade. Falsification: if results are positive at simulated zero-latency execution but negative at our actual ≥10s latency, the mechanism exists but is unreachable for this engine.

**Sub-scores:** mechanism=4 (well-documented), independence=5, impl=2 (new infrastructure), data=4 (need historical aggTrade, not just OHLC). **Composite ≈ 3.5.**

**Priority:** LOW-MEDIUM (high-effort relative to expected payoff for our latency profile)

---

## Composite ranking and milestone-2 selection guidance

| Rank | Idea | Composite | Priority | Notes |
|---:|---|---:|:---:|---|
| 1 | A2. ATR-targeted position sizing | 4.4 | HIGH | Orthogonal to entry — composes with anything |
| 1 | C1. Funding-extremum reversal | 4.4 | HIGH | Novel mechanism, data already in repo |
| 3 | A1. Realized-vol regime filter | 4.2 | HIGH | Filter, composes with current strategy |
| 4 | B2. BB squeeze release | 3.6 | MEDIUM | Different from existing BB shadow |
| 5 | D1. Session filter | 3.5 | MED-LOW | Cheap to test, weak prior |
| 5 | G1. Tick-imbalance | 3.5 | MED-LOW | Latency-bound for our infra |
| 7 | B1. RSI extremum | 3.4 | MEDIUM | Crypto-trend mechanism risk |
| 7 | F1. Multi-TF ensemble | 3.4 | LOW-MED | Independence assumption fragile |

**Recommended milestone-2 picks (top 5):** A2, C1, A1, B2, D1.

A2 is genuinely orthogonal — it can compose with the current live strategy AND with any new mechanism A1/B2/C1 produces. Test it first; if it improves baseline, all subsequent ideas inherit it.

After A2: run the three HIGH-priority entry-mechanism candidates (A1, C1) and one MEDIUM (B2) under realistic costs. Apply locked criteria mechanically.

The remaining three (D1, G1, B1, F1) — defer to milestone 3 unless milestone 2 has unexpected slack.

---

## Cross-references

- `results/real_money_protocol_decision_rule_2026-05-08.md` — the 4-stage real-money protocol; milestone 2 begins after STAGE_1 is reached or current strategy is killed
- `results/option_c_57sym_realistic_2026-05-05.txt` — methodology template for realistic-cost backtests
- `results/cat_x_bybit_replication_verdict_2026-05-07.md` — Cat X cross-exchange replication template (relevant for C1)
- `CLAUDE.md ## Things to NOT do during forward-paper` — discipline contract that prohibits running these now
- `scripts/realistic_sweep.sh` — the harness for executing locked verdicts at milestone 2
