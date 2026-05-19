# Testnet multi-symbol extension — capability pre-registration (2026-05-19)

**Status:** LOCKED 2026-05-19. Activates at deploy. Pure capability-build (Layer 2 acceleration), not a strategy decision.

## Context

`testnet-engine.service` has been running BTCUSDT in `--executor binance_live_testnet` mode since 2026-05-16 20:29 UTC. Two days uptime, clean heartbeats, lag p50 ~5s / p99 ~10s, zero signals fired (expected — BTCUSDT historical 4H short signal rate ~0.17/d AND BTCUSDT is on `persistent_losers` per `configs/symbols.yaml`).

The plumbing is validated. The remaining purpose of the testnet engine is **getting real Binance testnet fill data** to compare against the modeled fee/slip (10bp / 5bp) in the eventual completion review (`results/forward_paper_completion_review_decision_rule_2026-05-08.md` — abbreviated Layer 2 check, ±15% modeled tolerance).

At solo-BTCUSDT pace, the first fill arrives in ~7-14 days. That delays the Layer 2 close-out gate well past when forward-paper completes (~2026-07-12). **Multi-symbol testnet accelerates that gate.**

## What this change does

1. Stop `testnet-engine.service` (BTCUSDT, single-symbol).
2. Install `testnet-engine@.service` templated systemd unit (mirrors `paper-live@.service` pattern).
3. Enable two instances: `testnet-engine@kavausdt.service`, `testnet-engine@ensusdt.service`.
4. Drop BTCUSDT testnet (low signal rate + persistent loser).

## What this change does NOT do

- Does NOT change live cohort. Deployed-16 untouched, no parameter changes, no symbol additions to the live engine.
- Does NOT bias forward-paper conclusions. Testnet runs on separate creds, separate journal directory (`/var/log/paper-live/journal/testnet/`), separate executor mode. The forward-paper criteria evaluation in `forward_paper_status.sh` reads only the live journal.
- Does NOT modify any locked decision rule. The completion review thresholds for realized fee/slip apply to Layer 2 fill data — capability to obtain those data does not change the threshold or the rule.

## Why KAVAUSDT + ENSUSDT (not other symbols)

From forward-paper observation 2026-05-08 to 2026-05-18 (deployed-16, n=17 closed live trades):

| Symbol | Closed trades | Cumulative PnL | Trade rate |
|---|---:|---:|---:|
| KAVAUSDT | ≥3 | +$6,026 | ~0.30/d |
| ENSUSDT | ≥2 | +$4,871 | ~0.20/d |
| GRTUSDT | ≥2 | +$4,896 | ~0.20/d |

KAVAUSDT and ENSUSDT have the highest observed signal rates in the live cohort AND are the only winners (along with GRTUSDT). Picking these maximizes the expected fill rate.

This is NOT a deployment decision based on observed PnL — symbol pick is purely about signal/fill cadence. Per locked discipline, post-hoc PnL ranking does NOT promote symbols.

## Rate-limit math (binding constraint)

Binance USDT-M futures `GET /fapi/v1/aggTrades` weight: 20/call. Polling interval: 10s. Per-engine: **120 weight/min**.

| Cohort | Symbols | Weight/min |
|---|---:|---:|
| Live (deployed-16, REST fallback) | 16 | 1920 |
| Testnet (current, BTCUSDT) | 1 | 120 |
| Testnet (proposed, +KAVA +ENS, drop BTC) | 2 | 240 |
| **Total after change** | **18** | **2160** |
| Binance cap | | 2400 |
| Margin retained | | 240 |

WebSocket cluster is empirically unreliable for these symbols (per Bug 4) — all engines run on REST fallback steady-state. Verified 2026-05-19: `grep -c "aggTrade poll"` shows live ENSUSDT log at 2071 polls, testnet BTCUSDT at 2560 polls. The assumed-worst-case (all REST) is the actual case.

**Margin 240/min = 2 additional engines max** if WebSocket continues to fail and the rate-limit pool stays single-account. We are using exactly 2 of those 2. Headroom for further extensions: **zero without a different mechanism** (separate Binance accounts, longer poll interval, or WS recovery).

## Verification gates (this deploy)

1. `systemctl status testnet-engine.service` after stop: `inactive (dead)`.
2. `systemctl status testnet-engine@kavausdt.service testnet-engine@ensusdt.service`: both `active (running)`.
3. `grep "aggTrade poll" /var/log/paper-live/testnet-kavausdt.log /var/log/paper-live/testnet-ensusdt.log` within 60s: non-zero count (engines polling).
4. `grep "ERROR\|CRITICAL" /var/log/paper-live/testnet-*.log` within 5 min uptime: zero.
5. `scripts/post_deploy_check.sh` STRICT mode for live cohort: PASS (no regression).
6. Live `forward_paper_status.sh` output unchanged in structure.

## Expected outcomes (timeline)

- **First testnet fill:** within 3-5 days at observed signal rate (vs ~7-14 days solo-BTCUSDT). Power to evaluate fee/slip thresholds: ~10-15 fills → ~30-45 days.
- **Layer 2 close-out:** likely possible alongside forward-paper completion (~2026-07-12) instead of weeks later.

## What signals abort

- Sustained rate-limit 418/429 responses in any engine's log → revert to solo-BTC or reduce poll cadence.
- ANY change in live cohort's behavior post-deploy (lag tier degradation, trade-rate change, ERROR count spike) → revert immediately. Testnet must be fully isolated from live.
- Any drift detector firing → unrelated to this change but still triggers normal kill investigation.

## Reversal

```bash
ssh root@178.105.24.230 'systemctl stop testnet-engine@kavausdt.service testnet-engine@ensusdt.service && \
    systemctl disable testnet-engine@kavausdt.service testnet-engine@ensusdt.service'
```

If reverting fully to solo-BTC:
```bash
ssh root@178.105.24.230 'cp /opt/trading-engine/deploy/systemd/testnet-engine.service.bak /etc/systemd/system/testnet-engine.service && \
    systemctl daemon-reload && systemctl enable --now testnet-engine.service'
```

(Backup of the original non-templated unit is preserved as `.bak` during deploy.)

## Audit reference

This change is consistent with:
- `real_money_executor_architecture_decision_rule_2026-05-08.md` — Layer 2 testnet is part of the locked promotion path; multi-symbol does not change the architecture.
- `forward_paper_completion_review_decision_rule_2026-05-08.md` — Layer 2 fill data feeds the abbreviated cost-stack check; faster data accumulation strengthens the gate.

This change is NOT a deviation from:
- "Don't add symbols" (CLAUDE.md, forward-paper guidance) — that applies to LIVE deployed-16. Testnet is a separate cohort whose purpose IS Layer 2 capability.
- "Don't promote shadows" — testnet engines run `--executor binance_live_testnet`, not promoted from any shadow.
- "Don't tune parameters" — same `--signal-tf 4H --target-rr 6.0 --side-filter short --max-hold-hours 504` as live + current testnet.
