#!/usr/bin/env bash
# session_start_check.sh — lightweight pre-flight at the start of any
# operator (or Claude) session.
#
# Built 2026-05-19 after discovering main had been CI-red since 2026-05-18
# (commit 1c96779, daily_digest tests failing on Ubuntu) while the
# session-handoff doc said "shipping clean". Yesterday's session ended
# with a wrong claim, today's session inherited it. safe-push.sh caught
# the broken state during the first push attempt, but only because we
# happened to push. A check like this WOULD have caught it during the
# session-handoff briefing.
#
# Purpose: surface things that may have changed between session-end and
# session-resume that the handoff doc can't predict:
#
#   1. CI conclusion on the most-recent commit to main.
#   2. All systemd services active on the VPS (16 paper-live + 1 testnet
#      OR Layer 3 wrapper if enabled).
#   3. Drift detector freshness — should be ≤8 days (weekly cadence).
#   4. Layer 3 cron freshness — should be ≤8 days if cron is installed.
#   5. Alert-worthy engine-log lines (last 2 calendar days by the line's
#      own slog "time" field, scanned across *.log + *.log.1) — slog ERROR
#      plus the named WARN patterns that cover every historically-unnoticed
#      CRITICAL Telegram (safety-gate block, order reject, position drift,
#      telegram send failure). Added 2026-07-07 after three
#      fired-but-unnoticed alerts. NOTE: the date filter runs remote-side,
#      so the mock-ssh test harness cannot exercise it — verified live.
#
# This is NOT a replacement for post_deploy_check.sh (which is exhaustive
# and runs as part of deploy). This is the minimal "did anything go
# sideways between sessions" check.
#
# Usage:
#   ./scripts/session_start_check.sh                      # standard
#   ./scripts/session_start_check.sh --quiet              # 1-line summary
#   ./scripts/session_start_check.sh --strict             # exit non-zero on warnings
#
# Env overrides (testing):
#   SESSION_CHECK_VPS         — VPS target (default: root@178.105.24.230)
#   SESSION_CHECK_GH          — gh binary (default: gh)
#   SESSION_CHECK_SSH         — ssh binary (default: ssh)
#   SESSION_CHECK_MAX_AGE_DAYS — staleness threshold (default: 8)
#
# Exit codes:
#   0  CLEAN — all checks pass OR warnings reported in non-strict mode
#   1  WARN  — at least one warning AND --strict
#   2  ERROR — gh/ssh binary missing OR misconfig
set -uo pipefail

# --- args ---
QUIET=0
STRICT=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet)  QUIET=1; shift ;;
        --strict) STRICT=1; shift ;;
        --help|-h)
            sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "ERROR: unknown flag: $1" >&2; exit 2 ;;
    esac
done

# --- env ---
VPS="${SESSION_CHECK_VPS:-root@178.105.24.230}"
GH="${SESSION_CHECK_GH:-gh}"
SSH="${SESSION_CHECK_SSH:-ssh}"
MAX_AGE_DAYS="${SESSION_CHECK_MAX_AGE_DAYS:-8}"

# --- output ---
WARNINGS=()
CLEAN_CHECKS=()
INFO_LINES=()

add_warn() {
    WARNINGS+=("$1")
}

add_ok() {
    CLEAN_CHECKS+=("$1")
}

add_info() {
    INFO_LINES+=("$1")
}

# --- 1. CI on main ---
if ! command -v "$GH" >/dev/null 2>&1; then
    add_warn "gh CLI not found ($GH) — cannot check CI conclusion on main"
else
    # gh run list with json; takes ~1.5s on a working network.
    ci_json=$("$GH" run list --branch main --limit 1 --json conclusion,headSha,databaseId,displayTitle 2>/dev/null || echo "[]")
    # Distinguish: gh-not-authenticated (no rows) vs gh-authenticated-but-in-flight
    # (conclusion field is JSON null on a running run). jq -r prints null as the
    # literal string "null"; treat that as "in-flight" not "missing".
    ci_has_row=$(echo "$ci_json" | jq 'length > 0')
    ci_concl=$(echo "$ci_json" | jq -r '.[0].conclusion // "null"')
    ci_sha=$(echo "$ci_json" | jq -r '.[0].headSha // ""' | head -c 7)
    ci_id=$(echo "$ci_json" | jq -r '.[0].databaseId // ""')

    if [[ "$ci_has_row" != "true" ]]; then
        add_warn "Could not fetch CI status — gh might not be authenticated. Try: gh auth status"
    else
        case "$ci_concl" in
            success)
                add_ok "CI green on main (${ci_sha} run ${ci_id})"
                ;;
            null)
                add_warn "CI conclusion empty on most-recent main commit (${ci_sha}) — possibly still running. Watch with: gh run watch ${ci_id}"
                ;;
            *)
                add_warn "CI ${ci_concl} on main (${ci_sha} run ${ci_id}). Inspect: gh run view ${ci_id} --log-failed"
                ;;
        esac
    fi
    add_info "Latest commit: $(echo "$ci_json" | jq -r '.[0].displayTitle // ""')"
