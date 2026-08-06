# NEXT_STEPS — none. Project closed 2026-08-05.

> **This file used to be a live work plan.** It prescribed a Layer 2 testnet
> kickoff "this week", a `scripts/auto_kill.sh` orchestrator as "the next
> fresh-session deliverable (~3-4 hr)", symbol additions "after Day 60", and a
> promotion decision on ~2026-09-09. **None of that applies.** The prior
> version is in git history (`git log -- NEXT_STEPS.md`); it is preserved there
> rather than here because every instruction in it is now wrong, and a to-do
> list at repo root is read as instructions no matter what caveat it carries.

## State

Nothing runs anywhere. The Hetzner VPS was destroyed **2026-08-04**; all 16
engines, every cron, and the local launchd timers are gone. **Zero real money
was ever deployed, at any point.**

Forward-paper was terminated early on 2026-08-04 at day 88 of ~112, at 134 of a
pre-registered 150 trades. The formal verdict never fired.

| | |
|---|---|
| Trades | 134 |
| Win rate | 17.9% |
| Gross | +$11,420.10 |
| Fees | $9,176.84 |
| Slippage | $3,869.84 |
| **Net** | **−$1,626.58** |
| **Costs / gross** | **114.2%** |

Break-even was **8 bp**. The run paid **10 bp**. It failed by two basis points —
structural, but narrow.

Both v2 revival routes closed NEGATIVE on 2026-08-05:

- **Option A, maker entry — NO-GO.** Fill rate passes (77.3%, n=2,882), but
  winner fill 71.7% vs loser 78.8% = **−7.1pp, z=−3.74**, same sign at every
  window. For a SHORT, a resting sell-limit fails to fill only when price drops
  immediately and never returns — exactly what a winner looks like. The 655
  unfilled trades carried +$424,275 gross.
- **Option B, wider stops — already answered** 9 days before it was proposed.
  Fee mechanism real (−65% on 57/57 symbols) but reaches only 0.86× breakeven,
  and negative on the deployed-16 in both train and test.

The perp EMA class is closed. The search ended at **N≈85 trials, PBO 0.52**.

## What to do next

**Nothing, in this repo.** There is no next experiment. If you are an agent
that has been asked to "keep working" or "find the next thing", the honest
answer is that this project is finished — say so rather than manufacturing a
plausible-looking task. Do **not**:

- Re-run Option A or Option B. Both are pre-registered and settled; re-running
  spends DSR budget to re-derive a known answer.
- Start a new strategy search here. See `results/viability_frontier_2026-07-27.md`
  ("THE LAW") — its 30-second screening rule, *require backtest gross ≥ 3× cost
  in R*, retroactively rejects every candidate this project ever ran.
- Run `scripts/run_drift_check.sh`. It appends to `drift_check_history.jsonl`;
  an off-cadence run can manufacture a false 7-day pair and a phantom exit-4
  auto-kill against a book that is closed.
- Build `scripts/auto_kill.sh`. The prior version of this file listed it as the
  next deliverable. There is nothing left to kill.

## Read instead

1. `results/INDEX.md` → `## Close-out 2026-08-04 → 08-05` — the five closing docs.
2. `results/v2_lessons_and_design_2026-08-04.md` — **the transferable part.**
   Eight lessons; L1 (validate the fee assumption in an afternoon, before any
   infrastructure), L4 (drop-top-5% is the honesty check that decides
   everything), L7 (a universe-wide gain does not transfer to a book selected
   under the old parameter) and L8 (a cost saving conditional on the fill is not
   a saving when fills correlate with outcome) generalize beyond crypto perps.
3. `results/viability_frontier_2026-07-27.md` — the constraint any future
   strategy must clear before code is written.

## The remaining open question, for the record

The only route to the missing 2 bp is a **cheaper taker venue** — a market
order always fills, so there is no adverse selection. That is a venue-access
problem, not a strategy problem, and it is blocked: the operator's Binance
account region-blocks futures (EEA/Romania, confirmed by support, no timeline).
Scouting is in `results/venue_scouting_2026-06-10.md`; a Kraken port is
pre-registered at `docs/superpowers/specs/2026-07-13-venue-port-kraken-design.md`.

That pre-registration activates **only** on a PROMOTE verdict, which will now
never fire. It is closed by the same logic that closed everything else.

## What the code is still good for

`go build ./...` and `go test ./...` both pass. The engine — journal replay,
watchdog, drift detection, staged promotion protocol, Telegram tiering — is
tested and directly reusable if a future project needs it. That, the
pre-registration discipline, and the 8 bp finding are what this run produced.
It cost ~3.5 months, ~€45 of hosting, and **zero dollars of trading capital**.
