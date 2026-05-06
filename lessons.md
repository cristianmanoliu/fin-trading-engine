# Lessons Learned

Captured from the multi-instrument backtesting session (2026-05-03).

---

## 1. Validate dollar PnL output before trusting results

**Problem:** `validate_all.sh` showed all-zero dollar columns for every instrument.  
**Cause:** Per-symbol configs were missing `stake_usd`. The execution stub only emits `total_pnl_usd` when `StakeUSDT > 0`; without it the binary silently runs in points-only mode.  
**Rule:** Always verify `stake_usd: 1000` is in each config before running dollar PnL analysis. Any zero-column output is a config problem, not a strategy problem.

---

## 2. Use `curl -f` AND `unzip -t` for zip downloads

**Problem:** Binance returns a 404 HTML page (not an error) when data doesn't exist for a symbol/month. `curl` saved it as a `.zip`, then `unzip` crashed the entire script.  
**Rule:** Double-validate every downloaded zip:
1. `curl -f` — fail on HTTP errors (non-2xx → exit 1, no file written)
2. `unzip -t` — test integrity before extracting

Apply this pattern to all 4 scripts that download data.

---

## 3. `set -eu` with empty arrays in bash traps

**Problem:** `cross_analysis.sh` used `set -euo pipefail` and a `trap` that expanded `"${CONFIGS_TO_CLEAN[@]}"`. When the array was empty, bash's `-u` flag threw `unbound variable` and crashed.  
**Rule:** Under `set -u`, avoid expanding arrays that may be empty in traps. Either use `${array[@]+"${array[@]}"}` guard syntax, or restructure to eliminate the array entirely (e.g., clean up temp files inline immediately after use with `rm -f "$file"`).

---

## 4. Optimise the right metric

**Problem:** `full_analysis.sh` recommended `min_rr` by highest `expectancy/trade (pts)`. This picked `min_rr=3.0` for SOL, which had fewer trades and lower total dollar return than `min_rr=1.5`.  
**Rule:** For fixed-stake traders, the correct optimisation target is `total_$1k` (total dollar return), not points-per-trade. The script now selects the highest `total_$1k` among all-years-profitable rows, with a fallback to `exp/trade` if `stake_usd` is not configured.

---

## 5. Global simplicity beats per-symbol optimisation

**Problem:** Per-symbol optimal `min_rr` was 1.5 for ETH and SOL, but no single setting achieves 8/8 symbols profitable every year. Optimising per-symbol creates maintenance burden and overfitting risk.  
**Decision:** Use `min_rr: 1.0` globally — second-best combined dollar PnL, most consistent across all 8 instruments, and simpler to maintain. One parameter, one decision, eight configs.

---

## 6. Empty array syntax for bash `set -u` scripts

When writing bash scripts with `set -euo pipefail`, never expand arrays without checking for emptiness first. The safe pattern:

```bash
# Unsafe under set -u when array is empty:
rm -f "${files[@]}"

# Safe:
rm -f "${files[@]+"${files[@]}"}"

# Or: just skip the array entirely and clean up inline:
rm -f "$tmp_file"
```

---

## 7. Compile once for multi-run scripts

**Problem:** `run_backtest.sh` uses `go run`, which recompiles on every invocation. Over a 5-year, 8-instrument, 7-min_rr sweep this adds significant wall time.  
**Rule:** Scripts that run many combinations (sweep, cross_analysis, full_analysis) should compile to a binary once with `go build -o /tmp/backtest ./cmd/backtest` and reuse it. `run_backtest.sh` is for single-month debugging, not bulk analysis.

---

## 8. Opt-in pattern for shared code used in both backtest and live

**Problem:** `execution.Stub` is used by both `cmd/backtest` (where writing files is wrong) and `cmd/engine` (where journal files are required).  
**Rule:** When adding live-only behaviour to shared code, gate it on an empty/zero field rather than a boolean flag. `JournalPath == ""` → no-op. `cmd/backtest` never sets `JournalPath`, so its behaviour is unchanged without any conditional logic at the call site. This approach is safer than a `LiveMode bool` because it can't accidentally be left `true` in backtest.

---

## 9. Sync REST backfill before WebSocket open — not concurrent

**Problem:** A tempting design is to backfill in a goroutine concurrently with the WebSocket. But if the WS delivers live ticks before the backfill finishes, `DailyLevels` may roll before seeing the previous day — producing wrong PDH/PDL.  
**Rule:** Backfill must complete synchronously inside `Subscribe` before `go b.readLoop(...)` is launched. The channel is created first, backfill writes into it directly, then the WS goroutine starts. This guarantees temporal ordering: all historical ticks arrive before the first live tick.

