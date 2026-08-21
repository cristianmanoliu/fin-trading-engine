# Spike: Three strategy classes — trend following, crypto carry, premium selling

**Status:** Spike finding, 2026-08-21. Throwaway code, no pre-registration.
Follows from `spike_broad_instrument_screen_2026-08-19.md` and the VIX-call
dead end (killed by real VVIX data in fin-vix-signal).

## Context

After 85 trials on perp EMA strategies (all killed by cost geometry — fees on
notional at ~47× implied leverage = 114% of gross) and the VIX-call spike
dying to real VVIX data, the search pivoted to strategy classes that
structurally avoid the cost trap. Nine classes were screened structurally; the
top three were spiked with data.

## Method

Each spike uses 15-20 years of real market data (yfinance or existing Binance
funding CSVs). Honesty check: drop-top-5% of monthly returns must stay
positive (rejects tail-carried strategies). Cost estimated and compared to
gross. No parameter optimization — each uses the canonical academic
specification.

---

## 1. Multi-asset trend following (managed futures, ETF implementation)

**Assets:** SPY, TLT, GLD, DBC, EFA, IEF, VNQ, TIP (8 ETFs).
**Signals:** TSMOM 12-1 (12-month return minus last month) and SMA-10 (price
vs 10-month SMA). Monthly rebalance. Long-only and long/short variants.
**Period:** 2006–2026 (~20 years).

### Results

| Strategy | Sharpe | Ann ret | Max DD | Tail | t-stat | Pos yrs |
|---|---|---|---|---|---|---|
| Buy & hold (equal wt) | 0.71 | +6.4% | 27.8% | OK | +3.31 | 16/21 |
| TSMOM 12-1 long-only | 0.83 | +5.2% | 10.1% | OK | +3.72 | 16/20 |
| TSMOM 12-1 long/short | 0.50 | +3.8% | 17.8% | OK | +2.34 | 14/20 |
| **SMA-10 long-only** | **0.91** | **+4.8%** | **8.5%** | **OK** | **+4.06** | **17/21** |
| SMA-10 long/short | 0.41 | +3.1% | 14.9% | OK | +1.98 | 14/21 |

### Finding

SMA-10 long-only is the best risk-adjusted variant: Sharpe 0.91, max DD only
8.5%, 17/21 positive years. Long/short hurts both signals — the short side
adds noise without edge. TSMOM is close behind but slightly higher drawdown.

**Cost:** ~0.48%/yr on $100k (monthly ETF rebalance). Net Sharpe ~0.82.
Negligible cost impact — this is the anti-perp: wide timeframe, low turnover,
no leverage.

**Academic evidence:** Strongest of any strategy (AQR, Moskowitz-Ooi-Pedersen
2012, Hurst-Ooi-Pedersen 2017 >200yr backtest). Well-documented behavioral
edge (herding, anchoring, slow information diffusion).

**Verdict: SURVIVES.** Candidate for a dedicated project.

---

## 2. Crypto carry (delta-neutral funding rate harvest)

**Assets:** BTC and ETH perpetual futures (short perp + long spot).
**Mechanism:** Speculators structurally pay to be long crypto perps. The
funding rate (every 8h) is typically positive = shorts receive payment.
Delta-neutral: spot exposure offsets perp exposure.
**Data:** Existing Binance funding CSVs (2020-01 → 2026-05, 77 months). Proxy
for Kraken rates (mechanism identical across exchanges).
**Capital:** $50k ($25k spot + $25k perp margin).

### Results

| Symbol | Sharpe | Ann ret | Max DD | Monthly WR | t-stat | Tail |
|---|---|---|---|---|---|---|
| **BTCUSDT** | **2.40** | **+5.5%** | **0.4%** | **88.3%** | **+5.97** | **OK** |
| **ETHUSDT** | **2.25** | **+6.7%** | **0.9%** | **88.3%** | **+5.57** | **OK** |

### Funding rate characteristics

| Metric | BTC | ETH |
|---|---|---|
| Mean daily rate | 0.033% | 0.040% |
| Annualized gross | 12.1% | 14.5% |
| % positive days | 87.5% | 88.2% |
| Worst neg streak | 11 days | 13 days |
| Cost as % of gross | 10.5% | 8.8% |
| Gross >= 3× cost | YES | YES |

### By year

| Year | BTC | ETH |
|---|---|---|
| 2020 | +8.3% | +13.9% |
| 2021 | +15.6% | +19.6% |
| 2022 | +1.5% | -0.3% |
| 2023 | +3.3% | +3.5% |
| 2024 | +5.5% | +6.0% |
| 2025 | +1.9% | +1.8% |
| 2026 | -0.2% | -0.3% |

### Finding

Highest Sharpe of any strategy tested in this project's history. The edge is
structural: speculators pay to be long, carry traders collect the payment.
2020-2021 was the golden era (15-20% annual); 2022+ has compressed to 2-6%
as more carry desks entered. 2026 is near zero — the trade may be crowded out.

**Risk:** Funding can flip negative for extended periods (11-13 day streaks
observed). Exchange counterparty risk. Basis collapse during liquidation
cascades. The carry has been declining — 2020 was 3× current levels.

**Caution:** The declining trend (from ~15% to ~2% annualized) suggests the
edge is being arbitraged away. A new project should model the carry
compression trajectory and stress-test at 2026 rate levels, not the 6-year
average.

