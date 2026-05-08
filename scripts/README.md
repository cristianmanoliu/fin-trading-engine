# scripts/

Operational and analytical scripts for the trading engine. Tiered by frequency
of use — operational scripts at the top, one-off analytical scripts (each tied
to a pre-registered decision rule in `results/`) grouped by category at the
bottom.

For full operational doctrine see [CLAUDE.md](../CLAUDE.md). For the locked
deploy/kill rules see `## Forward-paper go/no-go criteria` in CLAUDE.md.

---

## Daily / Weekly Operational

These are the scripts an operator runs to verify the paper-live fleet is
healthy and to evaluate decision-grade signals.

| Script | Purpose | Cadence |
|---|---|---|
| `post_deploy_check.sh` | Operational health audit — engine state, watchdog timers, binary md5, tick freshness, rate-limit pressure, recovery events in last 24h. | After **every** `deploy/redeploy.sh`. `STRICT=1` for CI-style exit on any warning. |
| `forward_paper_status.sh` | Strategic go/no-go status against the criteria from CLAUDE.md (trade count, days elapsed, WR, fee/slip bps, BTC-HODL Δ, single-symbol concentration, currently-held positions). | Daily during forward-paper; verdict is `WAITING` / `DEPLOY-READY` / `KILL`. |
| `run_drift_check.sh` | Wraps `live_vs_backtest_drift.py` with state persistence so the locked **two-firings-≥7d-apart auto-kill rule** is mechanically evaluable across sessions. Writes to `results/drift_check_history.jsonl` + `results/drift_runs/<ts>.log`. | **Weekly** (per the time-to-detection verdict — daily testing inflates FP to 28%/year). `--quiet` for cron. |
| `paper_live_status.sh` | Local-process liveness check (heartbeat count, last trade time) for engines run via `paper_live_start.sh` rather than the VPS. | Ad-hoc when running locally. |
| `paper_live_trades.sh` | Passive trade view — wins/losses/PnL summary across the VPS journal directory. | Ad-hoc browsing. |
| `paper_live_report.sh` | Reconciliation report — compares live journals against backtest. | Ad-hoc, after enough closes have accumulated. |
| `paper_live_watchdog.sh` | Watchdog timer body (invoked by `paper-live-watchdog.timer`). Restarts engines that miss a tick deadline. | Internal — not run manually. |

## Deploy / Build

| Script | Purpose |
|---|---|
| `../deploy/redeploy.sh` | Sync + rebuild + restart on the VPS (one symbol or `all`). |
| `../deploy/sync.sh` | Sync code only — no restart. |
| `gen_configs.sh` | Generate per-symbol YAML configs from the deployed-16 list in `configs/symbols.yaml`. |
| `refresh_funding.sh` | Re-download funding-rate CSVs (use `INCREMENTAL=1` for delta-only). |
| `paper_live_start.sh` / `paper_live_stop.sh` | Local-only — launches/stops 8 background paper-live processes outside systemd. |

## Backtesting

| Script | When to use |
|---|---|
| `run_backtest.sh` | Single symbol × month — signal-level debugging. Auto-uses `configs/{symbol}.yaml`. |
| `full_analysis.sh` | Multi-year heatmap for one symbol; sweeps `min_rr`. |
| `cross_analysis.sh` | Find universal `min_rr` across multiple symbols. |
| `validate_all.sh` | All deployed-16 symbols at fixed settings — no sweep. |
| `realistic_sweep.sh` | Continuous 5y sweep with realistic fees + slippage — **the deployment-decision baseline**. |
| `realistic_targetrr_sweep.sh` | Cost-aware `target_rr` sweep (parameter falsification). |
| `walk_forward.sh` | Walk-forward validation across non-overlapping windows. |
| `run_p4_variant.sh` | Build per-symbol configs from `configs/default.yaml` + merged data and run a P4 variant. |
| `p4_oos_persistence.sh` | OOS persistence test for the P4 strategy. |
| `audit_phase4_btc.sh` | Reference harness for `--exact-fills --include-boundary` validation. |
| `continuous_sweep.sh` | Legacy gross-only sweep — kept for historical reproducibility. New deployment decisions use `realistic_sweep.sh`. |
| `monitor.sh` | Backtest progress monitor for long-running sweeps. |