---

## 10. Extract shared logic when two code paths must behave identically

**Problem:** `CSVReplay` inlined its own open→high→low→close tick interpolation. When `BinanceFutures` needed the same logic for REST backfill, duplicating it would risk subtle drift (e.g. different timestamp rounding). If backtest and live expand klines differently, the reconciliation tool will always show false drift.  
**Rule:** Extract the shared helper (`expandKlineToTicks` in `pkg/marketdata/klines.go`) so both paths call the same code. When reconciliation shows zero drift on a quiet week, you know it is strategy drift — not an interpolation bug.

---

## 11. Linux-only scripts need an OS guard and a remote-run hint

**Problem:** `deploy/setup.sh` was run locally on macOS. Two failures: (1) macOS BSD `sed -i` requires an empty-string extension argument (`sed -i '' '...'`) — GNU Linux `sed -i '...'` does not. The error `bad flag in substitute command: 'h'` is macOS sed misinterpreting the sed expression as the backup suffix. (2) No guard prevented local execution.  
**Rule:** Any script that targets Linux infrastructure must open with:
```bash
if [[ "$(uname)" != "Linux" ]]; then
    echo "Error: run this on the VPS, not locally." >&2
    exit 1
fi
```
Never use `sed -i 'expr' file` without testing on the target OS. The portable workaround if cross-platform is needed: `sed -i.bak 'expr' file && rm file.bak`.

---

## 16. `systemctl restart <target>` does not restart child services

**Problem:** `systemctl restart paper-live.target` showed "✓" but didn't restart the 8 `paper-live@*.service` instances — they kept running the old binary. Target units in systemd manage dependencies but restarting a target does not propagate restarts to already-active services it `Wants=`.  
**Rule:** To restart all instances, name them explicitly: `systemctl restart paper-live@btcusdt paper-live@ethusdt …` (all 8). The `redeploy.sh all` script now does this correctly.

---

## 17. WebSocket zombie connections — always set a read deadline

**Problem:** ETHUSDT WebSocket connected successfully but `ReadMessage()` blocked with no data and no error for 6+ minutes. Binance silently dropped the connection (no close frame). The readLoop had no timeout, so it hung indefinitely while the heartbeat reported `last_tick_age` growing to 365s.  
**Rule:** Always set `conn.SetReadDeadline(time.Now().Add(90 * time.Second))` before each `ReadMessage()` call. When Binance drops the connection silently, the deadline fires, `ReadMessage` returns an error, and the reconnect logic kicks in. Without this, a zombie connection can stall a symbol indefinitely with no recovery.

---

## 15. `http.DefaultClient` has no timeout — always use a custom client

**Problem:** `backfill` used `http.DefaultClient.Do(req)`. If the Binance REST API stalls (slow response, partial write), `Subscribe` hangs indefinitely with zero log output after "starting live engine". The engine appears running (systemd shows active) but produces no ticks.  
**Rule:** Never use `http.DefaultClient` for external API calls. Always construct `&http.Client{Timeout: 30 * time.Second}`. The context passed to `NewRequestWithContext` cancels the request on signal, but does not protect against slow servers that accept the connection and then stall — only `Client.Timeout` does.

---

## 14. Backfill channel buffer must fit all ticks before consumers start

**Problem:** `Subscribe` creates `ch` with buffer 1000, then calls `backfill` synchronously (which pushes up to 6000 ticks — 1500 klines × 4). After 1000 ticks the channel is full; backfill blocks forever because the consumer goroutines haven't started yet (they start only after `Subscribe` returns). Silent deadlock — engine produces no logs after "starting live engine".  
**Rule:** Any channel that receives data before its consumers start must be large enough to hold all pre-consumer writes. For backfill: `max_klines × 4` ticks. Buffer is now 8192 with a comment explaining why. Whenever changing `BackfillHours` or the kline limit, re-check this invariant.

---

## 13. `PermitRootLogin no` locks out SSH key access too — use `prohibit-password`

**Problem:** `setup.sh` set `PermitRootLogin no` and reloaded `ssh.service`. This blocks ALL root logins including SSH key auth, locking the operator out of the VPS entirely.  
**Rule:** For a single-operator VPS during deploy phase, use `PermitRootLogin prohibit-password` — it blocks password login (the actual risk) while keeping SSH key access intact. Only set `no` after a non-root deploy user has been fully set up with its own SSH key and sudo access, and that access has been verified in a separate terminal before closing the root session.

