# Hour-of-Day Decomposition — Verdict 2026-05-07

## Question

Does the deployed-16 candidate strategy (4H short EMA9/21 mh504 target_rr=6) show
a session-of-day effect — i.e., is its edge concentrated in a specific 4H entry
boundary (00, 04, 08, 12, 16, 20 UTC), or approximately uniform across all six?

## Decision rule (pre-registered)

A bucket counts as a *real* session effect only if **all three** hold:
1. Bonferroni-corrected significance vs overall WR (|z| > 2.64 at 5% across 6 buckets)
2. Per-symbol consistency (≥ 12 of 16 symbols positive in the bucket)
3. Per-year persistence (best/worst-hour ranking stable in ≥ 4 of 6 years)

Failing any of the three → **REJECT** the hypothesis.

## Results — full 5y, deployed-16, 2,210 trades

### Aggregate by entry hour

| hour | session                    | n   | WR%   | NET $    | mean $ | %total | z     |
|-----:|---------------------------:|----:|------:|---------:|-------:|-------:|------:|
|  00  | Asian open                 | 352 | 23.3% | +$154k   | $439   | 22.5%  | +0.49 |
|  04  | Asian midday               | 374 | 22.5% | +$121k   | $325   | 17.7%  | +0.11 |
|  08  | London open                | 290 | 21.0% | +$103k   | $356   | 15.0%  | −0.48 |
|  12  | NY pre-market              | 371 | 18.3% | +$25k    | $69    |  3.7%  | −1.80 |
|  16  | NY midday                  | 424 | 26.7% | +$209k   | $493   | 30.4%  | +2.20 |
|  20  | NY close / Asia evening    | 399 | 20.8% | +$73k    | $183   | 10.6%  | −0.68 |

Overall: 491/2210 wins (22.2% WR) | NET $687,072.

### Verdict per criterion

| | Criterion                                    | 16:00 (best)         | 12:00 (worst)        | Verdict |
|-|-:|:-:|:-:|:-:|
| 1 | Bonferroni-corrected (\|z\| > 2.64)         | z=+2.20 (FAIL)       | z=−1.80 (FAIL)       | ❌ both fail |
| 2 | ≥12 of 16 symbols same-sign as effect       | 12 of 16 positive    | 10 of 16 positive    | ❌ 12:00 fails |
| 3 | Best/worst-hour ranking stable ≥4/6 years   | best in 1/6 years    | worst in 1/6 years   | ❌ both fail |

**Best-hour-of-year tally**: 00h:1, 04h:1, 08h:2, 16h:1, 20h:1 — no hour dominates.
**Worst-hour-of-year tally**: 00h:1, 04h:1, 08h:2, 12h:1, 20h:1 — same story.

The "16:00 carries 30% of total NET" headline is dominated by **2023, the only
losing year of the sample, where 16:00 was the *only* positive hour** (+$31k vs
−$55k total). This is the kind of high-leverage cherry-pick that an OOS year
will not reproduce.

## Verdict: REJECT

The hypothesis "deployed candidate has a session-of-day edge" is **rejected**.
The 22% WR is approximately uniform across all six 4H boundaries; observed
dispersion is variance, not edge.

## Implications

- **No "skip 12:00" or "prefer 16:00" filter** should be added to the deployed
  strategy. Doing so would be data-mining a marginal result that fails all
  three pre-registered criteria.
- **Strengthens the rigor frame**: another null result on the candidate
  scoreboard. The deployed strategy's edge is structural (across all
  sessions), not regime-conditioned by time-of-day.
- **Operational use**: forward-paper trades should be expected to distribute
  roughly proportionally across the 6 entry hours (~17% each). A pronounced
  skew in live data would indicate sample bias worth flagging.
- **Diagnostic use**: if a deployment-decision-grade win streak comes through,
  expect mean PnL/trade to vary 7× across hours ($69 at 12:00 to $493 at
  16:00) by chance alone — don't read this as a "16:00 is hot" signal.

## Reproduction

```bash
bash scripts/hod_journals.sh                        # generate per-trade JSONL
python3 scripts/hod_decompose.py                    # decomposition table
# Raw output cached in results/hod_decomposition_2026-05-07.txt
```
