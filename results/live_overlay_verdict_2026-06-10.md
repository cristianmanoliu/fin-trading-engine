# Candidates #10 (vol-sizing) + #6 (OI gate) — overlays on the LIVE book — VERDICT: both NO-GO (2026-06-10)

**Type:** verdict (pre-registered, applied to the live-config trade stream → reject). Combined doc — both operate on the deployed short book rather than adding a new signal.
**Candidates:** #10, #6 of the 20-candidate search (`results/strategy_candidates_2026-06-10.md`).
**Scripts:** `scripts/gen_live_journals.sh` (live-config journals) + `scripts/vol_sizing_overlay_study.py` (#10) + `scripts/oi_gate_study.py` (#6).
**Data:** 3,491 live-config short trades across the deployed-20, full history (ema_mode true, target_rr 6.0, side short, max-hold 504, 4H, fee10/slip5, funding-accrued) — the actual deployed strategy's backtest trade stream.

## #10 — vol-targeted sizing overlay (the one price-only-EXEMPT candidate)

Re-size each trade to target constant risk: weight ∝ 1/stop_distance (the live wick stop = a vol read). 3,491 trades.

| sizing | ann.Sharpe |
|---|---|
| **BASELINE (as-traded $-risk)** | **0.809** |
| VOLTGT (w ∝ 1/stop_dist) | 0.725 |
| INVNOTIONAL (w ∝ 1/notional) | 0.377 |

**NO-GO — VOLTGT (0.725) is WORSE than baseline (0.809).** The principled reason: the live config sizes by `stake/stop_distance` (fixed $ risk), which **already normalizes by the wick width = a vol read**. Re-weighting by 1/stop_dist double-counts the vol adjustment and actively hurts; INVNOTIONAL is worse still. There is no Sharpe left for a sizing overlay to capture — the deployed sizing is already vol-aware by construction. (Reassuring: confirms the live sizing is sound. Baseline 0.809 matches the project's known ~0.8 deployed-book Sharpe.)

## #6 — OI-confirmed gate on the live EMA cross

Filter the live shorts by OI trend at entry (ΔOI over prior 4h). 2,830 trades with OI coverage (metrics window 2021-2026).

| gate | n | retain | Sharpe | net$ | WR |
|---|---|---|---|---|---|
| BASELINE (all) | 2830 | 100% | 1.361 | 453,037 | 21% |
| OI rising (>0) | 1197 | 42% | 1.448 | 282,299 | 21% |
| OI falling (<0) | 1632 | 58% | 1.337 | 171,845 | 21% |
| OI rising >2% | 264 | 9% | 0.041 | 18,526 | 19% |
| OI falling >2% | 515 | 18% | 1.202 | 633 | 22% |

**NO-GO.** The best qualifying gate ("OI rising", 42% retained) lifts Sharpe 1.361→1.448 (**+0.087**, below the +0.2 bar) while halving trade count and cutting net $453k→$282k. Directionally sensible (new shorts piling in = continuation) but not a meaningful improvement, and the extreme variants collapse n and edge. OI confirmation adds no real edge to the live cross — the confluence-filter history holds (spec prior: gates disappoint; prior price-filter gates were −23/−59%, regime-gate F=0/9).

## Verdicts

- **#10 vol-sizing overlay: NO-GO** — live $-risk sizing is already vol-aware; re-weighting hurts.
- **#6 OI gate: NO-GO** — +0.087 Sharpe at 42% retention, below the bar; gates disappoint.

## Cross-references

- Spec: `results/strategy_candidates_2026-06-10.md` (#10, #6)
- Live-journal generator (reusable): `scripts/gen_live_journals.sh` → results/live_journals/
- OI-direction prior: `results/oi_lsratio_verdict_2026-06-10.md` (#1 NO-GO)
- Gate skepticism: `project_regime_gate_shortonly_degenerate` memory (F=0/9)