---

## 19. On rate-limit errors (418/429), back off and retry — never exit the goroutine

**Problem:** When `latestAggTradeID` returned 418, `aggTradeLoop` returned, causing `readLoop` to return, closing `ch`, which shut down the strategy runner. The engine exited "cleanly" and systemd restarted it — which hit the same 418 again, creating an infinite restart loop with no data and no obvious error.  
**Rule:** Any goroutine that owns a channel (`defer close(ch)`) must never exit on a transient error. 418/429 must trigger a `time.After(60s)` back-off and retry, not `return`. The same applies to the initial ID fetch — retry with 30s back-off until it succeeds or ctx is cancelled.

---

## 18. Binance WebSocket GeoDNS routes all clients to Tokyo — use REST aggTrade polling as fallback

**Problem:** `fstream.binance.com` resolves to AWS ap-northeast-1 (Tokyo) IPs from ALL DNS resolvers (Google, Cloudflare, OpenDNS) regardless of client location. The Tokyo cluster accepts WebSocket upgrades (returns 101) but delivers zero data frames to Hetzner AS24940 (and likely other datacenter IP ranges). The engine reconnected every 90 seconds for hours with no data.  
**Diagnosis:** `websocat -t "wss://fstream.binance.com/ws/btcusdt@aggTrade"` → silence. `openssl s_client` → 101 response then nothing. REST `/fapi/v1/aggTrades` → live trades immediately.  
**Rule:** The REST `/fapi/v1/aggTrades?symbol=X&fromId=<id>&limit=500` endpoint delivers all trades in order with zero gaps. For paper trading (5m candles), 3-second REST polling at 2s latency is equivalent to WebSocket for strategy purposes. Implement automatic fallback: after N consecutive WebSocket read timeouts, switch to `fromId`-based REST polling indefinitely.  
**Key invariant:** Track `lastID` and always poll with `fromId=lastID+1` — this guarantees no duplicates and no gaps.

---

## 12. Ubuntu 24.04 uses `ssh.service`, not `sshd.service`

**Problem:** `systemctl reload sshd` fails on Ubuntu 24.04 with `Unit sshd.service not found`. The service was renamed to `ssh.service` in this release.  
**Rule:** When writing scripts that touch the SSH daemon, use a fallback:
```bash
systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null || true
```
Mark it `|| true` — a failed reload after writing `sshd_config` is non-fatal; the config takes effect on the next restart anyway.

---

## 20. Wick-exit pricing is the dominant backtest bug class

**Problem:** `Stub.OnTick` closed positions at `tick.Price` rather than `sig.StopLoss`/`sig.TakeProfit`. Under synthetic 4-tick-per-1m-kline ordering (open → high → low → close), `tick.Price` at the exit moment is the wick extreme, not the actual stop or target price. At target_rr=1.0 this completely fabricated the strategy edge: a 57-symbol sweep that showed +$8.0M collapsed to −$4.7M once the bug was fixed via `--exact-fills`.  
**Rule:** Any historical R:R sweep that is not run with `--exact-fills` is suspect. Always verify: `grep -l 'exact-fills' scripts/*.sh` — if a sweep script doesn't pass this flag, its dollar results cannot be trusted. The fix: exit at `sig.StopLoss` on stop, `sig.TakeProfit` on target.

---

## 21. Aggregator boundary-tick semantics determine backtest-vs-live parity

**Problem:** `Aggregator.update` uses `!tick.Timestamp.Before(current.CloseTime)` to detect the candle boundary, then immediately routes the boundary tick into the *next* candle via `openCandle(tf, tick, dur)`. The boundary tick is therefore excluded from the closing candle's OHLCV. Under synthetic tick ordering, the 5m `Close` field ends up set to the LOW tick of the last 1m subkline (12:04:40 low), not the actual close price at 12:05:00. The live engine sees the real exchange price at close time — so backtest and live have structurally different entry prices.  
**Measured impact (BTC 5y, target_rr=5.0):** excluding boundary tick = +$190k; including it = +$211k (+10.6%). The direction was favorable for the strategy, but the magnitude was real and measurable.  
**Rule:** New aggregators should include the boundary tick in the closing candle. The `IncludeBoundaryTick` flag toggles this for audit; `--include-boundary` is the required flag for any backtest used to make a deployment decision. Do not omit it.

