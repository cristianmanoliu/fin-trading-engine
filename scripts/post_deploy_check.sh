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

# ─────────────────────────────────────────────────────────────────────
# Internal helpers (extracted for testability — see scripts/test_post_deploy_check.sh)
# ─────────────────────────────────────────────────────────────────────

# compare_code_checksums: classify a (local, remote) md5 pair into one of
# MATCH / MISMATCH / UNAVAILABLE. The original `[[ "$LOCAL" == "$REMOTE" ]]`
# at §3 was a canonical empty-empty-equality fail-open: when both md5sum
# invocations produced empty output (e.g., source files moved locally
# AND ssh broken), empty == empty was TRUE → ok "checksums match" while
# the operator believed the binary was current and neither side could
# actually be checked. Pinned by PD-2 of the post_deploy_check audit.
compare_code_checksums() {
    local local_md5="$1" remote_md5="$2"
    if [[ -z "$local_md5" ]] || [[ -z "$remote_md5" ]]; then
        echo "UNAVAILABLE"
    elif [[ "$local_md5" == "$remote_md5" ]]; then
        echo "MATCH"
    else
        echo "MISMATCH"
    fi
}

# classify_ssh_exit: separate ssh-transport failure (couldn't reach host,
# auth, signal) from remote-command outcomes (the command ran and exited
# with some code). Without this, every section's `$(ssh ... || echo 0)`
# collapses ssh-level failure into the same value as a successful "0"
# count → misclassification of "ssh broken" as "the thing being measured
# is broken." Same shape as F2 _classify_validate_exit on weekly_audit.
classify_ssh_exit() {
    case "$1" in
        0)                   echo "OK" ;;
        126|127|130|137|255) echo "SSH_FAILURE" ;;
        *)                   echo "REMOTE_FAILURE" ;;
    esac
}

# ssh_remote: run a command via ssh, capture stdout+stderr + exit code in
# named globals (SSH_REPLY, SSH_EXIT). Returns 0 always so set -e doesn't
# abort on transient ssh; callers MUST check SSH_EXIT explicitly via
# classify_ssh_exit.
SSH_REPLY=
SSH_EXIT=
ssh_remote() {
    local _e
    set +e
    SSH_REPLY=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "$@" 2>&1)
    _e=$?
    set -e
    SSH_EXIT=$_e
    return 0
}

# Test entry-point: when sourced (BASH_SOURCE != $0), stop here so
# consumers get only the function definitions without triggering the
# main orchestration flow.
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    return 0
fi

# ─────────────────────────────────────────────────────────────────────
# Main orchestration flow
# ─────────────────────────────────────────────────────────────────────

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="${1:-root@178.105.24.230}"
STRICT="${STRICT:-0}"

source "${ROOT}/scripts/lib/symbols.sh"
# notify.sh sourced early so `crit` (below) can fire CRITICAL Telegram alerts
# inline rather than waiting for the end-of-script roll-up. The shared helper
# is a graceful no-op when TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID are unset, so
# this doesn't introduce a Telegram dependency for local invocations.
# shellcheck source=lib/notify.sh
source "${ROOT}/scripts/lib/notify.sh"
SYMBOLS_LC=$(get_symbols deployed lower)

echo "════════════════════════════════════════════════════════════════════════════════"
echo "  POST-DEPLOY HEALTH CHECK  $(date -u "+%Y-%m-%d %H:%M:%S") UTC  →  ${TARGET}"
echo "════════════════════════════════════════════════════════════════════════════════"

FAIL=0
warn() { echo "  ⚠  $*"; FAIL=$((FAIL + 1)); }
ok()   { echo "  ✓  $*"; }
# crit fires a CRITICAL-tier Telegram alert inline AND counts to FAIL so STRICT
# mode still exits 1. CRITICAL bypasses the WARN rate-limit (5/hr) and mute-hour
# suppression — reserved for failures where a delayed alert means a delayed
# response and the cost of that delay is high. As of 2026-05-10 only the drift-
# cron-absent case uses this: silent cron unload = silent decision-grade-kill
# offline = real-money exposure with no automated stop. If you add other crit
# call sites, make sure they meet the "no delay tolerated" bar — every CRITICAL
# trains the operator on what to drop everything for.
crit() {
    echo "  🚨 $*"
    FAIL=$((FAIL + 1))
    notify_telegram CRITICAL "post_deploy_check on $(hostname)" "$*"
}

