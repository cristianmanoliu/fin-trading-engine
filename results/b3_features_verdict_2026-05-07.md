# Symbol-Mechanism Cross-Validation — Verdict 2026-05-07

## Question

CLAUDE.md memory: "Selection adds variance not edge per mechanism analysis —
8 ROBUST vs 9.5 expected, z=−0.52". Per-symbol skill is statistically chance-
level when measured by *backtest performance ranking*.

But maybe there's a **structural feature** of each symbol — volatility,
liquidity, funding regime, listing age — that distinguishes deployed-16 from
rejected-41 *independent of historical performance*. If yes, that would form
a mechanism-based selection rule that survives multiple-comparisons in a way
performance-based selection cannot.

## Method

For all 57 universe symbols at the deployed candidate parameters (4H short
EMA9/21 mh504 RR6 fee=10 slip=5), computed:

1. **Per-symbol annual NET** via `scripts/b3_symbol_run.sh`
2. **Per-symbol features** via `scripts/b3_features.py`:
   - Annualized daily-return σ (vol_ann_pct)
   - Mean daily USD volume (mean_daily_vol_m, in millions)
   - Mean close price (mean_price)
   - Listing age (days from first to last 1m kline)
   - Daily-return skewness, excess kurtosis
   - Funding rate stats: 8h mean, fraction positive, std

3. **Two analyses**:
   - **Group comparison**: Mann-Whitney U + Welch's t between deployed-16
     and rejected-41 per feature
   - **Cross-symbol correlation**: Pearson ρ between each feature and annual
     NET across all 57 symbols

Both analyzed under Bonferroni α = 0.05/9 features = 0.0056 per test.

## Headline result

The two analyses give **different verdicts** that need to be reported together:

### A) deployed-16 vs rejected-41 group split: NO significant feature

| feature | deployed mean | rejected mean | Δ% | MWU p | Welch p |
|---|---:|---:|---:|---:|---:|
| daily_kurt | 11.06 | 14.88 | −25.6% | 0.76 | 0.28 |
| daily_skew | 0.085 | 0.335 | −74.6% | 0.46 | 0.30 |
| funding_mean_8h | 0.0001 | 0.0001 | +2.1% | 0.59 | 0.94 |
| funding_pct_pos | 0.751 | 0.753 | −0.2% | 0.26 | 0.96 |
| funding_std_8h | 0.0004 | 0.0004 | +7.7% | 0.23 | 0.52 |
| listing_age_days | 1,952 | 1,812 | +7.7% | 0.46 | 0.19 |
| mean_daily_vol_m | 188.6 | 763.6 | −75.3% | 0.66 | 0.12 |
| mean_price | $29.5 | $1,303 | −97.7% | 0.92 | 0.28 |
| vol_ann_pct | 109.4% | 110.7% | −1.2% | 0.37 | 0.70 |

The mean-price and mean-daily-vol differences look *visually huge* (deployed
runs at $29 mean price, rejected at $1,303), but the rank-based MWU test
gives p=0.92 and 0.66 respectively — the difference is driven by a few
high-priced rejected symbols (BTC at $60k, ETH at $3k, SOL, BNB) rather
than a structural shift. The DISTRIBUTIONS are statistically indistinguishable.

**Confirms CLAUDE.md memory**: the deployed-16 selection is a chance-level
ranking, not a mechanism-driven one.

### B) Cross-universe correlation with annual NET: TWO Bonferroni-significant

| feature | ρ vs annual NET | t | p | Bonferroni-9 sig |
|---|---:|---:|---:|:-:|
| **mean_price** | **−0.435** | **3.58** | **0.0003** | **★** |
| **mean_daily_vol_m** | **−0.424** | **3.47** | **0.0005** | **★** |
| vol_ann_pct | +0.297 | 2.31 | 0.021 | (single-test only) |
| daily_kurt | −0.295 | 2.29 | 0.022 | (single-test only) |
| funding_pct_pos | +0.166 | — | — | — |
| listing_age_days | −0.100 | — | — | — |

**Two features survive Bonferroni** at α=0.05/9 across all 57 symbols:
- **mean_price**: lower-priced symbols → higher annual NET
- **mean_daily_vol_m**: lower-liquidity symbols → higher annual NET

(These two are themselves heavily correlated — high-priced symbols like
BTC/ETH have huge volume — so they're partly the same finding viewed two
ways.)

The vol_ann_pct (ρ=+0.30) and daily_kurt (ρ=−0.30) correlations are
single-test significant but fail Bonferroni — suggestive but not robust.

## Synthesis

**A and B are not contradictory.** The split *between* deployed-16 and
rejected-41 isn't driven by any feature, but features DO predict performance
across the full universe:

- The strategy works better on **small-cap, low-liquidity altcoins** —
  empirically robust at Bonferroni α=0.0056.
- This is consistent with prior memory: "mid-vol altcoins are the sweet spot."

The deployed-16 happens to be a chance-level draw from a population with
heterogeneous performance. A **mechanism-based selection rule** —
"select bottom-quartile-by-price OR bottom-quartile-by-volume from the
universe" — would survive multiple-comparisons in a way the current
performance-based selection does not.

## Verdict

**Selection adds variance, not edge — re-confirmed.** The deployed-16 are
statistically indistinguishable from the rejected-41 by any structural
feature. CLAUDE.md framing stands.

**But the universe IS heterogeneous by mechanism.** Two symbol features —
mean price and mean daily volume — predict annual NET at Bonferroni
significance. Lower price + lower volume = higher expected NET.

This opens a mechanism-grounded next step (post forward-paper validation):
formalize a small-cap-bias selection rule. Specifically, a candidate Cat
F2 hypothesis worth pre-registering for future testing:

> "Select-by-volume-quartile: rank universe by mean daily $ volume.
>  Take the bottom 16-by-volume from the universe (ignoring backtest
>  performance ranking). Walk-forward across 6 windows. Compare to the
>  current deployed-16 selection. Adopt only if mean NET ≥ 90% of current
>  with at least 4/6 wins."

This is **NOT** a recommendation to act now. The current forward-paper
window is already in flight on the deployed-16; mid-flight selection
reshuffling is forbidden by the standing rules. F2 is staged for after
the 60-day kill/promote decision lands.

## What this is worth

- Negative result on "deployed-vs-rejected has a mechanism" (good — closes
  a hypothesis cheaply)
- Positive Bonferroni-significant result on "small-cap predicts performance
  across universe" (genuinely new, didn't exist in CLAUDE.md before)
- Sets up a **future** selection rule (Cat F2) that doesn't compete with
  the current forward-paper window's degrees of freedom

## Reproduction

```bash
bash scripts/b3_symbol_run.sh        # 57-symbol per-symbol NET sweep (~1 min)
python3 scripts/b3_features.py       # feature computation + analysis
```

CSVs:
- `results/b3_symbol_net_2026-05-07.csv` — per-symbol NET
- `results/b3_features_2026-05-07.csv` — per-symbol NET + features joined