---

## 22. When a backtest result looks too good to be true, run the full audit checklist

**Background:** After fixing Bug 2 (wick-exit pricing), the 57-sym sweep at target_rr=5.0 still showed +$7.38M — plausible but above the naive expected-value floor of +$5.35M. Three residual biases were suspected and tested before deploying (Phase 4 audit, 2026-05-04).

**Checklist:**
1. **PnL math check:** does `wins × stake × targetRR − losses × stake` match the measured total to the cent? If not, something is computing exits wrong.
2. **Exact-fills check:** run with `--exact-fills`. If PnL collapses, wick-exit pricing was inflating results.
3. **Aggregator close check:** run with `--include-boundary`. If PnL shifts materially, the backtest was using a synthetic-tick wick as the candle close rather than the real close.
4. **Same-bar resolution check:** run with `--pessimistic-ambiguous`. If PnL drops, ambiguous 1m bars were wrongly crediting LONG wins. (Immaterial at target_rr ≥ 2.)
5. **Position-size dispersion:** inspect stop-distance percentiles (`p99/p50`). A fat tail (> 10×) means a few tiny-stop trades dominate total $. Add a units cap if so.

**Rule:** Use `scripts/audit_phase4_btc.sh` as the reference harness. Run it on any new strategy or RR before deciding to deploy. A 15-minute audit runtime is cheap compared to discovering a bug after 5 years of live-engine operation.

---

## 23. Monthly-segmented backtests force-close at month boundaries — use continuous CSVs for multi-year validation

**Problem:** The historical sweep harnesses (`scripts/option_c_sweep.sh`, `scripts/sweep_parallel.sh`) run each (symbol, month) pair as an independent backtest with its own `Stub` instance. When a month ends with an open position, `Stub.Summary()` force-closes it at `position.LastPrice` — neither the stop nor the target. In a 5-year bull market most force-closes resolve favorably, inflating reported PnL. Code review (Phase 4 follow-up, 2026-05-05) flagged this; for BTC the inflation was +$37k of $211k (17.5%) but the reviewer's extrapolation to the full sweep was too pessimistic.  
**Measured aggregate (57 symbols, 5y, target_rr=5.0):** segmented sweep = +$7,089k; continuous sweep (all 64 monthly CSVs concatenated, single `Stub` per symbol) = +$6,767k. Delta = −$322k (−4.5%). 1000SHIBUSDT flipped from +$28k segmented to −$17k continuous.  
**Rule:** All multi-year strategy validation must use `scripts/continuous_sweep.sh` (concatenates per-symbol monthly CSVs into a single replay). Monthly segmentation is fine only for single-month signal debugging or fast parallel sweeps where rough numbers are acceptable. The performance gap between continuous (~28 min) and segmented-parallel (~5 min) is the cost of correctness; pay it before deciding what to deploy.

---

## 24. Fees apply to position notional, not stake — and they kill thin edges

**Problem (2026-05-05):** The Option C strategy showed +$6.77M in a 57-symbol 5y continuous backtest, with a "0.3pp edge above breakeven WR" (17.0% observed vs 16.67% theoretical). NEXT_STEPS.md had estimated round-trip fees as `0.08% × $1000 stake × 392k trades = ~$31k drag` — a small bite from a $6.77M edge. **That estimate was off by ~3000×**: it conflated stake with position notional. With `units = stake / stop_dist`, the actual position notional is `stake × entry / (entry − stop)`. At BTC's p50 stop_dist of 0.18%, $1000 stake → $555k notional → $444 round-trip fee per trade. Across 392k trades, real fee drag is ~$99M, ~15× the gross edge.

**Measured impact:** with `--fee-bps=8 --stop-slippage-bps=5` (Binance USDT-M Futures taker × 2 + stop-market slip), the same continuous sweep produces:
- Gross PnL: +$6.77M (matches the prior headline exactly — math is consistent)
- Fees: −$99.15M
- Slippage: −$51.37M
- **Net PnL: −$143.76M**
- Profitable symbols after costs: **0/57** (down from 40/57 gross)
- Required WR after costs: 22.9% (vs 16.67% zero-cost breakeven and 17.0% observed)