# ── 1. Engine systemd state ────────────────────────────────────────────────────
echo ""
echo "1. Engine systemd state"
EXPECTED=$(echo "$SYMBOLS_LC" | wc -w | tr -d ' ')
ssh_remote "systemctl list-units 'paper-live@*.service' --state=active --no-legend 2>/dev/null | wc -l"
case "$(classify_ssh_exit "$SSH_EXIT")" in
    OK)
        ACTIVE_COUNT="$SSH_REPLY"
        if [[ "$ACTIVE_COUNT" == "$EXPECTED" ]]; then
            ok "all $EXPECTED deployed engines active"
        else
            warn "$ACTIVE_COUNT/$EXPECTED engines active"
            ssh_remote "systemctl list-units 'paper-live@*.service' --no-legend 2>&1 | grep -v active"
            [[ "$SSH_EXIT" -eq 0 ]] && echo "$SSH_REPLY"
        fi
        ;;
    SSH_FAILURE|REMOTE_FAILURE)
        # PD-1: distinct from "0/N engines active" — this is "could not
        # query systemd state on the host." Misclassifying ssh failure
        # as engine death sends the operator down the wrong rabbit hole.
        warn "could not query systemd state on ${TARGET} (ssh exit=$SSH_EXIT) — engine count UNKNOWN"
        ;;
esac

# ── 2. Watchdog timers ─────────────────────────────────────────────────────────
echo ""
echo "2. Watchdog timers"
TIMER_STATUS=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "systemctl list-units --no-legend 'paper-live-*.timer' 2>/dev/null" || echo "")
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
ssh_remote "md5sum /opt/trading-engine/pkg/funding/funding.go /opt/trading-engine/pkg/strategy/engine.go /opt/trading-engine/pkg/strategy/entry.go /opt/trading-engine/cmd/engine/main.go 2>/dev/null | awk '{print \$1}' | sort | md5sum | awk '{print \$1}'"
REMOTE_MD5=""
[[ "$SSH_EXIT" -eq 0 ]] && REMOTE_MD5="$SSH_REPLY"
# PD-2: empty == empty is TRUE in [[ ]] string compare. Without the
# UNAVAILABLE branch, a double-failure (local source moved AND ssh broken)
# silently reported "checksums match" while the operator believed the
# binary was current and neither side could be checked. Canonical empty-
# empty equality fail-open.
case "$(compare_code_checksums "$LOCAL_MD5" "$REMOTE_MD5")" in
    MATCH)
        ok "code-path checksums match (engine binary is from current source)"
        ;;
    MISMATCH)
        warn "code drift: local=${LOCAL_MD5:0:16}... remote=${REMOTE_MD5:0:16}..."
        warn "  → run ./deploy/sync.sh to push current source"
        ;;
    UNAVAILABLE)
        MD5_LOCAL_STATE="set"; [[ -z "$LOCAL_MD5" ]] && MD5_LOCAL_STATE="empty"
        MD5_REMOTE_STATE="set"; [[ -z "$REMOTE_MD5" ]] && MD5_REMOTE_STATE="empty"
        warn "could not compare code-path checksums (local=${MD5_LOCAL_STATE} remote=${MD5_REMOTE_STATE} ssh_exit=$SSH_EXIT) — code-vs-binary drift UNKNOWN"
        ;;
esac

# ── 4. Per-engine tick freshness ──────────────────────────────────────────────
echo ""
echo "4. Per-engine tick freshness (heartbeat last_tick_age)"
echo ""
printf "  %-14s %-9s %-13s %-30s %s\n" "symbol" "state" "last_tick" "last_event" "uptime"
printf "  %-14s %-9s %-13s %-30s %s\n" "------" "------" "---------" "----------" "------"

ssh_remote "now=\$(date -u +%s)
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
done"
# PD-3: distinct ssh-failure path. Without this, ssh failure → empty
# CHECK_RESULTS → 0 loop iterations → green "all engines have recent
# tick activity" with literally zero data. Highest-stakes fail-open in
# this script — operator believes the fleet is healthy when no check
# actually ran.
if [[ "$SSH_EXIT" -ne 0 ]]; then
    warn "could not collect tick-freshness data on ${TARGET} (ssh exit=$SSH_EXIT) — fleet health UNKNOWN"
    CHECK_RESULTS=""
