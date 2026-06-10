# #25 Failed-pump cascade short — VERDICT: PASS on locked bar → MARGINAL LEAD (not deployable). (2026-06-10)

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (#25), locked grid + bar.
**Script:** `scripts/failed_pump_study.py`. **Cells CSV:** `results/failed_pump_cells_2026-06-10.csv`.

## Setup

De-survivorship universe: ALL 732 ever-listed USDT-M perps (daily klines + funding,
`data/listing/`). Pump = +25%/+50% over 2 days; failure = first close below prior day's
low within 10d; short next open; stop above pump high; target 1R capped at −33%; 7d max
hold; same-day stop-before-target (pessimistic); 70bp RT + actual funding on the short.
Delisting mid-trade = forced exit at last close (kept — that IS de-survivorship).

## Results (after funding-join fix)

| P | n | syms | mean | median | WR | t | drop-top-5% | funding/trade | yrs+ | stopped |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|
| **25% (primary)** | 515 | 373 | **+2.38%** | **+6.74%** | 62.5% | **2.36** | **+0.75%** | −0.20% | **5/7** | 15.5% |
| 50% | 175 | 149 | +2.03% | +7.66% | 65.1% | 1.02 | +0.35% | −0.60% | 4/7 | 12.6% |

Yearly means (P=25%): 2020 +2.1% · 2021 +3.2% · 2022 −5.6% · 2023 +2.0% · 2024 +5.5% ·
2025 +2.6% · 2026 −2.5%. **Ex-2025 (the 43%-of-trades listing-boom year): mean ≈ +2.2%
over 294 trades — the edge does NOT depend on 2025.**

**Locked criteria: ALL PASS in the primary cell** (median>0, drop5>0, 5/7 yrs, correct
sign, mean>0 with t>2). Secondary cell underpowered (n=175, t=1.02) — bar explicitly
allowed primary-cell pass.

## Audits performed (believe-nothing protocol)

1. **Funding join was silently broken in run 1** (`pd.Series(series, index=ts)` REINDEXES
   → empty join → funding ≡ 0). Fixed with `.values`; real bleed −0.20%/trade shaves mean
   2.58→2.38% and drop5 1.68→0.75%. The #12 funding-bleed killer is now IN the number.
2. **No look-ahead:** stop/trigger use only data through trigger day; entry next open.
3. **No single-symbol concentration:** 373 distinct symbols / 515 trades.
4. **Honest payoff shape:** median >> mean (left-tail squeeze losses drag) — the OPPOSITE
   of a tail-mirage; drop-top-5% stays positive.
5. **Daily-bar limitation:** fills at daily open/stop/target only; intrabar path unknown.
   35bp/side charged, but panic-tape slippage on illiquid alts is the main unmodeled risk.

## Why MARGINAL LEAD, not deployable

- **drop5 cushion is thin** (+0.75% on top of costs already charged) — half the locked
  honesty margin came out with the funding fix alone; the next unmodeled cost (panic
  slippage) could take the rest.
- **2026 YTD negative** (−2.5%, n=40) — possible decay; needs the forward window.
- **Capacity tiny:** ~0.26 trades/day across 732 perps; at $1k stakes ≈ $2-3k/yr. Only
  matters at larger stakes, exactly where illiquid-alt slippage bites.
- **Daily-bar sim** — needs 1m-data confirmation on the tradeable subset before any
  real consideration.

## Status

Recorded as a **milestone-2 lead** alongside #15/#16: first PASS of batch 3, with the
explicit follow-up requirements (intraday re-sim on liquid subset, slippage stress, 2026
forward check). NO shadow, NO deployment, NO live-config change — charter freeze applies.