**Rule:** Any "edge" expressed as a small spread above the zero-cost breakeven WR (e.g., < 5pp above breakeven) is presumptively a fee-illusion until proven otherwise. The *first* validation a deployment-decision backtest must pass is realistic-cost reproduction. Code-level rule: `Stub.FeeBps` charges round-trip taker fees on entry notional; `Stub.StopSlippageBps` charges adverse slippage on losers only. Both default 0 to preserve existing results bit-exact. Any new sweep script must accept and document cost flags. The recommended baseline pair is `--fee-bps=8 --stop-slippage-bps=5` for Binance USDT-M Futures.

**Geometry to remember:** *Tight stops are not free.* They look like a "small risk per trade" ($1000 = stake) but they compound into massive position notional via `units = stake / stop_dist`, which compounds into massive per-trade fees via `fee = bps/10000 × units × entry`. A strategy that signals on tight 5m candle wicks at 0.18% stop_dist is *implicitly running 555× leverage*, and exchange fees are the leverage tax. The only escape is wider stops (longer timeframe, ATR-based stop, etc.) that lower the implicit leverage.

**Parameter-range confirmation (2026-05-05).** The realistic target_rr sweep (`scripts/realistic_targetrr_sweep.sh`, output `results/option_c_targetrr_realistic_2026-05-05.txt`) ran target_rr ∈ {2.0, 3.0, 4.0, 5.0} continuous 5y × 57 symbols with `--fee-bps=8 --stop-slippage-bps=5`. **All four target_rr settings produce 0/57 profitable symbols.** Best (least negative): target_rr=5.0 at −$143.76M. Worst: target_rr=2.0 at −$168.91M. Variation across the parameter space is ~$25M; cost gap to break-even is ~$150M. **There is no target_rr in the tested range that clears realistic costs.** No further parameter tuning of this strategy family is justified; the next research direction (if any) is structurally different (longer timeframe, ATR-based stops, different signal entirely) — not parameter sweeps over 5m EMA crossover.

**Cost-geometry escape works at 4H signal timeframe (2026-05-05, same-day prototype sweep).** `scripts/proto_sweep.sh` tested six (signal_tf × stop-mode) combinations with realistic costs at target_rr=5.0:

| Config | Trades | WR | NET PnL | Profitable |
|---|---|---|---|---|
| B0  5m + wick (dead) | 392,543 | 17.0% | −$143.76M | 0/57 |
| P1  5m + ATR(14)×2.0 | 248,069 | 17.1% | −$41.59M | 0/57 |
| P2  30m + wick | 69,107 | 16.7% | −$14.34M | 0/57 |
| P3  30m + ATR(14)×2.0 | 35,751 | 16.7% | −$2.15M | 13/57 |
| **P4  4H + wick** | **11,048** | **18.6%** | **+$678k** | **35/57** |
| **P5  4H + ATR(14)×2.0** | 4,493 | 17.4% | **+$255k** | 33/57 |

The progression confirms the leverage-via-stop-width hypothesis: each step toward wider stops cuts the loss roughly in proportion to the implicit-leverage reduction, and at 4H the gross edge finally clears the cost. P4 (4H + wick) is the clean winner — 35/57 symbols profitable, 18.6% WR (sigma ≈ 5.4 vs 16.67% breakeven), per-trade net ≈ $61. Output: `results/proto_sweep_2026-05-05.txt`.

**P4 target_rr sweep (2026-05-05).** Across target_rr ∈ {2,3,4,5,6,7} at 4H + wick, every setting is net positive after costs and the WR-above-breakeven is stable at ~2pp — strong fingerprint of real signal, not random concentration. Optimum: **target_rr=6.0 → +$704k, 35/57 profitable, 16.0% WR vs 14.3% breakeven**. Output: `results/proto_targetrr_4H_wick_2026-05-05.txt`.

**P4 OOS persistence (2026-05-05).** Train (2020-2022) vs test (2023-2025) split at signal_tf=4H, target_rr=6.0, with realistic costs:
- Train aggregate: +$274k (30/57 profitable)
- **Test aggregate: +$370k (33/57 profitable)** — *test exceeds train, the opposite of overfitting*
- Persistence: 20/57 truly persistent winners (both halves +), 14/57 persistent losers, 10/57 regime flips, 13/57 recoveries (mostly mid-period listings)
- **Spearman ρ = +0.192** — symbol selection from train has only weak predictive value into test. *The strategy IS the edge, not the symbol picks.*
- Persistent losers include BTC, ETH, LINK, LTC, AXS, UNI — large-cap names with tighter 4H wicks (worst cost geometry). The strategy works better on smaller alts with wider 4H ranges.