else
    CHECK_RESULTS="$SSH_REPLY"
fi

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
# PD-3 closure: only emit the green "all healthy" line if we ACTUALLY
# inspected at least one engine. The empty-CHECK_RESULTS case (ssh
# failure handled above) must NOT fall through to ok().
if [[ "$STALE_COUNT" -eq 0 && "$NO_TICK_EVER" -eq 0 ]] && [[ -n "$CHECK_RESULTS" ]]; then
    ok "all engines have recent tick activity"
fi

# Compute fleet-min uptime from CHECK_RESULTS for use by sections 5 and 6:
# transient startup errors (WS reconnect failures, initial rate-limit bursts
# during the WS→REST fallback at t≈180s) are documented and self-clearing,
# so they should not flip a STRICT=1 run when at least one engine is still
# in its first 5 minutes. After all engines reach 5min the gate releases.
MIN_UPTIME=$(echo "$CHECK_RESULTS" | awk -F'|' 'BEGIN{min=999999} NF>=5 && $5+0 < min {min=$5+0} END{print min}')
if [[ -z "$MIN_UPTIME" ]] || [[ "$MIN_UPTIME" == "999999" ]]; then
    MIN_UPTIME=0
fi

# ── 5. Recent ERROR-level events ──────────────────────────────────────────────
# NB: previous version compared $0 (entire JSON line starting with `{`) against
# a timestamp cutoff lexicographically — `{` > `2026-...` for ANY timestamp,
# so all rows always matched. Use jq to extract the .time field for proper compare.
echo ""
echo "5. ERROR-level events in last 5 min"
ERR_TOTAL=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
for sym in $SYMBOLS_LC; do
  jq -rc --arg cutoff \"\$now_iso\" 'select(.level == \"ERROR\" and .time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null
done | wc -l")
if [[ "$ERR_TOTAL" == "0" ]]; then
    ok "no ERROR-level events in last 5 min"
elif [[ "$MIN_UPTIME" -lt 5 ]]; then
    # Fleet warming up — surface as info but do not increment FAIL. Errors
    # fired during the WS→REST fallback gap (t < 3min) are documented
    # transients; a real failure will still be visible after engines reach
    # 5min uptime, at which point the gate releases and any persistent
    # error flips back to a warn.
    echo "  ⓘ  $ERR_TOTAL ERROR-level events in last 5 min (fleet warming up: min uptime ${MIN_UPTIME}min — re-run after fleet ages 5+ min):"
    ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
    for sym in $SYMBOLS_LC; do
      jq -rc --arg cutoff \"\$now_iso\" 'select(.level == \"ERROR\" and .time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null | head -3 | sed \"s/^/    /\"
    done"
else
    warn "$ERR_TOTAL ERROR-level events in last 5 min — investigate:"
    ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
    for sym in $SYMBOLS_LC; do
      jq -rc --arg cutoff \"\$now_iso\" 'select(.level == \"ERROR\" and .time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null | head -3 | sed \"s/^/    /\"
    done"
fi

# ── 6. Recent rate-limit pressure ─────────────────────────────────────────────
echo ""
echo "6. Rate-limit pressure (last 5 min)"
RL_TOTAL=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "now_iso=\$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
for sym in $SYMBOLS_LC; do
  jq -rc --arg cutoff \"\$now_iso\" 'select(.msg | test(\"rate limited\")) | select(.time >= \$cutoff)' /var/log/paper-live/\${sym}.log 2>/dev/null
done | wc -l")
RL_PER_MIN=$(( RL_TOTAL / 5 ))
if [[ "$RL_TOTAL" -lt 50 ]]; then
    ok "$RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — comfortable)"
elif [[ "$RL_TOTAL" -lt 200 ]]; then
    ok "$RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — typical post-restart, monitor)"