fi

# --- 2. Systemd services on VPS ---
# Run via SSH BatchMode so we don't hang on auth prompts. Single SSH call
# combining all checks to keep total runtime low.
remote_combined=$(
    "$SSH" -o BatchMode=yes -o ConnectTimeout=5 "$VPS" '
        set -e
        echo "=== services ==="
        systemctl list-units --type=service --state=active --no-legend \
            "paper-live@*.service" "testnet-engine@*.service" "testnet-engine.service" 2>/dev/null \
            | awk "{print \$1}"
        echo "=== failed ==="
        # ONLY failed services — inactive includes template instances that
        # were enumerated but never enabled, which is normal not broken.
        systemctl list-units --type=service --state=failed --no-legend \
            "paper-live@*.service" "testnet-engine@*.service" 2>/dev/null \
            | awk "{print \$1}"
        echo "=== drift ==="
        # Drift detector freshness via history file mtime.
        if [[ -f /opt/trading-engine/results/drift_check_history.jsonl ]]; then
            stat -c %Y /opt/trading-engine/results/drift_check_history.jsonl 2>/dev/null \
                || stat -f %m /opt/trading-engine/results/drift_check_history.jsonl 2>/dev/null \
                || echo 0
        else
            echo "missing"
        fi
        echo "=== layer3 ==="
        # Layer 3 cron history freshness (file may not exist if cron not installed yet).
        if [[ -f /var/log/paper-live/layer3_history.jsonl ]]; then
            stat -c %Y /var/log/paper-live/layer3_history.jsonl 2>/dev/null \
                || stat -f %m /var/log/paper-live/layer3_history.jsonl 2>/dev/null \
                || echo 0
        else
            echo "missing"
        fi
        echo "=== alerts ==="
        # Patterns mirror the historically-unnoticed alerts (see header §5).
        # The date cutoff on the slog "time" field is load-bearing: the log
        # dir contains stale never-rotated files (retired testnet-engine@,
        # removed symbols) whose old lines would otherwise surface forever.
        cutoff=$(date -u -d "2 days ago" +%Y-%m-%d)
        grep -hE "\"level\":\"ERROR\"|signal blocked by safety gate|signal rejected|position drift detected|telegram send failed" \
            /var/log/paper-live/*.log /var/log/paper-live/*.log.1 2>/dev/null \
            | awk -F"\"" -v c="$cutoff" "substr(\$4,1,10) >= c" | tail -8
        echo "=== ts ==="
        date -u +%s
    ' 2>/dev/null
)

if [[ -z "$remote_combined" ]]; then
    add_warn "SSH to ${VPS} failed or returned empty — cannot check services/drift/layer3 freshness"
else
    # Parse sections by sentinel markers.
    active_services=$(echo "$remote_combined" | awk '/=== services ===/{f=1;next}/===/{f=0}f')
    failed_services=$(echo "$remote_combined" | awk '/=== failed ===/{f=1;next}/===/{f=0}f')
    drift_mtime=$(echo "$remote_combined" | awk '/=== drift ===/{getline; print}')
    layer3_mtime=$(echo "$remote_combined" | awk '/=== layer3 ===/{getline; print}')
    remote_now=$(echo "$remote_combined" | awk '/=== ts ===/{getline; print}')

    n_active=$(echo "$active_services" | grep -c '\.service$' || true)
    n_failed=$(echo "$failed_services" | grep -c '\.service$' || true)

    if [[ "$n_active" -ge 16 ]] && [[ "$n_failed" -eq 0 ]]; then
        add_ok "${n_active} services active on VPS, 0 failed"
    elif [[ "$n_failed" -gt 0 ]]; then
        add_warn "VPS has ${n_failed} failed service(s). ssh ${VPS} 'systemctl --failed'"
    else
        add_warn "VPS only ${n_active} services active (expected ≥16 — deployed-16 paper-live cohort)"
    fi

    # --- 3. Drift detector freshness ---
    if [[ "$drift_mtime" == "missing" ]]; then
        add_warn "drift_check_history.jsonl missing on VPS — drift detector may have never run"
    elif [[ "$drift_mtime" =~ ^[0-9]+$ ]] && [[ "$remote_now" =~ ^[0-9]+$ ]]; then
        age_seconds=$(( remote_now - drift_mtime ))
        age_days=$(( age_seconds / 86400 ))
        if [[ "$age_days" -le "$MAX_AGE_DAYS" ]]; then
            add_ok "drift detector last ran ${age_days}d ago (≤${MAX_AGE_DAYS}d)"
        else
            add_warn "drift detector last ran ${age_days}d ago (>${MAX_AGE_DAYS}d) — weekly cron may be broken"
        fi
    fi

    # --- 4. Layer 3 cron freshness ---
    if [[ "$layer3_mtime" == "missing" ]]; then
        # Distinguish "not yet installed" from "installed but hasn't fired". A
        # freshly-installed cron with no firings yet is informational, not a warning.
        add_info "Layer 3 cron history not yet written (cron may not be installed OR first firing pending — Sunday 10:00 UTC)"
    elif [[ "$layer3_mtime" =~ ^[0-9]+$ ]] && [[ "$remote_now" =~ ^[0-9]+$ ]]; then
        age_seconds=$(( remote_now - layer3_mtime ))
        age_days=$(( age_seconds / 86400 ))
        if [[ "$age_days" -le "$MAX_AGE_DAYS" ]]; then
            add_ok "Layer 3 cron last ran ${age_days}d ago (≤${MAX_AGE_DAYS}d)"
        else
            add_warn "Layer 3 cron last ran ${age_days}d ago (>${MAX_AGE_DAYS}d) — weekly cron may be broken"
        fi
    fi

    # --- 5. Alert-worthy engine-log lines (~48h) ---
    # Missing sentinel is a WARN, not a silent pass — the remote script
    # always emits it, so its absence means truncated/partial SSH output
    # (audit-lens: never fail open on missing data).
    if ! echo "$remote_combined" | grep -q '^=== alerts ==='; then
        add_warn "alerts section missing from remote output — cannot verify engine-log alerts"
    else
        alert_lines=$(echo "$remote_combined" | awk '/=== alerts ===/{f=1;next}/===/{f=0}f')
        if [[ -z "$alert_lines" ]]; then
            add_ok "no alert-worthy engine-log lines (last ~48h of VPS logs)"
        else
            n_alerts=$(echo "$alert_lines" | grep -c .)
            add_warn "${n_alerts} alert-worthy engine-log line(s) on VPS (last ~48h) — samples in ⓘ below; check the engine log at each ts"
            while IFS= read -r aline; do
                add_info "alert: ${aline:0:200}"
            done <<< "$(echo "$alert_lines" | head -5)"
        fi
    fi
fi

# --- output ---
n_warn=${#WARNINGS[@]}
n_ok=${#CLEAN_CHECKS[@]}

if [[ "$QUIET" == "1" ]]; then
    if [[ "$n_warn" -eq 0 ]]; then
        echo "session_start_check: CLEAN (${n_ok} ok, 0 warnings)"
    else
        echo "session_start_check: ${n_warn} WARN, ${n_ok} ok"
    fi
else
    echo "================================================================================"
    echo "  session_start_check  $(date -u '+%Y-%m-%d %H:%M UTC')"
    echo "================================================================================"
    if [[ "$n_ok" -gt 0 ]]; then
        for c in "${CLEAN_CHECKS[@]}"; do
            echo "  ✓  $c"
        done
    fi
    if [[ "$n_warn" -gt 0 ]]; then
        echo
        for w in "${WARNINGS[@]}"; do
            echo "  ⚠  $w"
        done
    fi
    if [[ ${#INFO_LINES[@]} -gt 0 ]]; then
        echo
        for i in "${INFO_LINES[@]}"; do
            echo "  ⓘ  $i"
        done
    fi
    echo
    if [[ "$n_warn" -eq 0 ]]; then
        echo "  → CLEAN — safe to proceed"
    else
        echo "  → ${n_warn} WARN — review before substantive work"
    fi
    echo "================================================================================"
fi

# Exit code
if [[ "$STRICT" == "1" ]] && [[ "$n_warn" -gt 0 ]]; then
    exit 1
fi
exit 0
