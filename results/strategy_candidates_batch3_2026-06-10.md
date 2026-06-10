# Strategy candidates batch 3 (#21–#25) — pre-registration (2026-06-10)

**Status:** LOCKED before any run. Written 2026-06-10 (afternoon session), committed before
first execution. Research-only: live EMA-9/21 config, VPS, and forward-paper untouched.

**Context:** The price-only search (PR≈1.9, DSR→0.001) and the 20-candidate orthogonal
search (0 deployable) are both CLOSED. This batch does NOT reopen them — it targets five
verified holes: (a) an already-open ledger item (#21 — the corrected MACD/RSI CANDIDATE
status from `alt_signals_phantom_corrected_verdict_2026-06-09.md` awaiting fresh pre-reg),
and (b) four strategy *structures* absent from both closed searches (two-sided payoff,
cross-asset conditioning, 2-asset RV, event-cascade short). Prior remains AGAINST: the
burden of proof is on each candidate, and the default outcome is NO-GO.

## Mandatory honesty block (every candidate, no exceptions)

Per `project_phase0_tail_mirage_lesson` + 20-candidate synthesis:

1. **Median net** per trade/period — not just mean/t.
2. **Drop-top-5%** retest — mean must stay positive after removing the best 5% of
   trades/periods (tail-mirage check).
3. **By-year decomposition** — ≥5/7 years positive (2020–2026), or the windowed analogue.
4. **Correct sign** — the economic story must match the measured direction.
5. **Selection correction** — results reported across the FULL locked grid (mean across
   cells), never the best cell alone. Grids are locked below; no post-hoc cells.