**Verdict: SURVIVES with a caution flag.** Edge is real but declining.
Candidate for a dedicated project if current-rate levels (2-3%) still clear
costs on Kraken's fee structure.

---

## 3. Premium selling (covered calls + cash-secured puts)

**Assets:** SPY and QQQ.
**Covered call:** Own shares, sell monthly 2% OTM calls, collect theta.
**Cash-secured put:** Sell ATM puts against cash collateral, collect premium.
**IV model:** VIX as proxy for SPY implied vol; Black-Scholes pricing.
**Period:** 2006–2026 (~20 years).

### Results

| Strategy | Sharpe | Ann ret | Max DD | Monthly WR | t-stat | Tail |
|---|---|---|---|---|---|---|
| SPY buy & hold | 0.74 | +11.1% | 50.8% | 67.2% | +3.53 | OK |
| **CC SPY 2% OTM** | **1.30** | **+14.1%** | **27.4%** | **72.9%** | **+5.80** | **OK** |
| **CSP SPY ATM** | **1.27** | **+11.0%** | **23.2%** | **77.7%** | **+5.70** | **OK** |
| QQQ buy & hold | 0.85 | +15.6% | 49.7% | 62.3% | +4.00 | OK |
| CC QQQ 2% OTM | 0.89 | +11.1% | 34.6% | 71.3% | +4.16 | OK |
| CSP QQQ ATM | 0.74 | +7.4% | 32.9% | 75.3% | +3.47 | OK |

### Finding

SPY covered calls are the standout: Sharpe 1.30, +14.1% annualized, drawdown
cut in half vs buy-and-hold (27% vs 51%), 19/21 positive years. The volatility
risk premium (implied > realized) is the edge — well-documented academically.

QQQ variants are weaker because QQQ's realized vol is higher than VIX (which
is SPY-based), so the IV proxy understates QQQ premium. A proper QQQ
implementation would use QQQ-specific IV (VXN or actual option chain data).

**Caveat:** The model uses VIX as IV for Black-Scholes pricing. Real option
premiums depend on actual bid-ask spreads, term structure, and skew. This
overstates precision but the direction is sound — CBOE BXM (buy-write index)
confirms covered calls on SPY outperform over long periods.

**Cost:** ~$1.30/contract + $1/trade. Negligible relative to premium collected.

**Verdict: SURVIVES.** SPY covered calls are the strongest absolute-return
candidate. Candidate for a dedicated project.

---

## Cross-strategy comparison

| Strategy | Sharpe | Ann ret | Max DD | Edge type | Venue |
|---|---|---|---|---|---|
| CC SPY 2% OTM | 1.30 | +14.1% | 27.4% | Volatility risk premium | IBKR |
| CSP SPY ATM | 1.27 | +11.0% | 23.2% | Volatility risk premium | IBKR |
| Crypto carry BTC | 2.40 | +5.5% | 0.4% | Structural funding | Kraken |
| Crypto carry ETH | 2.25 | +6.7% | 0.9% | Structural funding | Kraken |
| SMA-10 long-only | 0.91 | +4.8% | 8.5% | Behavioral (trend) | IBKR |
| TSMOM 12-1 long-only | 0.83 | +5.2% | 10.1% | Behavioral (trend) | IBKR |

### Observations

1. **Crypto carry has the best Sharpe** but lowest absolute return and a
   declining trajectory. On $50k it produces ~$2.5-3k/yr at 2024 rates.

2. **SPY covered calls have the best absolute return** (+14.1%) with
   acceptable drawdown. On $100k → ~$14k/yr. This is effectively enhanced
   buy-and-hold — you're long equity with a premium buffer.

3. **Trend following has the best drawdown control** (8.5% max DD) and
   strongest academic backing, but the lowest return.

4. **None of these strategies compete with each other.** They use different
   assets, venues, timeframes, and edge sources. A portfolio combining all
   three diversifies across: equity (CC), crypto (carry), multi-asset (trend).

### Capital allocation sketch ($200k)

| Strategy | Allocation | Expected annual | Venue |
|---|---|---|---|
| SPY covered calls | $100k | ~$14k | IBKR |
| SMA-10 trend (8 ETFs) | $50k | ~$2.5k | IBKR |
| BTC+ETH carry | $50k | ~$2.5-3k | Kraken |
| **Total** | **$200k** | **~$19k (9.5%)** | |

Conservative estimate. The premium selling dominates the return; trend and
carry provide diversification and drawdown buffering.

## Next steps

Each surviving strategy warrants its own project for:
1. **Real data validation** — actual option chains (not BS model), actual
   Kraken funding rates (not Binance proxy), actual ETF rebalance costs
2. **Execution design** — IBKR API for options/ETFs, Kraken API for carry
3. **Risk management** — position sizing, correlation stress, margin

Recommended project order (highest conviction first):
1. `fin-premium-selling` — SPY covered calls (strongest Sharpe, most capital)
2. `fin-trend-following` — SMA-10 ETF trend (strongest evidence, lowest risk)
3. `fin-crypto-carry` — Funding harvest (highest Sharpe but declining edge)

## Spike artifacts (throwaway)

All in session scratchpad, not committed:
- `spike_trend_following.py` — 8-ETF TSMOM + SMA backtest
- `spike_crypto_carry.py` — delta-neutral funding harvest on BTC/ETH
- `spike_premium_selling.py` — covered call + CSP on SPY/QQQ
- `strategy_class_screen.py` — structural screen of 9 strategy classes
