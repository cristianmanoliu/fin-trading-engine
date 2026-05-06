# NEXT_STEPS — Live trial, 2026-05-05 onward

> **⚠️ 2026-05-05 (later that day) — Option C falsified by fee modeling.** The "30-day quiet period" plan below is **invalid**. Fees + stop slippage applied to the same continuous sweep flip the result from +$6.77M to −$143.76M, with 0/57 symbols profitable. The "edge is real but per-symbol selection is mostly noise" framing in the original opening paragraph is wrong: the *gross* edge is real, the *net* edge is deeply negative. The advice in this document about not tinkering for 30 days does not apply — you don't have a working strategy to leave alone. See `results/option_c_57sym_realistic_2026-05-05.txt` and lesson #24. Recommended action: stop the live engines, then run `scripts/realistic_targetrr_sweep.sh` to formally close the parameter door before deciding whether to pivot or stop.

The Phase 4 audit is done. 11 paper-live engines on `root@178.105.24.230` are running Option C (EMA9×EMA21) with `target_rr: 5.0`. The OOS persistence test (train 2020-2022 vs test 2023-2025) confirmed the **strategy edge is real but per-symbol selection is mostly noise** (Spearman ρ=+0.25, train top-16 lost 69% of its edge OOS). Forward expected PnL is roughly $1.5–2M over the next 28 months at $1k stake — not the +$6.77M 5y in-sample headline.

**The biggest risk now is over-tinkering.** This doc is the commitment you make to yourself to stop tinkering for 30 days.

---

## Pre-commit actions (before walking away from the keyboard)

Five things that take 30 minutes total and you'll regret skipping:

- [ ] **Initialize git.** `git init && git add -A && git commit -m "Snapshot: 11-symbol Option C target_rr=5.0 deploy 2026-05-05"`. After tonight's session there's no version control — fix that. Future-you running A/B comparisons will need this baseline.
- [ ] **Pick a stopping rule.** What cumulative paper-loss across the 11 engines triggers a halt + reconciliation? Suggested default: **−$80k cumulative** (~10× the worst expected single month). Write the number here once chosen: `STOPPING_RULE = $______`.
- [ ] **Set up a daily journal backup.** `crontab -e` on local machine: `0 3 * * * rsync -aq root@178.105.24.230:/var/log/paper-live/journal/ ~/backups/journal/` Or equivalent. The journal data is your only forward-OOS dataset. If the VPS dies tonight, you lose everything.
- [ ] **Verify Telegram alerts.** Check `/etc/paper-live/env` on VPS has `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`. Restart one engine and confirm a message arrives. If not, fix it now or you won't notice when an engine dies silently.
- [ ] **Calendar reminders for Day 7, Day 30, Day 60.** 2026-05-12, 2026-06-04, 2026-07-04. Without these you'll either forget or check obsessively daily.

---

## The deal with yourself: no changes for 30 days

For the next 30 calendar days you do NOT:

- Change `target_rr`, `min_rr`, or any strategy parameter
- Add or drop symbols from the deployed 11
- Modify any code in `pkg/strategy/`, `pkg/execution/`, `pkg/aggregator/`
- Run additional backtests "to check" something
- "Tune" anything

You DO:
- Watch logs for engine deaths or error spam
- Run weekly reconciliation (Day 7, 14, 21, 28)
- Compute rolling WR / PnL stats
- Read this document when you're tempted to break the rules above

The exception: if you find an actual *bug* (not a "could be better"), fix it. Bug fixes are fine. Optimization is not.

---

## Daily / Weekly / Monthly playbook

### Daily (1 minute)

```bash
ssh root@178.105.24.230 'systemctl status "paper-live@*.service" --no-pager | grep Active' | grep -v active
```

Should produce **no output**. Any output means a service died — investigate that one engine, leave the rest alone.

### Weekly (Day 7, 14, 21, 28 — 10 minutes)

```bash
# 1. Reconciliation: live vs backtest expectation
./scripts/paper_live_report.sh

# 2. Rolling stats per symbol — compute live WR over last 7 days
ssh root@178.105.24.230 'for s in btcusdt ethusdt solusdt runeusdt linkusdt xlmusdt apeusdt enjusdt tiausdt zilusdt ldousdt; do
  jq -s --arg sym "$s" "[.[] | select(.event==\"close\")] | length as \$n | [.[] | select(.outcome==\"TARGET\")] | length as \$w | \"\(\$sym): \(\$n) closes, \(\$w) wins, WR=\(\$w/\$n*100|tostring|.[0:5])%\"" /var/log/paper-live/journal/${s^^}-2026-*.jsonl 2>/dev/null
done'

# 3. Aggregate PnL across 11 engines
./scripts/paper_live_trades.sh root@178.105.24.230
```

