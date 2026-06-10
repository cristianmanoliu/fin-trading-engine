# #25 Failed-pump cascade short — FINAL VERDICT: **KILLED** (F6 full-history re-run; original PASS was a data-truncation artifact). (2026-06-10)

> **SUPERSEDED SAME-DAY by the F1–F6 follow-ups (locked pre-reg:
> `results/failed_pump_followup_decision_rule_2026-06-10.md`).** The original PASS below
> was real arithmetic on truncated data: `data/listing/klines` holds only ~30–60 days
> post-listing per symbol (it was fetched for #12), so the "732-perp 2020–2026 universe"
> was in fact ONLY listing windows. The F6 full-history re-fetch + re-run (726/732
> symbols, fapi.binance.com) shows the general strategy LOSES: primary cell n=3,898,
> mean −0.99%, t=−3.05, 0/7 years positive; ex-first-90d slice worse (n=2,973, −1.33%,
> t=−3.79, 0/7). The +2.38% edge exists ONLY inside the first 90 days post-listing —
> i.e., it is the dead #12 new-listing family conditioned on pump-fail, now a post-hoc
> subgroup of a significantly NEGATIVE general strategy. Locked F6 disposition applied
> mechanically: **(a) failed → lead KILLED.** Full detail in the follow-up section at
> the bottom. Original verdict preserved below for the record.

# [SUPERSEDED] Original verdict: PASS on locked bar → MARGINAL LEAD (not deployable). (2026-06-10)

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

---

## Follow-up results F1–F6 (same-day; locked pre-reg `failed_pump_followup_decision_rule_2026-06-10.md`)

| check | result | detail |
|---|---|---|
| F1 intraday re-sim | informative-only (n=32) | 1m-sim ≈ daily-sim (−4.62% vs −4.35% mean, identical 25% stop rate) → daily fills faithful; subset = old-major 2020 cohorts |
| F2 slippage stress | PASS (then moot) | breakeven 308bp RT; mean +1.08% at 200bp RT — on the truncated universe |
| F3 2026 decay | consistent (z=−1.32) | on the truncated universe |
| F4 corr to live book | PASS, −0.024 | genuine independence (77 months) — moot after F6 |
| F5 listing-age | **all 515 trades <90d** | exposed the truncation: listing-klines = 30–60d windows only |
| **F6 full-history** | **FAIL → KILLED** | full: n=3,898, mean −0.99%, t=−3.05, 0/7 yrs, drop5 −2.75%. Ex-90d: n=2,973, −1.33%, t=−3.79, 0/7. CSVs: `failed_pump_cells_fullhist_2026-06-10.csv`, `failed_pump_cells_ex90d_2026-06-10.csv` |

Fetch coverage: 726/732 symbols (3 delisted purged from the API — BDXN/BTCST/SXP — plus
2 invalid meme tickers + 1 other; 0.8% gap, biases marginally TOWARD survivors, i.e. the
true general result is at least as bad).

## Final decomposition (the actual finding)

- **First 90 days post-listing:** +2.38%/trade, t=2.36 (n=515) — the #12 new-listing
  family conditioned on pump-fail.
- **Everything after day 90:** −1.33%/trade, t=−3.79 (n=2,973) — the market punishes
  the same pattern once a symbol matures.
- A post-hoc positive subgroup inside a significantly negative general strategy is
  exactly what the multiple-testing discipline exists to reject. If the listing-window
  variant is ever reconsidered, it must be pre-registered FRESH as a #12-family
  candidate at milestone-2 — carrying this decomposition as its prior, plus the thin
  drop5 (+0.75%), the negative 2026 listing cohort, and #12's original kill reasons.

**Batch-3 final tally is therefore 5/5 NO-GO.** The milestone-2 leads remain #15 + #16
only.
