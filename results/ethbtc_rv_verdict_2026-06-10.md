# #22 ETH/BTC majors-only RV pair — VERDICT: NO-GO. (2026-06-10)

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (#22), locked grid + bar.
**Script:** `scripts/ethbtc_rv_study.py`. **Cells CSV:** `results/ethbtc_rv_cells_2026-06-10.csv`.

## Setup

ETH/BTC daily ratio 2020→2026-06 (right-edge labels per #23 lesson). Dollar-neutral
$1/$1, pair return = pos×(r_ETH − r_BTC). Costs: 15bp × |Δpos| (30bp full pair cycle)
+ actual per-leg funding (long pays, short receives, CSV). 6 locked cells.

## Results

| cell | days in pos | ann.Sharpe | mean bp/d | median bp (in-pos) | drop5 bp | total % | yrs+ |
|---|---:|---:|---:|---:|---:|---:|---|
| trend_k7 | 2304 | 0.222 | 2.9 | −1.8 | −29.5 | 67.8 | 3/7 |
| trend_k14 | 2297 | 0.349 | 4.6 | −0.8 | −27.1 | 106.0 | 4/7 |
| trend_k30 | 2281 | 0.147 | 1.9 | +1.2 | −29.5 | 44.6 | 3/7 |
| mr_z1.5 | 1565 | −0.588 | −6.6 | −3.4 | −40.7 | −152.0 | 2/7 |
| mr_z2.0 | 1262 | −0.537 | −5.5 | −5.3 | −42.2 | −127.9 | 2/7 |
| mr_z2.5 | 977 | −0.534 | −4.9 | −6.1 | −43.2 | −113.2 | 2/7 |

**Grid-mean ann.Sharpe = −0.157** (bar: > 0.8). Best cell trend_k14 = 0.349 with negative
median and negative drop-top-5%.

## Conclusion

Fails the bar by a mile, on both locked conditions. The ratio TRENDS (every MR cell loses
~−0.55 Sharpe — fading ETH/BTC extremes is structurally wrong), but the trend is too slow
and tail-driven to clear 30bp pair costs (positive mean, negative median = a few big
ratio-regime years carry it; 3-4/7 years positive). The 57-alt cross-sectional book's
death was NOT a liquidity artifact — even the cleanest 2-asset version of crypto RV has
no fee-clearing edge at daily scale. **RV/pair family closed.**
