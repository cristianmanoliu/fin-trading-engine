# BTC Regime-Switch (Full-Stop) — panel verdict

**Date:** 2026-06-08 (run completed 2026-06-09 00:41)
**Decision rule:** `results/regime_switch_fullstop_decision_rule_2026-06-08.md`
**Panel verdict: VOID — "no problem to solve."**

## Headline

F (cohorts whose always-short baseline FAILS Crit 2) = **0 of 9**. The locked PASS
rule makes the panel **VOID** when F is empty: the strategy class does **not exhibit
back-to-back negative years on the out-of-sample window (2023-2026)**, even with no
regime switch at all. The switch has nothing to repair.

The mechanism is NOT broken — it is non-degenerate (switch totals ≠ baseline totals,
unlike the degenerate gate) and it correctly force-closes on regime exit. It simply
solves a problem that does not exist on OOS data, **at a consistent cost to total P&L**.

## Why VOID (not a failure)

The operator's stated intolerance was **back-to-back negative years**. The pre-registered
test asked: does a full-stop BTC regime switch flip that failure FAIL→PASS across the
class? But the premise turned out false on the scored window:

**Every cohort's always-short baseline already passes Crit 2.** Per-year OOS baseline P&L:

| cohort | 2023 | 2024 | 2025 | 2026 | back-to-back red? |
|--------|------|------|------|------|-------------------|
| live (9/21) | −106,182 | +434,212 | +392,132 | +16,239 | NO (only 2023 red) |
| alt5-15-336 | −102,182 | +390,888 | +833,707 | −100,924 | NO (2023, 2026 — not adjacent) |
| alt5-15-504 | −84,755 | +394,401 | +790,558 | −2,480 | NO |
| alt5-21-504 | −242,233 | +493,586 | +706,314 | −19,114 | NO |
| alt7-14-504 | −202,415 | +355,248 | +702,753 | −66,071 | NO |
| alt10-30-504 | −94,783 | +297,908 | +376,492 | −71,183 | NO |
| alt12-26-504 | −29,748 | +292,082 | +411,575 | −54,842 | NO |
| alt21-50-504 | −50,789 | +141,037 | +277,202 | −37,823 | NO |
| bb20 | −3,376 | +594,878 | +254,633 | −127,029 | NO |

The reds cluster at **2023** (alt-bull, short-hostile) and **2026** (partial year), with
**2024 and 2025 always green** between them. No two adjacent red years anywhere in the
panel.

**The brutal consecutive-red bull years (2020-2021) are in-sample** (walk-forward training
folds), not scored OOS. On the OOS window, the 2022 crash + 2025 chop made always-short
net-positive most years. So the deployed strategy class, evaluated honestly out-of-sample,
does not have the consecutive-red-year problem the switch was built to fix.

## Switch is a net cost (Crit 1: NO for all 9)

Where the switch DOES act, it **lowers total P&L** every time — it cuts profitable
always-short exposure during non-bear BTC regimes:

| cohort | switch OOS total | baseline OOS total | delta (switch − base) |
|--------|------------------|--------------------|-----------------------|
| live (9/21) | +563,505 | +736,401 | **−172,896** |
| alt5-15-336 | +575,471 | +1,021,488 | −446,017 |
| alt5-15-504 | +584,329 | +1,097,724 | −513,395 |
| alt5-21-504 | +544,769 | +938,553 | −393,784 |
| alt7-14-504 | +546,570 | +789,514 | −242,944 |
| alt10-30-504 | +359,244 | +508,434 | −149,190 |
| alt12-26-504 | +276,126 | +619,068 | −342,942 |
| alt21-50-504 | +184,607 | +329,627 | −145,020 |
| bb20 | +0 | +719,106 | −719,106 |

Every delta negative. The switch buys protection against a risk (consecutive red years)
that did not materialize OOS, and pays for it in foregone profit. bb20's switch total is
$0 — Bollinger fires too rarely under short-only episode-slicing to produce trades.

## Non-degeneracy confirmed (the key contrast with the gate)

The prior SHORT_ONLY gate was degenerate: switch == baseline to the cent (504h positions
spanned the FLAT carve-outs). This full-stop switch is **genuinely different** — the live
non-degeneracy gate at (X=10,Y=14) showed switch −$299,209 vs continuous baseline
+$1,291,608, |diff| = $1.59M. The force-close-on-regime-exit + dwell guard (D=3) really do
change the P&L. The mechanism works; the verdict is VOID on *need*, not on *function*.

## Criteria (all cohorts identical disposition)

- **Crit 2 (PRIMARY): baseline PASS, switch PASS** — no back-to-back red either way → not in F.
- **Crit 3 (X,Y stability): PASS** — all folds picked (5,30) for every cohort (the same
  anti-overfit signature the gate test showed: X=5, Y=30 is the stable short-regime cell).
- **Crit 1 (beat baseline, reported-not-gated): NO** for all 9 — switch costs P&L.

## Caveats

1. **OOS window dependence.** VOID is specific to the 2023-2026 OOS folds. The
   consecutive-red years live in 2020-2021 (in-sample). If forward-paper or a future
   regime delivers a sustained multi-year bull, the premise could re-activate. This test
   does not say "the switch can never help" — it says "the class has no back-to-back-red
   problem to fix on the 2023-2026 OOS data."
2. **Crit 1 is gross of switching cost** but the directional conclusion (switch < baseline)
   is large and consistent — adding switch-cost would only widen it.
3. **Research-only.** No live/shadow/VPS change. Per the locked rule, a VOID produces no
   deployment action.

## Deferred follow-ups — NOT triggered

Per the decision rule §8, the close-if-profitable and tighten-stops variants run only if
force-close PASSES. It did not PASS (VOID), so they are **not run**. DoF preserved.

## What this closes

The regime-timing thread for the deployed strategy class is now thoroughly mapped across
three pre-registered tests (BTC gate, breadth gate, full-stop switch):
- **BTC gate**: +$700k POSITIVE was ~50% LONG flip; short-only degenerate.
- **Breadth gate**: NEGATIVE (Z unstable).
- **Full-stop switch**: VOID — no consecutive-red problem to fix OOS; switch costs P&L.

**Bottom line for the operator's intolerance:** on out-of-sample data the strategy class
does not produce back-to-back negative years, so no regime overlay is needed to satisfy
that constraint — and adding one (the switch) reduces returns. If consecutive-red is the
hard line, the existing always-short class already clears it OOS. The remaining real risks
are the known ones (forward-paper power floor, censoring, fee-dominated low-WR), not
regime clustering.
