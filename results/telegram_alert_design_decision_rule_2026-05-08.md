# Telegram alert design — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08. Activates when the first non-startup/shutdown alert is needed. The current `pkg/notify/telegram.go` handles only startup/shutdown messages; this rule designs the structured-alert layer on top.

## Question

Several locked rules reference "Telegram alert" as the operator-notification mechanism:
- `auto_kill_execution_decision_rule_2026-05-08.md` Phase 5 ("Telegram alert via pkg/notify/telegram.go")
- `real_money_executor_architecture_decision_rule_2026-05-08.md` Component 4 (alert on rate-limit sustained, F2 sustained network, F5 position drift)
- `stage_promotion_runbook_decision_rule_2026-05-08.md` ("Telegram alert on first-trade fill anomaly: ENABLED")
- `drift_firing_investigation_decision_rule_2026-05-08.md` (TRIAGE-C convening)

Each reference assumes a Telegram alert exists with appropriate semantics, but the design itself is unspecified. Without locking:
- **Alert fatigue:** every operational hiccup pages the operator → operator stops reading alerts → real alerts get missed.
- **Alert under-noise:** alerts only fire on KILL events → operator misses precursor patterns (e.g., drift exit-1 single firing).
- **Format inconsistency:** different alerts use different structures, formats, severity markers — operator can't pattern-match quickly.
- **Rate-limit confusion:** if drift fires repeatedly during a TRIAGE-C, do we send 100 messages?

## Locked design — three severity tiers

Every alert maps to one of three severity tiers. Each tier has locked properties: format, rate limit, expected operator response time.

| Tier | Symbol | Format | Rate limit | Operator response window | Mute hours? |
|---|:---:|---|:---:|:---:|:---:|
| INFO | ℹ | one-line + 1 fact | ≤ 10/hour | 24h | yes (00:00-08:00 local) |
| WARN | ⚠ | 2-3 lines + structured facts | ≤ 5/hour | 4h | partial (only WARN, never escalate) |
| CRITICAL | 🚨 | structured 4-6 lines + action prompt | unlimited | 30 min | NEVER muted |

**Rate-limit semantics:** if a tier exceeds its hourly cap, additional messages of the same tier within that hour are AGGREGATED into a single "N additional events suppressed" message. The aggregation message itself counts as one (so even worst-case the operator sees ≤cap+1 messages per hour per tier).

**Mute-hour semantics:** during configured local mute hours (defined in `/etc/paper-live/env` as `TELEGRAM_MUTE_HOURS_LOCAL=00:00-08:00`), INFO alerts are suppressed and queued; WARN alerts are still sent but with a `[QUIET]` prefix. CRITICAL alerts are NEVER suppressed regardless of mute hours.

## Per-event tier assignment

Every event the system can emit is assigned a fixed tier:

### CRITICAL events (🚨, unlimited rate, no mute)

These require operator action within 30 minutes. They represent SAFETY-OF-CAPITAL or SAFETY-OF-DATA events at STAGE_1+.

| Event | Source | Locked rule that fires it |
|---|---|---|
| Auto-kill candidate (drift wrapper exit 4) | `run_drift_check.sh` | `drift_wrapper_cron_decision_rule` |
| HARD KILL execution started | manual operator action | `auto_kill_execution_decision_rule` Phase 5 |
| Real-money daily-loss circuit breaker tripped (Gate B) | `BinanceLive` | `real_money_executor_architecture` Gate B |
| Position reconciliation drift detected (F5 with material delta) | `PositionReconciler` | `real_money_executor_architecture` F5 |
| Real-money first-trade fill anomaly during STAGE-promotion verification | `BinanceLive` + runbook | `stage_promotion_runbook` Phase 4 |
| Engine-level critical error (panic, unrecoverable failure) | engine slog Error | engine.go |

### WARN events (⚠, ≤5/hour, partial mute)

These require operator review within 4 hours. They represent OPERATIONAL ATTENTION events.