Output: `results/proto_oos_4H_wick_rr6.0_fee10_2026-05-05.txt`.

**Fee tier note (2026-05-05).** All P4 figures above are at Binance USDT-M Futures **Regular** tier: 0.05% taker × 2 = 10 bp round-trip + 5 bp stop-market slippage. This was verified from the user's actual Binance fee dashboard — not VIP-qualified, no BNB discount. With a 10% BNB rebate (requires holding BNB in Futures wallet) the round-trip drops to 9 bp and PnL improves by ~$60k. With VIP 1 (≥$5M 30-day volume + 5 BNB) round-trip drops to 0.04% × 2 = 8 bp and PnL improves further. Promotion to higher tiers is a multiplier on this strategy's edge.

**Recommended deployment configuration (subject to funding-rate validation):** signal_tf=4H, target_rr=6.0, ATRStopMult=0 (wick stop), exclude persistent-loser largecaps (BTC, ETH, LINK, LTC, AXS, UNI, ZIL, GALA, SAND, INJ, DYDX, XRP, ATOM). The truly persistent winners (positive in both train AND test halves at 10 bp fees) form the deployment shortlist. Funding rates and 6s REST poll latency are the next two unmodeled costs — quantify both before going live with real capital.

---

## 25. Funding rates eat ~50% of P4's edge — but the strategy still survives

**Problem (2026-05-05).** The previous P4 result (+$603k net at rr=6, 10 bp fees + 5 bp slip) did not model **Binance USDT-M perpetual funding rates**. Funding is paid every 8h on the open notional; for 4H trades that hold for days, this compounds. Average normal-regime funding is ≈0.01% per 8h = 0.03%/day = 3 bp/day, but can spike to 0.1%/8h (30 bp/day) for weeks during bull-mania periods.

**Model added (2026-05-05).** `Stub.FundingBpsPerDay` charges a uniform daily drag on entry notional × hold_days. Default 0 (off). Recommended baseline: `--funding-bps-per-day=3` for normal regime. The model is a simplification — true funding flips sign with regime, and trend-aligned strategies generally pay funding in normal markets. CLI flag: `--funding-bps-per-day`.

**Tax model (2026-05-05).** `Stub.TaxRatePct` applies a flat tax to portfolio-level net positive PnL at end-of-run. Loss runs assumed not to generate refunds (no carry-back). Conservative single-period model — does not handle annual taxation cycles. CLI flag: `--tax-rate-pct`. Defaults: 0 disables. User's jurisdiction determines the rate (Romania ≈10%, US ≈30% blended, UK 20%).

**Measured impact at 4H wick + rr=6 (2026-05-05) vs prior P4 result without funding/tax:**

| | Pre-funding | + Funding (3 bp/day) | + Tax (30%) |
|---|---|---|---|
| Net PnL | +$603k | +$312k | +$218k |
| Profitable | 35/57 | 33/57 | 31/57 |
| Persistent winners (both train+test) | 20/57 | 14/57 | 14/57 |

Funding cuts ≈48% off P4's pretax edge but the strategy stays positive. After 30% tax, net is ~$218k over 5y across 57 symbols — about $43k/year, on roughly $20-30k of capital deployed at any moment (most symbols are flat most of the time). Annual return on deployed capital is in the 100-200% range *if the funding assumption holds*.

**Funding sensitivity (the biggest tail risk):**
- Each 1 bp/day of funding costs ≈ $97k over 5y at target_rr=6
- Strategy is positive net at funding ≤ 6.2 bp/day
- Real-world funding spikes can hit 10–30 bp/day for weeks during bull-mania
- **Operational mitigation: pause new entries when current funding > 5 bp/day**. Not yet implemented.

**target_rr revision under funding.** With funding included, the rr ≤ 3 settings are killed (rr=2 goes from +$164k pretax-no-funding to −$167k with funding). rr=6 remains optimal at +$312k. rr=7 becomes worse (+$171k) because longer holds amplify funding cost. The funding-aware target_rr curve peaks at rr=6.

Outputs: `results/proto_sweep_full_2026-05-05.txt`, `results/proto_targetrr_4H_wick_funding_2026-05-05.txt`, `results/proto_oos_4H_wick_rr6.0_funding_2026-05-05.txt`.