elif [[ "$MIN_UPTIME" -lt 5 ]]; then
    # 200+ during the warm-up window can come from the simultaneous WS→REST
    # transition across all 16 engines at t≈180s. Surface but do not FAIL.
    echo "  ⓘ  $RL_TOTAL rate-limit warnings in 5 min ($RL_PER_MIN/min — fleet warming up; re-run after 5+ min uptime)"
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
REC_TOTAL=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "cutoff=\$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')
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
    ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "cutoff=\$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')
    for sym in $SYMBOLS_LC; do
      for f in /var/log/paper-live/\${sym}.log /var/log/paper-live/\${sym}.log.1; do
        [[ -f \"\$f\" ]] || continue
        jq -rc --arg cutoff \"\$cutoff\" 'select(.msg == \"position recovered from journal\" and .time >= \$cutoff) | .time[0:19] + \"  \" + .symbol + \"  \" + .side + \"  entry=\" + (.entry | tostring) + \"  cohort=\" + (if (.journal_path | test(\"/shadow/\")) then (.journal_path | split(\"/\") | last) else \"live\" end)' \"\$f\" 2>/dev/null
      done
    done | sort -u" | sed 's/^/    /'
fi

# Section 8: Executor mode per engine — surfaces which symbols are running
# real-money (--executor binance_live) vs paper-money (default stub). Reads
# the actual ExecStart string from systemd so per-symbol overrides via
# `systemctl edit paper-live@<sym>.service` are visible. After any STAGE_1+
# promotion this is the operator's "did the right symbols get flipped"
# check; pre-promotion it should always show 0 real-money engines.
echo ""
echo "8. Executor mode per engine"
ssh_remote "real_count=0
real_list=''
testnet_count=0
testnet_list=''
missing_count=0
missing_list=''
for sym in $SYMBOLS_LC; do
    es=\$(systemctl show -p ExecStart --value paper-live@\${sym}.service 2>/dev/null || true)
    # Empty ExecStart means systemctl failed or the service doesn't exist.
    # Was a fail-open: the engine fell through every pattern and was silently
    # counted as 'stub' by absence, masquerading systemd brokenness as healthy.
    # Now: track explicitly so 16 broken engines can't pose as 16 stub engines.
    if [[ -z \"\$es\" ]]; then
        missing_count=\$((missing_count + 1))
        missing_list=\"\$missing_list \$sym\"
        continue
    fi
    # Match the binance_live token exactly — trailing space (common, more args
    # follow) or end-of-string. Without the trailing-space anchor, the glob
    # '*--executor binance_live*' would falsely match binance_live_testnet too.
    if [[ \"\$es\" == *'--executor binance_live '* || \"\$es\" == *'--executor=binance_live '* \\
       || \"\$es\" == *'--executor binance_live' || \"\$es\" == *'--executor=binance_live' ]]; then
        real_count=\$((real_count + 1))
        real_list=\"\$real_list \$sym\"
    elif [[ \"\$es\" == *'--executor binance_live_testnet'* || \"\$es\" == *'--executor=binance_live_testnet'* ]]; then
        testnet_count=\$((testnet_count + 1))
        testnet_list=\"\$testnet_list \$sym\"
    fi
done
echo \"\$real_count|\$real_list|\$testnet_count|\$testnet_list|\$missing_count|\$missing_list\""
TOTAL=$(echo "$SYMBOLS_LC" | wc -w | tr -d ' ')
# PD-4: distinct ssh-failure path. Without this, an ssh failure (or a
# remote-script crash that produces empty stdout) leaves EXEC_REPORT
# empty → IFS read produces empty REAL_COUNT/TESTNET_COUNT → the inner
# `if [[ "$REAL_COUNT" != "0" ]]` is TRUE because "" != "0" → false-
# positive "REAL-MONEY ACTIVE on / 16 engines:" panic banner with empty
# values. Operator pages themselves about real-money on a paper-only
# deploy; trust in the dashboard collapses.
if [[ "$SSH_EXIT" -ne 0 ]] || [[ -z "$SSH_REPLY" ]]; then
    warn "could not query executor modes on ${TARGET} (ssh exit=$SSH_EXIT, output empty=${SSH_REPLY:-y}) — executor state UNKNOWN"
else
    EXEC_REPORT="$SSH_REPLY"
    IFS='|' read -r REAL_COUNT REAL_LIST TESTNET_COUNT TESTNET_LIST MISSING_COUNT MISSING_LIST <<<"$EXEC_REPORT"
    MISSING_COUNT="${MISSING_COUNT:-0}"
    REAL_COUNT="${REAL_COUNT:-0}"
    TESTNET_COUNT="${TESTNET_COUNT:-0}"
    if [[ "$MISSING_COUNT" != "0" ]]; then
        # Loud warn — empty ExecStart on N engines means we cannot tell their
        # executor mode at all. Section 1 may show those engines as "active" but
        # this section's contract — "verify executor mode" — fails.
        warn "$MISSING_COUNT / $TOTAL engines have empty ExecStart (systemctl failed):$MISSING_LIST"
    fi
    if [[ "$REAL_COUNT" == "0" && "$TESTNET_COUNT" == "0" && "$MISSING_COUNT" == "0" ]]; then
        ok "all $TOTAL engines on stub (paper-money) — pre-STAGE_1 expected state"
    elif [[ "$REAL_COUNT" == "0" && "$TESTNET_COUNT" == "0" ]]; then
        # All visible engines are stub but some couldn't be inspected. Don't
        # report a green "all on stub" — the missing ones are unknowns.
        :  # warn for $MISSING_COUNT was already emitted above
    else
        if [[ "$REAL_COUNT" != "0" ]]; then
            # Real money is loud: emit an unmistakable banner. This is NOT a warning
            # in the FAIL sense (real money on a promoted engine is the desired state
            # post-STAGE_1), but it MUST be visible at every post_deploy_check.
            echo "  🚨 REAL-MONEY ACTIVE on $REAL_COUNT / $TOTAL engines:$REAL_LIST"
            echo "     Verify this matches your current STAGE_<N> promotion roster."
            echo "     Per stage_promotion_runbook_decision_rule_2026-05-08.md, only"
            echo "     ONE symbol promotes at a time and other symbols stay on stub."
        fi
        if [[ "$TESTNET_COUNT" != "0" ]]; then
            # Testnet is play-money but operationally distinct from stub — orders
            # leave the host. Surface for visibility but don't escalate.
            echo "  ⓘ  TESTNET executor active on $TESTNET_COUNT / $TOTAL engines:$TESTNET_LIST"
            echo "     (Layer 2 integration gate — orders go to testnet.binancefuture.com)"
        fi
    fi
fi

# ── 9. Funding CSV staleness ──────────────────────────────────────────────────
# cmd/engine logs `slog.Warn("funding CSV is stale; ...")` once at startup if
# the loaded historical funding CSV's last entry is >7d old. Section 5
# (ERROR-level last 5min) misses this — it's a WARN and the audit is often
# run >5min after restart. Scan the current log for the literal message.
echo ""
echo "9. Funding CSV staleness"
STALE_REPORT=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "stale_lines=\$(for sym in $SYMBOLS_LC; do
  jq -rc 'select(.msg == \"funding CSV is stale; trades held past last entry will accrue \$0 funding — run scripts/refresh_funding.sh + redeploy\") | \"  \" + .symbol + \"  stale_days=\" + (.stale_days|tostring) + \"  last_funding_ts=\" + .last_funding_ts' /var/log/paper-live/\${sym}.log 2>/dev/null | tail -1
done | grep -v '^\$' || true)
echo -n \"\$stale_lines\"")
if [[ -z "$STALE_REPORT" ]]; then
    ok "no stale funding-CSV warnings (all engines started with ≤7d-old data)"
else
    n=$(printf '%s\n' "$STALE_REPORT" | wc -l | tr -d ' ')
    warn "$n engine(s) started with stale funding CSV — run scripts/refresh_funding.sh + redeploy:"
    printf '%s\n' "$STALE_REPORT"
fi

# ── 10. Disk space + log size sanity ──────────────────────────────────────────
# Engines write JSONL journals + structured logs continuously. Daily log
# rotation bounds growth in steady state, but a runaway logging loop, stuck
# process, or filled / could all silently corrupt journals (write-failure on
# an already-locked engine doesn't crash it, just produces missing events).
# Two thresholds: ample-headroom for free space + per-file ceiling for
# runaway logs.
echo ""
echo "10. Disk space"
ssh_remote "free_mb=\$(df -BM / | tail -1 | awk '{print \$4}' | sed 's/M\$//')
largest_bytes=\$(ls -l /var/log/paper-live/*.log 2>/dev/null | awk '{print \$5}' | sort -n | tail -1)
largest_bytes=\${largest_bytes:-0}
echo \"\$free_mb|\$largest_bytes\""
# PD-5: distinct ssh-failure path. Without this, ssh failure → empty
# DISK_REPORT → IFS read produces empty FREE_MB → defaulted to "0" via
# `:-0` → false-positive "low disk space: 0M free" warning. Operator
# investigates a non-existent disk-full when reality is "ssh broken."
if [[ "$SSH_EXIT" -ne 0 ]] || [[ -z "$SSH_REPLY" ]]; then
    warn "could not query disk state on ${TARGET} (ssh exit=$SSH_EXIT) — disk headroom UNKNOWN"
else
    DISK_REPORT="$SSH_REPLY"
    IFS='|' read -r FREE_MB LARGEST_BYTES <<<"$DISK_REPORT"
    FREE_MB="${FREE_MB:-0}"
    LARGEST_BYTES="${LARGEST_BYTES:-0}"
    LARGEST_MB=$(( LARGEST_BYTES / 1048576 ))

    if [[ "$FREE_MB" -lt 1024 ]]; then
        warn "low disk space: ${FREE_MB}M free on / (engine journal writes will fail when full)"
    elif [[ "$FREE_MB" -lt 5120 ]]; then
        echo "  ⓘ  ${FREE_MB}M free on / (warn threshold ≥1024M; ample ≥5120M)"
    else
        ok "${FREE_MB}M free on / (ample headroom)"
    fi

    # Largest current log: rotation runs daily, so steady-state ≤ ~1d × ~10MB/day
    # per engine. >500MB in a single .log file means logging-rate is an order of
    # magnitude above expected — investigate before rotation hides it.
    if [[ "$LARGEST_BYTES" -gt 1073741824 ]]; then     # 1GB
        warn "largest engine log file is ${LARGEST_MB}M (>1GB — investigate logging loop)"
    elif [[ "$LARGEST_BYTES" -gt 524288000 ]]; then     # 500MB
        echo "  ⓘ  largest engine log file is ${LARGEST_MB}M (above typical; rotation will trim daily)"
    else
        ok "largest engine log file is ${LARGEST_MB}M (within typical bounds)"
    fi
fi

# ── 11. Drift detector cron freshness ─────────────────────────────────────────
# The drift detector is the decision-grade kill mechanism (per kill-bar
# mis-calibration finding). Its launchd job fires weekly (Sunday 09:00
# local). Silent failure of THAT cron — e.g., job got unloaded, launchd
# stopped firing it — would leave the operator with no decision-grade
# signal until forward_paper_status.sh's threshold criteria scream (which
# are advisory only). Two-stage check: launchd registration + history
# freshness.
echo ""
echo "11. Drift detector cron freshness"
HISTORY="${ROOT}/results/drift_check_history.jsonl"
LAUNCHD_LABEL="com.tradingengine.drift-check"
NOW_EPOCH=$(date -u +%s)
# Capture launchctl output first — `set -euo pipefail` + `grep -q` causes a
# false negative because grep -q exits early on match, sending SIGPIPE to
# launchctl, and the pipeline exit code reflects launchctl's SIGPIPE rather
# than grep's 0. Capture then grep avoids the pipeline.
LAUNCHD_LIST="$(launchctl list 2>/dev/null || true)"
if ! echo "$LAUNCHD_LIST" | grep -q "$LAUNCHD_LABEL"; then
    if [[ "$(uname -s)" == "Darwin" ]]; then
        warn "launchd job '$LAUNCHD_LABEL' not loaded — drift detector won't fire"
        warn "  → run: launchctl load -w ~/Library/LaunchAgents/${LAUNCHD_LABEL}.plist"
    else
        echo "  ⓘ  launchd not available on this OS — skipping registration check"
    fi
else
    # Closes a fail-open: a launchd job can be REGISTERED but FAILING on
    # every invocation (wrong path in plist, ENV missing, plist syntax
    # error that loads but doesn't run). The original check only verified
    # registration; a registered-but-failing cron would silently look
    # healthy here while never producing drift_check runs. `launchctl list`
    # output shows last-exit-status in column 2: 0 = clean, anything else
    # = the cron's last invocation failed. ENOENT (78) is the launchd code
    # for "couldn't exec the program" — a particularly noisy fail mode.
    LAUNCHD_STATUS=$(echo "$LAUNCHD_LIST" | awk -v lbl="$LAUNCHD_LABEL" '$3 == lbl {print $2}' | head -1)
    if [[ -n "$LAUNCHD_STATUS" ]] && [[ "$LAUNCHD_STATUS" != "0" ]] && [[ "$LAUNCHD_STATUS" != "-" ]]; then
        warn "launchd job '$LAUNCHD_LABEL' last exit status = $LAUNCHD_STATUS"
        warn "  → tail results/drift_runs/launchd.err.log for the failure cause"
    fi

    if [[ ! -s "$HISTORY" ]]; then
        # If launchd is loaded AND last exit clean (or never run) but history
        # is empty, that's "first run pending" — legitimate state when the
        # plist was just registered. The launchd-status check above catches
        # the "registered but failing" variant separately.
        echo "  ⓘ  drift_check_history.jsonl empty/missing — first cron run pending"
        echo "     If this persists >7d, verify the plist actually invokes weekly_audit.sh"
    elif ! command -v jq >/dev/null 2>&1; then
        # PD-6: jq missing was previously silent fall-through — drift
        # freshness verdict simply absent from output. The operator could
        # not distinguish "freshness OK" from "couldn't check freshness."
        warn "jq missing on this machine — drift_check freshness UNKNOWN (install jq or run on a machine that has it)"
    elif command -v jq >/dev/null 2>&1; then
        last_ts=$(tail -1 "$HISTORY" | jq -r '.ts')
        last_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$last_ts" +%s 2>/dev/null || \
                     date -u -d "$last_ts" +%s 2>/dev/null || echo 0)
        if [[ "$last_epoch" == "0" ]]; then
            echo "  ⓘ  could not parse last drift_check timestamp — manually verify $HISTORY"
        else
            age_days=$(( (NOW_EPOCH - last_epoch) / 86400 ))
            # Tier ladder: ≤7d clean (within one weekly cycle), 8-10d info
            # (within slack of one missed cycle e.g. DST drift), 11-14d WARN
            # (one cycle definitely missed — investigate), >14d CRITICAL (two
            # cycles missed → decision-grade kill mechanism is offline → real-
            # money exposure with no automated stop).
            if [[ "$age_days" -gt 14 ]]; then
                crit "drift_check last ran ${age_days}d ago (>14d — decision-grade kill mechanism OFFLINE)
Two consecutive weekly cycles missed. Investigate IMMEDIATELY:
  - launchctl list | grep tradingengine  (verify plist still loaded)
  - tail results/drift_runs/launchd.{out,err}.log  (last invocation cause)
  - launchctl unload + reload the plist if no recent runs
Real-money positions (if any) have no automated drift kill while this remains red."
            elif [[ "$age_days" -gt 10 ]]; then
                warn "drift_check last ran ${age_days}d ago (>10d — weekly cron may have stopped firing)"
            elif [[ "$age_days" -gt 7 ]]; then
                echo "  ⓘ  drift_check last ran ${age_days}d ago (within 1 cycle of expected weekly cadence)"
            else
                ok "drift_check last ran ${age_days}d ago (latest: ${last_ts})"
            fi
        fi
    fi
fi

# ── 12. Restart-loop detection ────────────────────────────────────────────────
# Section 1 verifies engines are active *right now* and section 4 checks
# heartbeat freshness, but neither catches the failure mode where systemd
# is auto-restarting a crash-on-startup engine. Between restarts the engine
# IS "active" briefly and section 4's heartbeat may not yet be stale, so a
# repeated-crash loop hides in plain sight. Count systemd-Started events
# per engine over the last hour; >2 implies more than one planned restart;
# >5 implies a real loop.
echo ""
echo "12. Restart-loop detection (last 1 hour)"
ssh_remote "for sym in $SYMBOLS_LC; do
    n=\$(journalctl -u paper-live@\${sym}.service --since '1 hour ago' --no-pager 2>/dev/null | grep -c 'Started paper-live' || true)
    if [[ \$n -gt 2 ]]; then
        echo \"\$sym|\$n\"
    fi
done"
# PD-7: distinct ssh-failure path. Without this, ssh-or-journalctl
# failure → empty RESTART_REPORT → green "no engines restarted" while
# we never actually inspected the restart history. A real restart loop
# during a window where ssh is broken would be invisible.
if [[ "$SSH_EXIT" -ne 0 ]]; then
    warn "could not query restart history on ${TARGET} (ssh exit=$SSH_EXIT) — restart-loop check UNKNOWN"
else
    RESTART_REPORT="$SSH_REPLY"
    if [[ -z "$RESTART_REPORT" ]]; then
        ok "no engines restarted >2 times in last hour"
    else
        while IFS='|' read -r sym n; do
            [[ -z "$sym" ]] && continue
            if [[ "$n" -gt 5 ]]; then
                warn "$sym restarted $n times in last hour (>5 — restart LOOP, investigate now)"
            else
                warn "$sym restarted $n times in last hour (>2 — verify no crash-on-startup pattern)"
            fi
        done <<< "$RESTART_REPORT"
    fi
fi

# ── 13. Live-config compliance ────────────────────────────────────────────────
# Catches a real pre-registration risk: someone manually `systemctl edit
# paper-live@<sym>.service` overrides the locked strategy parameters. §3
# (binary md5) catches code drift but not config drift — an engine running
# the right binary with the wrong --target-rr or wrong --shadow specs is
# silently off-spec. Per CLAUDE.md "Strategy status" + the locked rule
# stack, the live config is:
#
#   --signal-tf 4H --target-rr 6.0 --side-filter short --max-hold-hours 504
#   --fee-bps 10 --stop-slippage-bps 5
#   --shadow alt5-15-336:5-15-336,alt5-15-504:5-15-504,bb20:bb:20-2.0-504
#   --funding-csv-dir <any path; presence required>
#
# --executor is intentionally NOT checked here — STAGE_1+ promotion will
# legitimately switch one symbol to binance_live, and §8 covers that
# distinction. Funding-csv-dir path varies (relative on dev, absolute on
# VPS) so we only assert the flag is present.
echo ""
echo "13. Live-config compliance"
DEVIATIONS=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "for sym in $SYMBOLS_LC; do
    es=\$(systemctl show -p ExecStart --value paper-live@\${sym}.service 2>/dev/null || true)
    missing=''
    for spec in \\
        '--signal-tf 4H' \\
        '--target-rr 6.0' \\
        '--side-filter short' \\
        '--max-hold-hours 504' \\
        '--fee-bps 10' \\
        '--stop-slippage-bps 5' \\
        '--shadow alt5-15-336:5-15-336,alt5-15-504:5-15-504,bb20:bb:20-2.0-504' \\
        '--funding-csv-dir'; do
        if [[ \"\$es\" != *\"\$spec\"* ]]; then
            missing=\"\${missing}\${missing:+; }\$spec\"
        fi
    done
    if [[ -n \"\$missing\" ]]; then
        echo \"\$sym|\$missing\"
    fi
done")
if [[ -z "$DEVIATIONS" ]]; then
    ok "all engines run locked CLAUDE.md live-config (signal-tf/target-rr/side/max-hold/fee/slip/shadow)"
else
    while IFS='|' read -r sym missing; do
        [[ -z "$sym" ]] && continue
        warn "$sym deviates from locked live-config — missing: $missing"
    done <<< "$DEVIATIONS"
fi

# notify.sh is sourced at the top of the script (above) so inline crit()
# helpers can fire CRITICAL Telegram alerts as the failure is detected, not
# only via the end-of-script roll-up. The roll-up below remains for WARN-tier
# warnings that don't warrant immediate operator paging.

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
    # Alert on STRICT-mode failures only — warnings during fleet warm-up are
    # routine (see the 5min-uptime gates in §§4-6) and would desensitize the
    # operator. STRICT=1 is the operator-set "I want this to fail loud" signal.
    if [[ "$STRICT" == "1" ]]; then
        notify_telegram WARN "post_deploy_check on $(hostname)" \
            "$FAIL warning(s) on ${TARGET}
re-run scripts/post_deploy_check.sh for details, or check the original output"
        exit 1
    fi
    exit 0
fi