| Event | Source | Locked rule |
|---|---|---|
| Drift wrapper exit 1 (single firing — INVESTIGATION) | `run_drift_check.sh` | `drift_firing_investigation` TRIAGE-A or B |
| `forward_paper_status` reports verdict transition WAITING → KILL | `forward_paper_status.sh` (manual or scheduled) | `forward_paper_completion_review` if mid-review |
| post_deploy_check warning at STRICT=1 | `post_deploy_check.sh` (manual or scheduled) | various |
| Recovery event in last hour (engine restarted with in-flight position) | `post_deploy_check.sh` Section 7 | engine.go restart |
| Sustained rate-limit warnings (≥30/hour from any engine) | engine logs | F4 in `real_money_executor_architecture` (paper engine has same warning class) |
| Per-symbol SOFT pause executed | manual operator action | `per_symbol_pause_decision_rule` Phase 1 |
| Heartbeat stalled (>90s gap; the existing heartbeat warn line) | `pkg/marketdata/heartbeat.go` | engine.go |

### INFO events (ℹ, ≤10/hour, full mute)

These are FYI-level. Operator reviews when convenient (next pass within 24h).

| Event | Source |
|---|---|
| Engine started/stopped (the existing startup/shutdown notifications) | `pkg/notify/telegram.go` (current behavior) |
| Drift wrapper run completed CLEAN (exit 0) | `run_drift_check.sh` weekly cadence |
| Daily summary of trades closed across cohorts | scheduled digest (existing watchdog/digest timer) |
| New release deployed via redeploy.sh | (informational; future: integrate with deploy/redeploy.sh) |

## Locked message format

### CRITICAL message format

```
🚨 KILL CANDIDATE [drift wrapper exit 4]
firing pair: 2026-04-29 10:00 ↔ 2026-05-07 11:00
n_live: 87, last p_min: 0.0003
ACTION: review run log + verify per drift_firing_investigation TRIAGE-C
artifact: results/drift_runs/2026-05-07T11:00:00Z.log
```

Required structure:
1. Line 1: `🚨 <CATEGORY>` (≤30 chars, e.g., "KILL CANDIDATE", "DAILY-LOSS BREAKER", "POSITION DRIFT")
2. Lines 2-N: structured facts in `key: value` format, ≤5 facts
3. Line N+1: `ACTION: <imperative>` — what specifically the operator should do
4. Line N+2 (optional): `artifact:` reference to the locked-artifact location

### WARN message format

```
⚠ DRIFT FIRING (single)
metric: pnl_per_trade, p=0.0009, n_live=42
investigate per TRIAGE-A within 24h
log: results/drift_runs/2026-05-22T08:00:00Z.log
```

Required structure:
1. Line 1: `⚠ <CATEGORY>` (≤30 chars)
2. Lines 2-3: 1-3 key facts
3. Optional ACTION line for non-obvious response
4. Optional artifact reference

### INFO message format

```
ℹ engine started: imxusdt @ 2026-05-08T20:24:47Z
```

Required structure:
1. Single line: `ℹ <event>`
2. ≤120 characters total

## Implementation contract

The existing `pkg/notify/telegram.go` `Send(text)` function MUST be extended (not replaced) with:

```go
type Severity int

const (
    SeverityInfo Severity = iota
    SeverityWarn
    SeverityCritical
)

// SendStructured emits an alert with locked tier semantics: rate limiting
// per tier, mute-hour suppression for INFO/WARN, format prefix.
// Caller provides only the message body; Send adds the symbol prefix and
// applies rate limiting.
func SendStructured(severity Severity, body string) error
```

The current `Send` remains for unstructured calls (e.g., the existing startup/shutdown messages, which are pre-tier).