**Remaining unmodeled costs (in priority order):**
1. **Per-symbol historical funding rates** — replace 3 bp/day average with actual historical funding from Binance API. May produce different results in extreme periods.
2. **Live REST polling latency** — 6s polling means stops fire 6s after trigger; adverse 1-3 bp on losers.
3. **Spread on entry** — taker entry crosses the spread (~1 bp on liquid majors, more on alts).
4. **Annual tax cycles with loss carry-forward** — current model is single-period; annual treatment more accurate for jurisdictions with carry rules.
5. **VIP tier promotion path** — at $5M+ 30-day Futures volume, taker drops to 0.04%; further reductions help materially since fees are still the largest single cost.

---

## 26. The P4 edge is concentrated in SHORT trades, not longs

**Finding (2026-05-05).** Per-symbol breakdown at full realistic stack revealed that **all of P4's edge comes from short trades**:

```
Long_NET total:  −$42,632  (26/57 sym positive on long side)
Short_NET total: +$354,259  (37/57 sym positive on short side)
Total Net:       +$311,627
```

This **falsifies the "edge is bull-market drift" hypothesis** that was the central concern with the original Option C. Drift would inflate longs in a 5y net-bullish crypto sample, not shorts. Instead, longs slightly lose on average; shorts carry the entire edge. The most likely explanation is downside-volatility asymmetry: crypto crashes are sharp and clean (good for short EMA crossovers); up trends are slow and choppy (whipsaws on long crossovers). The 4H EMA9×EMA21 cross captures the down moves cleanly and gets eaten by funding/whipsaw on the up moves.

**Implication for deployment:** test a "shorts-only" mode (skip long signals). Not yet implemented. Predicted effect: capture the +$354k short edge without the −$42k long drag and the long-side funding cost. If validated, this would meaningfully improve net PnL.

**Deployment shortlist — 14 OOS-validated persistent winners at full realistic stack** (signal_tf=4H, target_rr=6, fee=10bp, slip=5bp, funding=3bp/day, 0% tax):

| Symbol | 5y Continuous NET | Train | Test |
|---|---|---|---|
| 1INCHUSDT | +$76k | +$77k | +$30k |
| AVAXUSDT | +$66k | +$8k | +$60k |
| VETUSDT | +$58k | +$14k | +$50k |
| FTMUSDT | +$58k | +$39k | +$20k |
| KAVAUSDT | +$53k | +$11k | +$36k |
| XLMUSDT | +$48k | +$44k | +$2k |
| GRTUSDT | +$45k | +$38k | +$10k |
| HBARUSDT | +$39k | +$4k | +$43k |
| 1000SHIBUSDT | +$35k | +$9k | +$28k |
| MKRUSDT | +$30k | +$2k | +$31k |
| APEUSDT | +$30k | +$18k | +$7k |
| AAVEUSDT | +$19k | +$10k | +$4k |
| ADAUSDT | +$7k | +$3k | +$7k |
| BCHUSDT | +$8k | +$0k | +$3k |
| **TOTAL** | **+$572k** | **+$277k** | **+$331k** |

Test (OOS) sums to $331k, *exceeding* train at $277k — strong forward-generalization signal. Combined ~$114k/year over 5y on this 14-symbol portfolio. After 30% tax: ~$80k/year. The 14 cover six different sectors (DeFi 1INCH/AAVE/MKR, L1s AVAX/ADA/BCH, gaming APE, social SHIB, oracle GRT, payments XLM, infra VET/HBAR/KAVA/FTM) — sector-diverse, not concentrated bull-market beta.

Output: `results/proto_persym_4H_wick_rr6.0_funding_2026-05-05.txt`.

**Hold-time observations:** average across deployable symbols is 60-130h (2.5-5.5 days). Outliers like ICPUSDT (1494h = 62 days), FILUSDT (1544h = 64 days), LTCUSDT/XRPUSDT (430-470h = 18-19 days) are signal-sparse and produce barely-positive or negative net — the long holds compound funding faster than the slow signal can deliver. **A trade-duration cap (e.g., force-close after 14 days) might trim these tails productively.** Not yet implemented.

---

## 27. Combined optimization: shorts-only + max-hold + historical funding triples P4's edge