Look for:
- **Per-symbol trade count drift** — backtest predicts ~2-5 trades/day/symbol. If ETH fired 50 trades in a week and BTC fired 2, something is wrong.
- **WR aberration** — any single-symbol rolling 7-day WR below 8% or above 28% is a flag (mean ~17%, but small sample noise is huge in 7 days; alarm only on extremes).
- **Aggregate PnL within ±2σ of expectation** — see Day 30 section below for what 2σ looks like.

### Day 30 — first real decision point (2026-06-04)

This is the first checkpoint where you decide anything. Three branches:

| 30-day live PnL | Interpretation | Action |
|----|----|----|
| **+$30k to +$120k** | In expected range (OOS extrapolation: ~$50k/month central, σ ~ $50k) | Continue. Schedule Day 60 review. |
| **−$50k to +$30k** | Below expectation but within noise band | Continue. Schedule Day 60 — extended noise window is normal for trend strategies. |
| **−$100k to −$50k** | Material underperformance | Run full reconciliation. Compare per-symbol live PnL vs backtest *for the same 30-day window*. If any single symbol is the dominant negative driver (e.g., −$60k from one), check for live-data bug. |
| **Below −$100k** | Hit stopping rule. **Halt**. | `ssh root@178.105.24.230 'systemctl stop "paper-live@*.service"'`. Then investigate before resuming. |
| **Above +$200k** | Suspiciously good | Verify it's not a single force-close win or an order-handling bug. Don't celebrate yet. |

### Day 60 — second checkpoint (2026-07-04)

By now you have 60 days of fresh OOS data — completely outside the 2020-2025 backtest window. This is when symbol-rotation decisions become defensible.

If 60-day cumulative PnL is positive and per-symbol WRs look healthy:
- Consider dropping the 3 deployed non-OOS-validated names (XLMUSDT, TIAUSDT, ZILUSDT) if their 60-day WR is < 16%.
- Consider adding HBARUSDT, BNBUSDT, IOTAUSDT, GRTUSDT, TRXUSDT (persistent winners not yet deployed). REST budget allows up to 16 engines; you have headroom.
- **Do not rotate based on rank within the 60-day window** — that re-introduces selection bias. Use absolute thresholds instead (e.g., "drop any symbol with WR < 15% AND PnL < 0 over 60 days").

### Day 90 — strategic checkpoint (2026-08-03)