**Locked behaviors of the implementation:**
- Rate limiting state lives in-memory (a sliding-window counter per tier). Engine restart resets the counter — which is acceptable because the operator's rate-fatigue is per-session anyway.
- Mute hours are read from `/etc/paper-live/env` `TELEGRAM_MUTE_HOURS_LOCAL` at startup; changing them requires engine restart (acceptable; mute hours are stable config).
- A failed Telegram POST does NOT block the caller. The error is logged via `slog.Error` and dropped. Alerting is best-effort (the firing rule's locked-artifact write is the primary record; alert is the convenience).
- The `Send` and `SendStructured` functions are GOROUTINE-SAFE (multiple Stub instances + watchdogs may call concurrently). Use `sync.Mutex` around the rate-limiter state.

**NOT locked (defer to implementation):**
- Specific HTTP timeout values (calibrate during implementation)
- Specific rate-limit window precision (sliding vs tumbling — pick one when implementing)
- Whether to batch the aggregation message immediately or at next-hour boundary

## Edge cases — pre-locked

### Operator's Telegram is unreachable for sustained period (>1 hour)

Alerts continue to be ATTEMPTED. Each failed POST logs `slog.Error`. The operator catches up via log review on the next reachable session. The locked rules (firing investigation, kill execution) all reference ARTIFACTS in `results/` as primary records — Telegram is convenience, not authority.

### Multiple CRITICAL events fire within a minute

Each CRITICAL gets its own message (CRITICAL is unlimited rate). The operator may receive several rapid-fire alerts; all are necessary because each is a 30-min-response event.

### Rate-limit aggregation message arrives 50 minutes after the last suppressed event

The aggregation is "N additional events suppressed during last hour." A suppressed event from 50 min ago is still relevant context (the operator might not have read the earlier message yet). Aggregation is correct semantics even if delivery is delayed.

### CRITICAL event fires during configured mute hours

CRITICAL is NEVER muted. The operator's phone wakes them up. This is the locked tradeoff: mute hours protect operator sleep from INFO/WARN noise but CRITICAL is by definition important enough to wake.

### A locked rule's pre-reg references a tier that this design doesn't cover

E.g., a future rule references "MEDIUM tier" alerts. Migration trigger #2 (below) — re-open this rule, decide whether to add a tier or remap.

### `pkg/notify/telegram.go` is misconfigured (env vars unset)

The existing `Send` is a no-op when env vars are unset. `SendStructured` MUST inherit this behavior — silent no-op with one-time `slog.Warn` at startup if the operator forgot to configure. This matches the current `JournalPath` empty-string convention.

## Alternatives considered (rejected)

### Single-tier (just send everything)

**Considered:** simplest implementation, no rate limiting, no muting.

**Rejected because:** alert fatigue is the actual risk. After 200 INFO messages in 24h the operator stops reading; when CRITICAL arrives, it's missed. Tiered rate limiting is the standard solution.

### More tiers (5+ tiers like syslog: emerg/alert/crit/err/warning/notice/info/debug)

**Considered:** finer granularity.

**Rejected because:** for a solo-operator project, three tiers (do-now / do-soon / FYI) are sufficient. More tiers complicate event-tier assignment without adding decision-relevant distinction.

### Use a different channel (Slack, email, PagerDuty)

**Considered:** Telegram bot already exists; switching adds infrastructure.

**Rejected because:** Telegram is already wired and proven. PagerDuty would be appropriate at higher scale (multi-operator, on-call rotation) but is overkill for current scope. Re-evaluate if team grows.

### Allow operator to override tier per event at runtime

**Considered:** flexibility — operator promotes/demotes specific events.

**Rejected because:** runtime overrides defeat the locked-tier-assignment discipline. If a tier is wrong, change it via this rule's migration trigger and update all relevant rules — don't silently override.

## Migration triggers

The rule re-opens for design when:

1. **First INFO/WARN/CRITICAL alert fires in production** — review whether tier assignment matched operator response. If a CRITICAL felt routine, demote; if a WARN was missed and turned out important, promote.
2. **A new locked rule references an alert tier not in this design** — extend or remap.
3. **Operator response window is missed materially** — if CRITICAL alerts are routinely unaddressed within 30 min, either the tier is over-assigned or the response window is unrealistic; recalibrate.
4. **STAGE_3+ promotion** — real-money exposure increases; tier semantics may need tightening (e.g., WARN response window halves to 2h).
5. **Multi-operator / on-call rotation** — shared alerting needs ack/escalation semantics; this rule's solo-operator framing is insufficient.

## Cross-references

- `pkg/notify/telegram.go` — implementation lives here; `SendStructured` is the new entry point
- `pkg/notify/telegram_test.go` — tests must extend to cover tier semantics
- `results/auto_kill_execution_decision_rule_2026-05-08.md` Phase 5 — uses CRITICAL tier
- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` Component 4 — uses CRITICAL for Gate B trip + F5 drift
- `results/stage_promotion_runbook_decision_rule_2026-05-08.md` Phase 4 — uses CRITICAL for first-trade fill anomaly
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — uses WARN for TRIAGE-A/B firings; CRITICAL for TRIAGE-C escalation
- `results/per_symbol_pause_decision_rule_2026-05-08.md` Phase 1 — uses WARN for SOFT pause execution
- `pkg/marketdata/heartbeat.go` — the heartbeat-stalled WARN is referenced in the WARN tier; aligned with the existing 90s threshold
- `/etc/paper-live/env` — config for `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_MUTE_HOURS_LOCAL`
