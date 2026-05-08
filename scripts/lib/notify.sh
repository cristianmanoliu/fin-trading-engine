#!/usr/bin/env bash
# notify.sh — shared Telegram alert helper for bash operational scripts.
#
# Mirrors the Go engine's pkg/notify/telegram.go semantics:
#   - tier-aware retry: CRITICAL 3, WARN 2, INFO 1 (jittered exp backoff)
#   - graceful no-op when TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID env vars unset
#   - bot token never written to stdout/stderr (curl output silenced)
#   - never fails the calling script even if all retries exhaust
#
# Usage from a script:
#   source "$(dirname "$0")/lib/notify.sh"
#   notify_telegram CRITICAL "subject of alert" "body lines
#   here"
#
# The first arg is severity (CRITICAL | WARN | INFO). The second is a short
# subject line. The third is the message body — typically multi-line.
#
# Why a shared library: run_drift_check.sh and post_deploy_check.sh both
# need this; without dedup, the tier-retry behavior had to be added to two
# places independently. A drift between the two would have been a real
# operational bug (one script alerting reliably, the other silently dropping
# under transient network failure).

# Send a Telegram alert. Returns 0 always (calling script must not fail
# because of a notification path failure).
notify_telegram() {
    local severity="$1" subject="$2" body="${3:-}"
    local token="${TELEGRAM_BOT_TOKEN:-}"
    local chat="${TELEGRAM_CHAT_ID:-}"
    [[ -z "$token" || -z "$chat" ]] && return 0

    local prefix retries
    case "$severity" in
        CRITICAL) prefix="🚨 [CRITICAL]"; retries=3 ;;
        WARN)     prefix="⚠ [WARN]";     retries=2 ;;
        *)        prefix="ℹ [INFO]";     retries=1 ;;
    esac

    local text="${prefix} ${subject}"
    if [[ -n "$body" ]]; then
        text="${text}
${body}"
    fi

    local attempt delay=1
    for (( attempt=0; attempt < retries; attempt++ )); do
        # All output silenced so the bot token (embedded in URL) never
        # surfaces via stderr. Curl exit codes drive retry.
        if curl -s --max-time 10 \
            -X POST "https://api.telegram.org/bot${token}/sendMessage" \
            --data-urlencode "chat_id=${chat}" \
            --data-urlencode "text=${text}" \
            >/dev/null 2>&1; then
            return 0
        fi
        # Exponential backoff with small jitter (avoid thundering-herd if
        # multiple scripts retry against a flaky network at once).
        sleep "$(awk -v d="$delay" 'BEGIN{srand(); print d + rand()*0.3}')"
        delay=$(( delay * 2 ))
    done
    # Final failure → silently give up. Calling script logs locally
    # regardless; the notification is best-effort.
    return 0
}
