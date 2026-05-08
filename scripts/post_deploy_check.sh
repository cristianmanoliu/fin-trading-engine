#!/usr/bin/env bash
# post_deploy_check.sh — operational health audit after every redeploy.
#
# Run after deploy/redeploy.sh to verify that:
#   1. All deployed engines are systemctl-active
#   2. Watchdog timers are armed
#   3. Each engine is receiving live tick data (heartbeat shows recent activity)
#   4. No ERROR-level logs in last 5 min
#   5. (Optional) verifies binary md5 matches local — confirms the deployed
#      binary was actually built from current code, not a stale cached build
#
# Usage:
#   ./scripts/post_deploy_check.sh                          # check default VPS
#   ./scripts/post_deploy_check.sh root@host                # different host
#   STRICT=1 ./scripts/post_deploy_check.sh                 # exit 1 on any FAIL
#
# Exit codes:
#   0 = all healthy
#   1 = any check failed (only when STRICT=1; otherwise always 0 with warnings)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="${1:-root@178.105.24.230}"
STRICT="${STRICT:-0}"

source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS_LC=$(get_symbols deployed lower)

echo "════════════════════════════════════════════════════════════════════════════════"
echo "  POST-DEPLOY HEALTH CHECK  $(date -u "+%Y-%m-%d %H:%M:%S") UTC  →  ${TARGET}"
echo "════════════════════════════════════════════════════════════════════════════════"

FAIL=0
warn() { echo "  ⚠  $*"; FAIL=$((FAIL + 1)); }
ok()   { echo "  ✓  $*"; }

# ── 1. Engine systemd state ────────────────────────────────────────────────────
echo ""
echo "1. Engine systemd state"
ACTIVE_COUNT=$(ssh "${TARGET}" "systemctl list-units 'paper-live@*.service' --state=active --no-legend 2>/dev/null | wc -l" || echo 0)
EXPECTED=$(echo "$SYMBOLS_LC" | wc -w | tr -d ' ')
if [[ "$ACTIVE_COUNT" == "$EXPECTED" ]]; then
    ok "all $EXPECTED deployed engines active"
else
    warn "$ACTIVE_COUNT/$EXPECTED engines active"
    ssh "${TARGET}" "systemctl list-units 'paper-live@*.service' --no-legend 2>&1 | grep -v active" || true
fi

# ── 2. Watchdog timers ─────────────────────────────────────────────────────────
echo ""
echo "2. Watchdog timers"
TIMER_STATUS=$(ssh "${TARGET}" "systemctl list-units --no-legend 'paper-live-*.timer' 2>/dev/null" || echo "")
for timer in paper-live-watchdog.timer paper-live-digest.timer; do
    # systemctl output indents with 2 leading spaces — match anywhere on the timer's line.
    if echo "$TIMER_STATUS" | grep -qE "${timer}.*active"; then
        ok "$timer active"
    else
        warn "$timer not active"
    fi
done

# ── 3. Engine binary freshness (md5 match) ────────────────────────────────────
echo ""
echo "3. Binary code matches local"
LOCAL_MD5=$(md5sum pkg/funding/funding.go pkg/strategy/engine.go pkg/strategy/entry.go cmd/engine/main.go 2>/dev/null | awk '{print $1}' | sort | md5sum | awk '{print $1}')
REMOTE_MD5=$(ssh "${TARGET}" "md5sum /opt/trading-engine/pkg/funding/funding.go /opt/trading-engine/pkg/strategy/engine.go /opt/trading-engine/pkg/strategy/entry.go /opt/trading-engine/cmd/engine/main.go 2>/dev/null | awk '{print \$1}' | sort | md5sum | awk '{print \$1}'" 2>/dev/null || echo "")
if [[ "$LOCAL_MD5" == "$REMOTE_MD5" ]]; then
    ok "code-path checksums match (engine binary is from current source)"
else
    warn "code drift: local=${LOCAL_MD5:0:16}... remote=${REMOTE_MD5:0:16}..."
    warn "  → run ./deploy/sync.sh to push current source"
fi

# ── 4. Per-engine tick freshness ──────────────────────────────────────────────
echo ""
echo "4. Per-engine tick freshness (heartbeat last_tick_age)"
echo ""
printf "  %-14s %-9s %-13s %-30s %s\n" "symbol" "state" "last_tick" "last_event" "uptime"
printf "  %-14s %-9s %-13s %-30s %s\n" "------" "------" "---------" "----------" "------"

