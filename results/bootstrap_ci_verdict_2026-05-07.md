# Block Bootstrap CI — Verdict 2026-05-07

## Question

The deployed-16 candidate's CI in CLAUDE.md was [-$111k, +$372k]/yr — derived
from N=6 walk-forward quarters. Two natural questions:

1. With N=2,210 individual trades, can we get a tighter, more robust CI?
2. If yes, *should we use it* — or does walk-forward capture variance that
   trade-level resampling misses?

## Method

Stationary bootstrap (Politis-Romano) on chronologically-sorted trade NETs.
Resamples consecutive blocks of mean length `L` so within-block dependence
is preserved. Run at L ∈ {1, 5, 25, 47=√N, 100, 250, 500} × 5,000 resamples.
Cross-checked against parametric IID baseline.

Sample: 2,210 trades over 5.28 years (2020-01-20 → 2025-04-30) at the
deployed cost stack (fee=10bp, slip=5bp, funding=historical, max-hold=504h).

## Findings

### 1. Per-trade autocorrelation is real but short-lived

| lag |   ρ    |
|----:|-------:|
|   1 | +0.323 |
|   5 | +0.099 |
|  10 | -0.017 |
|  25 | +0.005 |
|  50 | -0.011 |

Effective sample size from `1 + 2·Σρ_k`: ~1,230 trades from 2,210 actual
(ESS ratio = 56%, CI inflation ×1.34). Lag-1 dominates — consecutive trades
across the fleet correlate moderately, then independence by lag-10.

### 2. Bootstrap CI converges around L=47-250

| L | median | 2.5% | 97.5% | half-width | P(>$50k) | P(>$100k) |
|---:|------:|------:|------:|------:|------:|------:|
|   1 | $130k | +$84k | +$175k | $46k | 100% | 89% |
|   5 | $130k | +$60k | +$203k | $72k |  99% | 80% |
|  25 | $129k | +$47k | +$211k | $82k |  97% | 77% |
|  47 ← √N | $130k | +$42k | +$217k | $88k | **97%** | **75%** |
| 100 | $129k | +$40k | +$221k | $91k |  96% | 74% |
| 250 | $129k | +$42k | +$223k | $90k |  96% | 74% |
| 500 | $130k | +$53k | +$209k | $78k |  98% | 77% |

L=1 ≈ parametric IID (independence assumption baked in). CI widens with L
until L ≈ 100, then plateaus. L=500 narrows again — too few effective
blocks (~4 per resample of length 2,210), bootstrap distribution becomes
lumpy. The honest plateau CI is ≈ **[+$42k, +$220k]/yr**.

### 3. Walk-forward CI is 2.8× wider than block bootstrap

| Estimator | 95% CI annual NET | Half-width |
|---|---|---:|
| Parametric IID | [+$83k, +$177k] | $47k |
| Block bootstrap (L=√N=47) | [+$42k, +$217k] | $88k |
| Walk-forward (6 quarter-blocks, CLAUDE.md) | [-$111k, +$372k] | **$242k** |

The walk-forward half-width is 2.8× the block-bootstrap half-width.
This gap is the regime-variance component that trade-level resampling
**cannot capture**: quarter-to-quarter market regimes have correlated
PnL across hundreds of trades simultaneously, but the stationary
bootstrap's mean block length L=47 is too short to preserve that
quarter-scale dependence at the fleet level.

## Verdict

1. **Walk-forward CI [-$111k, +$372k] is the right anchor.** Bootstrap
   confirms its tightness was *not* an artifact of N=6 — the quarter-level
   regime variance is a real source of uncertainty that trade-level data
   alone cannot characterize.

2. **Parametric IID CI [+$83k, +$177k] is overconfident** by a factor of
   ~1.34 from autocorrelation alone, ~5× from regime variance.

3. **Bootstrap probability statements are NOT decision-grade**:
   - Bootstrap P(>$50k) = 96.5% sounds like deployment-ready
   - Walk-forward-implied P(>$50k) (Gaussian fit to mean=$130k, σ≈$123k)
     ≈ 74% — much closer to a coin-flip on the honest threshold
   - Use walk-forward, not bootstrap, when reasoning about deploy-readiness

4. **Updated framing** for go/no-go decision:
   - Point estimate: ≈ +$130k/yr (deployed-cost-stack) or ≈ +$69k/yr (honest at slip=25bp)
   - Honest CI: walk-forward [-$111k, +$372k]/yr
   - The 60-day forward-paper window is statistically expected to land
     anywhere in [-$18k, +$61k] (60d pro-rated) with 95% probability —
     a *negative first 60 days is well within model expectations*

## Implications

- **Don't read short forward-paper losses as alpha decay**. Both bootstrap
  and walk-forward say a 60-day window can be negative purely from regime
  luck. The kill-switch criterion "first 60 days net-negative" is a
  *trigger* for further investigation, not a refutation by itself.
- **Don't read short forward-paper wins as confirmation either**. The same
  variance that allows negative outcomes allows lucky-window positive ones.
  A single profitable 60-day window is necessary but not sufficient.
- **CLAUDE.md "Validation status" line updated** to reference both CIs and
  the bootstrap-vs-WF gap.

## Reproduction

```bash
bash scripts/hod_journals.sh                  # produces per-trade JSONL
python3 scripts/bootstrap_ci.py               # bootstrap + parametric + comparison
# Raw output cached in results/bootstrap_ci_2026-05-07.txt
```