Three months of live OOS data. You can now meaningfully ask:
- Does aggregate PnL match backtest extrapolation? (If yes, the strategy is real. If 30-50% off, there's drag — slippage, fees, regime drift.)
- Is the win rate stable? Plot rolling 30-day WR. Stationary = good. Declining = regime change.
- Are any symbols clear outliers in either direction?

If everything looks healthy, this is where you can start thinking about real-money — but only after a separate audit (see "real-money threshold" below).

---

## What you should genuinely worry about

**1. Trend-following strategies fail in ranging markets.** 2020-2025 was extremely trendy in crypto. If 2026 is sideways, the strategy will print red consistently — and that is *expected behavior*, not a bug. Mentally separate "lost money this month from a bug" from "lost money this month from being a trend follower in a range." If you're not prepared to weather a 3-6 month drawdown without intervention, you're not running a trend-following strategy — you're running a slot machine and tinkering with it.

**2. Win rate is the leading indicator, not PnL.** PnL is noisy at 30-day timescales (5R wins cluster temporally). Rolling WR is much more stable. **If WR drops from 17% to 15% and stays there for two weeks, the edge is gone.** Build a script that computes rolling WR weekly. Most informative monitoring you can write.

**3. The aggregate edge could decay.** Crypto trend-following has a natural lifecycle as markets mature and more participants run similar strategies. The 5y window we backtested is the past; the future may be different. After 90 days of OOS, you'll have signal on whether decay is happening.

**4. Per-symbol selection might still be wrong.** OOS test showed XLMUSDT looked great in train (+$468k) and crashed in test (−$39k). Some currently-deployed symbols may behave the same way in the next 28 months. The decision to keep them is based on the principle of "diversification beats concentration in the presence of weak persistence." If the 11-symbol portfolio loses while individual non-deployed symbols win, that principle was wrong for this regime.

---

## Real-money threshold (much later — 90+ days minimum)

Paper trading is psychologically and practically much easier than real money. If you ever consider real money, requires another full audit pass on:

- **Slippage** — paper engine assumes exact stop/target fills. Real orders gap through stops on volatile bars; actual loss can be 1.5-3× the stake. This alone can flip a thin edge.
- **Funding rates** — Binance perp futures charge ~0.01-0.1% every 8 hours on held positions. For 5R targets sometimes held 1-3 days, this is a material drag.
- **Fees (CORRECTED 2026-05-05 — was previously off by ~3000×).** 0.04% maker / 0.04% taker = 0.08% round-trip on Binance Futures, **applied to position notional, not stake**. Position notional = `stake / stop_dist`, which at p50 BTC stop_dist of 0.18% is `$1000 / 0.0018 = $555k`. Round-trip fee = `0.08% × $555k = $444 per trade`. Across 392k trades over 5y = **~$99M fee drag** at p50 BTC stop_dist; even higher across faster-moving alts. The fee/slippage code was added to the backtest on 2026-05-05 (`Stub.FeeBps`, `Stub.StopSlippageBps`) and the resulting net PnL on the same 57-symbol continuous sweep is −$143.76M. The earlier "$0.80 per trade × 392k = $31k drag" estimate that lived here was the load-bearing analytical error that allowed the strategy to look real.
- **Order rejection / partial fills** — paper engine has 100% fill rate; real markets don't.

Conservative real-money plan when ready: **start at 10% of paper stake** ($100/trade), monitor for 30 days, then scale up if reconciliation matches paper performance.

---

## Operational gotcha — three lists must stay in sync

If/when you rotate symbols, **all three of these lists must be updated together** or the watchdog will spam Telegram with false alerts:

1. `deploy/redeploy.sh` — the `systemctl restart paper-live@<symbols>` line. Determines which engines actually run.
2. `scripts/paper_live_watchdog.sh` line 11 — the `SYMBOLS=(...)` array. Determines which symbols the watchdog checks for liveness.
3. `configs/<symbol>.yaml` — must exist and have `target_rr: 5.0`, `ema_mode: true`, `stake_usd: 1000` for any symbol you add to lists 1-2.

A mismatch between (1) and (2) is what caused the false-alarm storm on 2026-05-05 — the watchdog had the original 8-symbol hardcoded list (`bnb/xrp/doge/ltc/...`) while `redeploy.sh` had been expanded to 12 (and then 11). It alerted every 10 min for ~5h overnight on the 4 "deployed-by-watchdog-not-by-redeploy" symbols. Future-you, save yourself the panic.

## Things on the roadmap but NOT urgent

These don't need to happen in the 30-day quiet period. List them here so they're not forgotten.

**From code review (none affect PnL):**
- `pkg/strategy/entry.go` — `MinRR` filter is silently ignored in EMA mode. Apply it for consistency with absorption/breakout paths.
- `pkg/aggregator/aggregator.go` — at 4H boundaries, 5m candle is processed before 4H bias updates (deflationary, not inflationary; minor).
- `pkg/execution/stub.go:261` — journal close timestamp uses `time.Now()` instead of tick timestamp. Cosmetic for live; backtest unaffected.

**From CLAUDE.md known issues:**
- `go.mod` declares `go 1.26.2` (invalid). Fix to current stable when you're next in the file.
- `CSVReplay` double-close (defer + main.go defer). Harmless but ugly.
- `go mod tidy` to clean up indirect deps.

**Strategic / future:**
- Add 3-5 more symbols (HBARUSDT, BNBUSDT, IOTAUSDT, GRTUSDT, TRXUSDT) up to 16-engine REST limit, after Day 60 if live performance looks good.
- Build a regime detector (cross-asset realized vol, BTC dominance, etc.) that pauses engines in ranging conditions.
- Per-symbol stopping rule built into the engine itself (auto-halt a symbol if 30-day rolling PnL < threshold).

---

## The single most important sentence in this document

**The session you just ran was the work. The next 30 days are a different skill: patience. Set a calendar reminder, write down your stopping rule, close the laptop, and don't open this repo until 2026-05-12 unless something is on fire.**