Costs locked per candidate below. Any candidate whose screen fails dies WITHOUT a strategy
build (pattern: #4 taker-flow). Negative results get verdict docs too.

---

## #21 — RSI-14 / MACD deep validation (settle the open ledger item)

- **Hypothesis:** The phantom-corrected positives (RSI +79% 3/3 windows, MACD +18% 2/3 vs
  live baseline) survive per-trade honesty + extended walk-forward; OR they are tail/regime
  artifacts and the ledger item closes as NO-GO.
- **Method:** Generate per-trade journals (continuous 2020→2026-06) for three cells —
  EMA-9/21 baseline, MACD-12/26/9, RSI-14 — on the deployed-20 journal universe
  (`gen_live_journals.sh` SYMS list), live cost model (fee=10bp, slip=5bp, mh504, 4H,
  short-only, funding CSV). Then: honesty block on per-trade nets; 6-window walk-forward
  (yearly windows 2020–2026); monthly-PnL correlation to baseline.
- **Grid (locked):** exactly 3 cells. No parameter tuning of RSI period or MACD periods.
- **Verdict criteria (locked):**
  - **CANDIDATE-CONFIRMED** (→ eligible for future-milestone shadow pre-reg, NOT deployment):
    vs baseline, variant has (a) positive mean AND median per-trade net, (b) mean stays
    positive after drop-top-5% of trades, (c) ≥4/6 yearly windows net-positive, (d) ≥5/7
    calendar years positive, (e) monthly-PnL corr to baseline < 0.7 OR total net > baseline.
  - **NO-GO** otherwise; ledger item closes permanently.
- **Known objection carried in:** family DSR(34)=0.658. A pass here does NOT erase the
  multiple-testing haircut — it only re-opens the question for milestone-2 with cleaner
  evidence. Stated now so a pass cannot be inflated later.

## #22 — ETH/BTC majors-only relative-value pair

- **Hypothesis:** The ETH/BTC ratio carries exploitable trend or mean-reversion at 1D scale
  that survives realistic 2-leg costs — distinct from the dead 57-alt cross-sectional book
  (which died of borrow/illiquidity/degenerate-universe confounds, not of majors-pair
  evidence).
- **Data:** BTCUSDT + ETHUSDT 1m CSVs → 4H/1D closes; funding CSVs both legs.
- **Grid (locked, 6 cells):** trend: position = sign(trailing k-day ratio return),
  k ∈ {7, 14, 30}, rebalance daily. MR: z = (ratio − SMA30) / SD30; enter |z| > 2 fade,
  exit z crosses 0, k irrelevant; cells: entry z ∈ {1.5, 2.0, 2.5}.
- **Costs (locked):** 30bp per pair round-trip (10bp round-trip + 5bp slip per leg) +
  per-leg funding from CSVs (long pays positive, short receives).
- **Verdict criteria (locked):** tradeable lead only if grid-mean ann.Sharpe > 0.8 AND best
  cell passes the full honesty block (median > 0, drop-top-5% positive, ≥5/7 yrs). Sharpe
  computed per-period, no compounding (cross-sectional hardening lesson). Else NO-GO.

## #23 — BTC→alt lead-lag spillover

- **Hypothesis:** Large BTC 1H/4H moves predict same-direction alt continuation with
  conditional forward mean exceeding cost. (Prior: heavily arbed; likely dead. Cheap to
  falsify.)
- **Method:** EVENT STUDY ONLY first (no strategy build unless screen passes). BTC trailing
  1H return z-scored on rolling 30d; events at |z| > 2 and |z| > 3. Measure alt forward
  returns (15m, 1H, 4H) conditional on event sign, all alts with data, 2020–2026.
- **Screen (locked):** |conditional mean forward return| in the BTC-move direction must
  exceed **15bp** (taker round-trip + slip) at ≥1 horizon with |t| > 3 AND survive the
  honesty block. Fail → NO-GO, no build.
- **Grid (locked):** 2 z-thresholds × 3 horizons = 6 cells, selection-corrected.

## #24 — Vol-event two-sided breakout (direction-free expression)

- **Hypothesis:** The 20-candidate search's one robust finding — gates mark WHEN 60–330bp
  moves happen, not WHICH WAY — is monetizable direction-free: bracket stop-entries both
  sides at gate-fire, ride the side that fills. Profitable iff post-fill continuation
  beats whipsaw + double-fee.
- **Gates (locked, zero-fetch):** (a) macro events (CPI/FOMC/NFP calendar from #14),
  (b) OI z-spike |z|>2 (data/metrics, 20 syms), (c) funding-settlement EXTREME_LOW window
  (the #11 gate — strongest pre-window move). One gate-family at a time, no compositing.
- **Mechanism (locked):** at gate fire, brackets at ±0.75×ATR(4H,14) from spot; first
  touch fills; stop = opposite bracket; exit at +2R or 24h, whichever first. Simulated on
  1m closes, deployed-16 universe.
- **Costs (locked):** 15bp round-trip on the filled leg (10bp fee + 5bp slip); unfilled
  bracket costs nothing (stop-entry never hit). Whipsaw (fill → stop at opposite bracket)
  pays full cost + 1R loss — this is the honest killer and must be reported.
- **Verdict criteria (locked):** mean AND median net per event > 0, drop-top-5% positive,
  ≥5/7 yrs, across ≥2 of 3 gate families (one gate alone = likely artifact). Else NO-GO.

## #25 — Failed-pump cascade short (H3 from milestone-2 addendum)

- **Hypothesis:** Alt pump-and-fail sequences cascade downward hard enough to clear fat
  alt-short costs. Short-only, matches live book geometry.
- **Data:** de-survivorship universe — `data/listing/klines` daily bars, 732 ever-listed
  perps + `data/listing/funding` (Phase-0 lesson: survivor-only listing studies are
  mirages; de-survivorship mandatory).
- **Definition (locked):** pump = close/close_2d_ago − 1 ≥ P, P ∈ {25%, 50%}. Failure
  trigger = first subsequent day closing below the prior day's low. Entry next open.
  Exit: −33% of entry-to-stop distance gain (target), stop above pump high, or 7d max
  hold. Simplified daily-bar fills (open/close only, no intrabar) — stated limitation.
- **Costs (locked):** 70bp round-trip (35bp/side — cross-sectional hardening lesson for
  illiquid alt shorts) + funding bleed (shorts during post-pump often pay: use actual CSV).
- **Grid (locked):** 2 pump thresholds × 1 trigger × 1 exit = 2 cells.
- **Verdict criteria (locked):** honesty block (median > 0, drop-top-5% positive, ≥5/7
  listing-era years, correct sign) AND mean net per trade > 0 with t > 2 in BOTH cells or
  in the pre-specified primary cell (P=25%, the higher-n cell). Else NO-GO.

---

## Execution order (locked)

#21 (background, longest Go compute) → #23 (cheapest screen) → #22 → #25 → #24 (heaviest
sim). Each candidate: study script in `scripts/`, verdict doc
`results/<name>_verdict_2026-06-10.md`, atomic commit. Batch synthesis at end.

## What a survivor earns

Nothing live. A pass = a documented lead for milestone-2 pre-registration (same status as
#15/#16). No shadow additions, no deployment, no live-config edits from this batch —
charter freeze respected.