## Data Acquisition

| Script | Purpose |
|---|---|
| `download_data.sh` | Download a Binance USDT-M futures kline CSV for one symbol-year-month. |
| `download_data_all.sh` | Bulk download across the deployed symbol set. |
| `download_funding.sh` | Download per-symbol funding-rate history (per-8h). |
| `hod_journals.sh` | Generate hold-out reference journals (`results/hod_journals/<date>-mfe/`) used by the drift detector. |

## Analytical (pre-registered analyses)

Each script in this section corresponds to a pre-registered decision rule in
`results/` and produces a `*_verdict_*.md` artifact. The rule is locked
**before** the script runs and the verdict applies mechanically. See
CLAUDE.md "Today's findings" + the locked-rule files for context.

**Bootstrap / cross-validation**
- `bootstrap_ci.py` — block bootstrap CI on cumulative annual NET
- `sample_split_bootstrap.py` — sample-split bootstrap (train/test independence check)
- `walk_forward.sh` / `walk_forward_compare.py` — walk-forward window sweep
- `oos_persistence.sh` / `oos_persistence.py` — out-of-sample persistence
- `fresh_oos_compare.py` — single-window OOS comparison
- `edge_stability.py` — edge stability across regimes
- `persistent_alpha.py` — alpha persistence test

**Cost / fee modeling**
- `slip_cliff.sh` — slippage cliff sensitivity
- `slip_stress_report.py` — slip stress aggregation
- `option_*_sweep.sh` — strategy option sweeps with fee modeling
- `param_sweep.sh` / `proto_sweep.sh` / `sweep.sh` / `sweep_parallel.sh` — parameter exploration

**Drift detection**
- `live_vs_backtest_drift.py` — distribution-based drift detector (decision-grade kill mechanism)
- `drift_detector_calibration.py` — locked operating-point calibration
- `drift_detector_time_to_detection.py` — time-to-detection sensitivity
- `kill_bar_calibration.py` / `kill_bar_recal.py` — threshold kill-bar calibration (advisory only after 2026-05-07)

**Mechanism analysis**
- `mechanism_analysis.py` — per-symbol mechanism breakdown
- `quarterly_mechanism.py` — regime-conditioned mechanism check
- `cat_f1_run.sh` / `cat_f1_walk_forward.sh` — Cat F1 (funding cross) analyses
- `cat_f2_cocross_analyze.py` — Cat F2 (co-cross) analysis
- `cat_g_f1xf2_composite.py` / `cat_g_prime_universe57.py` — Cat G / G' composite tests
- `cat_x_bybit_replication.py` — Cat X cross-exchange replication on Bybit
- `funding_regime_recompare.py` — funding-regime correlation re-validation

**Symbol selection**
- `select_16_engines.py` — derive deployed-16 from universe candidates
- `train_only_shortlist.py` — train-only shortlist diagnostic
- `rolling_shortlist.py` — rolling-window shortlist
- `symbol_characteristics.py` — per-symbol descriptive stats
- `why_some_symbols_work.py` — heterogeneity exploration

**Feature analysis / signal context**
- `b3_features.py` / `b3_symbol_run.sh` — B3 feature audit
- `mfe_mae_analysis.py` — MFE/MAE distribution from journals
- `hod_decompose.py` — hold-out journal decomposition

**Benchmarks**
- `btc_hodl_benchmark.py` — BTC-HODL benchmark helper used by `forward_paper_status.sh`

## Quickstart for operators

```bash
# After every deploy
./scripts/post_deploy_check.sh

# Daily check during forward-paper
./scripts/forward_paper_status.sh

# Weekly (NOT daily — sequential FP inflation)
./scripts/run_drift_check.sh
```

Reference data the analytical scripts depend on:
- `data/{SYMBOL}-{YYYY-MM}.csv` (klines) — produced by `download_data*.sh`
- `data/funding/{SYMBOL}.csv` (funding) — produced by `download_funding.sh`
- `results/hod_journals/<date>-mfe/` (drift backtest reference) — produced by `hod_journals.sh`

`lib/symbols.sh` exports the canonical deployed-16 list — sourced by every operational script via `get_symbols deployed lower`.
