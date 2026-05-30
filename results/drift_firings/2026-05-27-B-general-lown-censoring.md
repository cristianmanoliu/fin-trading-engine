# Drift firing investigation — 2026-05-27

**Triage tier:** B (metric-count + direction classify B; Tier-B 48h budget expired at
investigation time → escalated to C-depth cross-checks per the locked rule's time-budget clause.
Verdict path is B's directional-inconsistency branch.)
**Wrapper exit code:** 1 (single firing; second corroborating run 3d later is <7d apart → no auto-kill)
**Metrics fired:** see per-run table below
**n_live at firing:** 30 (2026-05-27) → 35 (2026-05-30 corroborating re-run)

> Investigated 2026-05-30 (idle-day session). Firing sat 63h un-triaged (forward-paper is inside the
> <60-day / <150-trade power floor where CLAUDE.md doctrine says "do not draw conclusions"), so the
> Tier-B 48h budget had expired → C-depth cross-checks applied.

## Per-run metric breakdown (cross-check 4)

Bonferroni α = 0.001 / 6 = 0.0002. ★ = Bonferroni-significant (DRIFT). · = single-test only.

| metric | 2026-05-27 (n=30) | 2026-05-30 (n=35) | reading |
|---|---|---|---|
| `mfe_r` | ★ p≈0.0000 Δ−1.36R | ★ p≈0.0000 Δ−1.31R | **persistent firer** |
| `win_pnl` | ★ p≈0.0000 Δ+$789 | ★ p≈0.0000 Δ+$789 | **persistent firer** |
| `loss_pnl_abs` | ★ p=0.0001 Δ+$50 | · p=0.0004 Δ+$42 | faded ★→single-test |
| `pnl_per_trade` | ok p=0.0038 Δ−$958 | · p=0.0004 Δ−$1017 | never crosses Bonferroni |
| `WR` | ok p=0.0412 (6.7%) | ok p=0.0193 (5.7%) | never crosses Bonferroni |
| `mae_r` | ok p=0.6843 | ok p=0.5048 | flat |

## Cross-checks

- **Operational state (post_deploy_check):** clean — 16/16 engines `active`; KAVA/ENS on the 2026-05-29
  reconciler-backoff fix build (restarted 08:48Z). Full `STRICT=1` not re-run this session; liveness
  confirmed by direct `systemctl status`.
- **Forward-paper threshold:** clean — `forward_paper_status.sh` VERDICT WAITING; LIMBO resolution rule
  returns **CONTINUE** (n_trades_live < 50 power floor); not KILL, no single-criterion FAIL. Realized
  fee/slip n/a (paper mode; pre-decomposition closes show 0 — PASS by construction).
- **Funding-CSV freshness:** clean (for traded symbols) — deployed-16 funding fresh on VPS (newest
  2026-05-27, 3d). The 41 CSVs stale >14d are **non-deployed** symbols; `funding_refresh_cron` scope is
  deployed-16 by design. (Initial *nit* — VPS `funding_refresh.log` tailing empty — investigated and
  resolved: daily logrotate truncates the live log at 00:00; the Sunday-03:00 cron is healthy, syslog
  confirms 05-17 + 05-24 runs, output preserved in `funding_refresh.log.1`. Not a confounder.)
- **(B+) Per-metric directional consistency:** **CONTRADICTORY** → ambiguous. `mfe_r` DOWN (−1.3R,
  degradation-signed) but `win_pnl` UP (+$789, improvement-signed). A coherent "strategy degraded" story
  would show low MFE *and* low win PnL together; it doesn't.
- **(B+) Symbol concentration:** **BALANCED** — closed losses spread across 13 symbols; largest single
  contributor ETCUSDT −$4,325 ≈ 13% of gross loss (well below the >50% concentration flag). The only two
  closed winners are 1000SHIBUSDT (+$4,714) and ENSUSDT (+$2,742). Total closed PnL −$24,699 over n=35.
  Not symbol-specific.
- **(B+) Recent code/deploy events (14d):** clean — every commit is infra/deploy/shadow/rename
  (reconciler 418/429 backoff = Layer-3/testnet; +5 Cat-A shadows; Go module rename; launchd/path fixes).
  **Zero changes touch the live strategy economics** (EMA cross, `--side-filter short`, `target_rr 6.0`,
  wick stop, max-hold). No restart/recovery cluster on the live path.
- **(B+) Exchange-side changes:** not formally checked (Binance announcements). No known fee-schedule /
  contract-spec / funding-formula change; low prior. Flagged for re-check only if the firing recurs ≥7d out.
- **(C only) STAGE-promotion paused:** N/A — zero real-money stages active (earliest STAGE_1 ~2026-08-07).

## Decisive context — winner-censoring (why the firing is low-power)

The strategy is **6:1 R:R with 336–504h max-hold**: losers stop out fast, winners ride for days toward
+6R. At forward-paper day ~21 the **closed** sample is therefore loser-censored by construction. Live state
at investigation:

- **8 open positions, ALL green** (+0.85R … +4.16R, none near stop) — these are the embryonic winners
  still riding; they are *absent from the closed sample* the detector reads.
- The two Bonferroni firers are exactly the censoring-distorted metrics: `mfe_r` reads low because the
  high-MFE trades haven't closed yet, and `win_pnl` / `WR` (5.7% = **2 winners**) are degenerate on a
  2-trade winning subsample.
- Expected WR 20.6% → ~7 wins/35; observed 2 closed + 8 riding open. If ~half the open positions convert,
  realized WR lands near the 14.3% breakeven — ordinary low-n variance, not degradation.

## Decision

**Verdict:** continue per cadence.

**Rationale:** Three locked criteria converge on low-power-noise, not degradation: (1) directional
inconsistency (`mfe_r`↓ vs `win_pnl`↑) → rule's ambiguous branch = "continue per cadence"; (2) the only
persistent firers are the metrics most distorted by 6:1-RR winner-censoring at n=35 (2 closed winners,
8 winners still open); (3) every confounder cross-check is clean (operational, forward-paper CONTINUE,
funding fresh for traded symbols, no live-strategy code change, no symbol concentration). The corroborating
re-fire 3d later is "benign-but-corroborating" per the locked multiple-firings-within-7d edge case — it does
**not** trip auto-kill (firings 3d < 7d apart; wrapper correctly returned exit 1, not 4) and is expected
under the censoring hypothesis. No cadence intensification (the every-3-4-day path requires *directional
consistency*, which fails here) — that also avoids FP-budget inflation.

**Auto-kill clock:** the 2026-05-27 firing remains the armed anchor. The next firing that is **≥7 days
after 2026-05-27 (i.e., ≥2026-06-03)** trips the two-firings rule (wrapper exit 4). The next scheduled
weekly run (~2026-05-31) is still <7d from the anchor → a firing there would again be benign-corroborating;
the first decision-grade ≥7d-apart watch run is **~2026-06-07**.

**Next wrapper run scheduled:** next weekly (launchd Sunday 09:00). No manual off-cadence re-run before then.

## Audit trail

- Run logs: `results/drift_runs/2026-05-27T17:44:44Z.log`, `results/drift_runs/2026-05-30T06:10:51Z.log`
- History index: `results/drift_check_history.jsonl` (2 DRIFT_FIRED entries 3d apart; rule not tripped)
- Cross-check transcripts: this session (forward_paper_status WAITING/CONTINUE; VPS funding newest 05-27;
  per-symbol closed-PnL aggregate above; `git log --since="14 days ago" -- pkg/ cmd/ deploy/`)
- Governing rule: `results/drift_firing_investigation_decision_rule_2026-05-08.md`
- Edge case applied: "Multiple firings within 7 days (NOT ≥7d apart)" + B "directional inconsistency"