CHECK_RESULTS=$(ssh "${TARGET}" "now=\$(date -u +%s)
for sym in $SYMBOLS_LC; do
  active=\$(systemctl is-active paper-live@\${sym}.service 2>&1)
  last_hb=\$(grep '\"msg\":\"heartbeat\"' /var/log/paper-live/\${sym}.log 2>/dev/null | tail -1)
  age_ns=\$(echo \"\$last_hb\" | jq -r '.last_tick_age // 0' 2>/dev/null || echo 0)
  age_s=\$(( age_ns / 1000000000 ))
  last_event=\$(tail -1 /var/log/paper-live/\${sym}.log 2>/dev/null | jq -r '.msg' 2>/dev/null)
  # Uptime via systemd (source of truth; survives daily log rotation that
  # would otherwise hide the 'starting live engine' line in .log.1).
  start_epoch=\$(systemctl show paper-live@\${sym}.service -p ActiveEnterTimestampMonotonic --value 2>/dev/null)
  if [[ -n \"\$start_epoch\" ]] && [[ \"\$start_epoch\" != \"0\" ]]; then
    enter_real=\$(systemctl show paper-live@\${sym}.service -p ActiveEnterTimestamp --value 2>/dev/null | sed 's/[A-Z]\\{3\\} //; s/ UTC//')
    if [[ -n \"\$enter_real\" ]]; then
      enter_epoch=\$(date -u -d \"\$enter_real\" +%s 2>/dev/null || echo 0)
      uptime_min=\$(( (now - enter_epoch) / 60 ))
    else
      uptime_min=0
    fi
  else
    uptime_min=0
  fi
  printf '%s|%s|%d|%s|%d\n' \"\$sym\" \"\$active\" \"\$age_s\" \"\$last_event\" \"\$uptime_min\"
done")

STALE_COUNT=0
NO_TICK_EVER=0
while IFS='|' read -r sym state age_s event uptime; do
    [[ -z "$sym" ]] && continue
    case "$state" in
        active) state_disp="active" ;;
        *)      state_disp="$state" ; warn "  $sym not active ($state)" ;;
    esac
    # Stale = last tick > 5 min AND uptime > 5 min (allow startup grace)
    if [[ "$age_s" -gt 300 ]] && [[ "$uptime" -gt 5 ]]; then
        STALE_COUNT=$((STALE_COUNT + 1))
        printf "  %-14s %-9s %-13s %-30s %dmin  ⚠ STALE\n" "$sym" "$state_disp" "${age_s}s" "$event" "$uptime"
    elif [[ "$age_s" == "0" ]] && [[ "$uptime" -gt 10 ]]; then
        # Heartbeat says no ticks ever and engine has been up 10+ min
        NO_TICK_EVER=$((NO_TICK_EVER + 1))
        printf "  %-14s %-9s %-13s %-30s %dmin  ⚠ NO TICKS\n" "$sym" "$state_disp" "${age_s}s" "$event" "$uptime"
    else
        printf "  %-14s %-9s %-13s %-30s %dmin\n" "$sym" "$state_disp" "${age_s}s" "$event" "$uptime"
    fi
done <<< "$CHECK_RESULTS"

if [[ "$STALE_COUNT" -gt 0 ]]; then
    warn "$STALE_COUNT engine(s) stale (last tick > 5min, uptime > 5min)"
fi
if [[ "$NO_TICK_EVER" -gt 0 ]]; then
    warn "$NO_TICK_EVER engine(s) reported zero ticks despite >10min uptime — possible bad WS connection"
fi
if [[ "$STALE_COUNT" -eq 0 && "$NO_TICK_EVER" -eq 0 ]]; then
    ok "all engines have recent tick activity"
fi

# ── 5. Recent ERROR-level events ──────────────────────────────────────────────
# NB: previous version compared $0 (entire JSON line starting with `{`) against
# a timestamp cutoff lexicographically — `{` > `2026-...` for ANY timestamp,
# so all rows always matched. Use jq to extract the .time field for proper compare.
echo ""
echo "5. ERROR-level events in last 5 min"
ERR_TOTAL=$(ssh "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
for sym in $SYMBOLS_LC; do
  jq -rc --arg cutoff \"\$now_iso\" 'select(.level == \"ERROR\" and .time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null
done | wc -l")
if [[ "$ERR_TOTAL" == "0" ]]; then
    ok "no ERROR-level events in last 5 min"
else
    warn "$ERR_TOTAL ERROR-level events in last 5 min — investigate:"
    ssh "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
    for sym in $SYMBOLS_LC; do
      jq -rc --arg cutoff \"\$now_iso\" 'select(.level == \"ERROR\" and .time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null | head -3 | sed \"s/^/    /\"
    done"
fi

# ── 6. Recent rate-limit pressure ─────────────────────────────────────────────
echo ""
echo "6. Rate-limit pressure (last 5 min)"
RL_TOTAL=$(ssh "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
for sym in $SYMBOLS_LC; do
  jq -rc --arg cutoff \"\$now_iso\" 'select(.msg | test(\"rate limited\")) | select(.time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null
done | wc -l")
RL_PER_MIN=$(( RL_TOTAL / 5 ))
if [[ "$RL_TOTAL" -lt 50 ]]; then
    ok "$RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — comfortable)"
elif [[ "$RL_TOTAL" -lt 200 ]]; then
    ok "$RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — typical post-restart, monitor)"
else
    warn "$RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — high)"
fi

# ── 7. Position recovery events in last 24h ───────────────────────────────────
# Each engine restart with an in-flight position triggers Stub.RecoverFromJournal,
# which logs "position recovered from journal" once per recovered orphan. In
# steady state recoveries are rare — each one signals an unplanned (crash) or
# planned (deploy) restart. Surface them so silent restart loops or unexpected
# crashes show up in the audit instead of staying buried in the log files.
#
# 24h window scans .log + .log.1 (midnight-UTC rotation makes .log.1 the
# yesterday file). Always sufficient for a 24h cutoff regardless of run time.
# Cohort split (live vs shadow/<label>) derived from the .journal_path field.
echo ""
echo "7. Position recovery events in last 24h"
REC_TOTAL=$(ssh "${TARGET}" "cutoff=\$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')
total=0
for sym in $SYMBOLS_LC; do
  for f in /var/log/paper-live/\${sym}.log /var/log/paper-live/\${sym}.log.1; do
    [[ -f \"\$f\" ]] || continue
    n=\$(jq -rc --arg cutoff \"\$cutoff\" 'select(.msg == \"position recovered from journal\" and .time >= \$cutoff)' \"\$f\" 2>/dev/null | wc -l)
    total=\$((total + n))
  done
done
echo \$total")

if [[ "$REC_TOTAL" == "0" ]]; then
    ok "no position recoveries in last 24h"
else
    # ⓘ rather than ⚠: a recovery isn't inherently a problem (planned deploy
    # restarts will trigger it), but the operator should verify each is expected.
    echo "  ⓘ  $REC_TOTAL recovery event(s) in last 24h — verify each is a planned restart"
    ssh "${TARGET}" "cutoff=\$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')
    for sym in $SYMBOLS_LC; do
      for f in /var/log/paper-live/\${sym}.log /var/log/paper-live/\${sym}.log.1; do
        [[ -f \"\$f\" ]] || continue
        jq -rc --arg cutoff \"\$cutoff\" 'select(.msg == \"position recovered from journal\" and .time >= \$cutoff) | .time[0:19] + \"  \" + .symbol + \"  \" + .side + \"  entry=\" + (.entry | tostring) + \"  cohort=\" + (if (.journal_path | test(\"/shadow/\")) then (.journal_path | split(\"/\") | last) else \"live\" end)' \"\$f\" 2>/dev/null
      done
    done | sort -u" | sed 's/^/    /'
fi

# ── Verdict ───────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════════════════════"
if [[ "$FAIL" -eq 0 ]]; then
    echo "  ✓ HEALTHY — all post-deploy checks passed"
    echo "════════════════════════════════════════════════════════════════════════════════"
    exit 0
else
    echo "  ⚠ $FAIL warning(s) — review above"
    echo "════════════════════════════════════════════════════════════════════════════════"
    [[ "$STRICT" == "1" ]] && exit 1 || exit 0
fi