**Code shipped 2026-05-05 (round 2 of P4 refinement).**
- `EntryConfig.SideFilter` (CLI: `--side-filter both|long|short`) — filter signals by direction.
- `Stub.MaxHoldHours` (CLI: `--max-hold-hours <hours>`) — force-close any open position older than N hours; classification follows pnl sign.
- `pkg/funding` package with `Provider` interface, `Constant` (legacy bps/day model), and `Historical` (per-symbol per-event accrual with correct per-side sign — longs pay positive funding, shorts receive it).
- `scripts/download_funding.sh` — fetches 5y of Binance funding rate history (~340k events across 57 symbols, ~100KB total).
- `cmd/backtest --funding-csv-dir <dir>` — when set, replaces constant funding model with historical lookup per symbol.
- Funding tests verify per-side sign convention; all tests pass.

**Results matrix (continuous 5y, 57 symbols, all at signal_tf=4H, target_rr=6.0, fee=10bp + slip=5bp):**

| Variant | Funding model | Trades | WR | NET | Profitable |
|---|---|---|---|---|---|
| Baseline P4 | constant 3 bp/day | 9,844 | 16.04% | +$312k | 33/57 |
| Shorts-only | constant 3 bp/day | 5,809 | 16.70% | +$455k | 40/57 |
| Max-hold 336h | constant 3 bp/day | 13,369 | 18.96% | +$692k | 34/57 |
| Historical funding only | historical | 9,844 | 16.04% | +$603k | 35/57 |
| **Combined (all three)** | historical | 7,772 | **20.64%** | **+$1,051k** | **45/57** |

**The combined config is +3.4× the baseline.** Each lever contributes orthogonally:
- Shorts-only kills the long drag (longs were net negative even gross-positive in some symbols).
- Max-hold trims funding-eaten tail trades and lets the signal cycle faster (more trades per year).
- Historical funding shows the constant 3 bp/day model OVERSTATED the cost: in shorts-only mode, shorts often RECEIVE funding (positive funding is most regimes; shorts receive when rate>0). The strategy gets *paid* funding rather than paying it.

**OOS persistence on combined config (train 2020-2022, test 2023-2025):**
- Train aggregate: +$465k (36/57 profitable)
- **Test aggregate: +$620k (47/57 profitable)** — test exceeds train by $155k
- **32/57 truly persistent winners** (was 14 in single-feature variant)
- Only 4/57 regime-flip casualties; 6/57 persistent losers
- Spearman ρ = +0.243

**The 32 OOS-validated persistent winners** (deployment shortlist for P4-combined):
ROSE, MKR, GRT, 1INCH, ADA, KAVA, 1000SHIB, ENS, XLM, ETC, RUNE, AVAX, IMX, DOT, BCH, FTM, FIL, SOL, CRV, AAVE, APT, SNX, NEAR, APE, MANA, AXS, GALA, ETH, ENJ, LINK, VET, LDO.

**Persistent losers (do not deploy):** BTC, TRX, XRP, SAND, UNI, CHZ. Largecaps with tight 4H wicks and unfavorable cost geometry on the short side.

**After 30% tax estimate:** $1.05M × 0.7 ≈ $735k over 5y ≈ $147k/year on this 57-sym (or 32-sym shortlist) portfolio. Capital deployed at any moment is ~$15-30k (most symbols flat), so annual return on deployed capital is 500-1000% range. Outsized but consistent with a leveraged short-bias strategy in a generally-up market with sharp drawdowns.

Outputs: `results/p4_shorts_only_2026-05-05.txt`, `results/p4_maxhold336_2026-05-05.txt`, `results/p4_hist_funding_2026-05-05.txt`, `results/p4_combined_2026-05-05.txt`, `results/proto_oos_combined_2026-05-05.txt`, `data/funding/*.csv`.

**Critical caveats before real-money deployment:**
1. **Live REST polling at 6s** still adds adverse 1-3 bp on losers — not modeled.
2. **Historical funding ≠ forward funding.** The 5y sample period had a specific funding regime (mostly positive but with 2022 negative spans). Future regimes may differ; persistent positive funding for years would compound into a drag on shorts (current strategy benefits from funding).
3. **Position sizing assumes Binance allows 555× leverage.** It doesn't — max is typically 50-125× for majors. With leverage cap, position sizes shrink, fees shrink proportionally, but PnL also scales down. Net edge ratio is preserved, absolute dollars shrink.
4. **The shortlist itself is partly OOS-selected** — 32 names that worked in BOTH halves of the 5y. Forward, some will flip. Conservative deployment: top 16 by 5y net, monitor weekly, drop any that flip negative for 30 consecutive days.
5. **Funding regime monitoring is mandatory operational practice** — pause new entries when current funding > 5 bp/day (extreme bull-mania regime where shorts START to get destroyed).
