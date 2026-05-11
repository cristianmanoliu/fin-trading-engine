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

# Internal: pick prefix + retry count for a severity tier. Echoes the
# pair as "prefix|retries" for the caller to split. Unknown severity is
# treated as CRITICAL with an explicit [UNKNOWN SEVERITY: ...] tag — fail
# loud, never silently downgrade a typo'd alert to INFO.
#
# The original case-with-`*) INFO` was a quiet downgrade: a caller that
# wrote `notify_telegram CRITCAL ...` (typo) or `notify_telegram critical
# ...` (lowercase) saw their CRITICAL turn into INFO at 1 retry with no
# visual prefix indicating something was wrong. Same shape as the
# missing-input → silent-success audit pattern: malformed input maps
# silently into the lowest-urgency branch. Pinned by F6 of
# scripts/test_notify.sh.
_notify_select_tier() {
    case "$1" in
        CRITICAL) echo "🚨 [CRITICAL]|3" ;;
        WARN)     echo "⚠ [WARN]|2" ;;
        INFO)     echo "ℹ [INFO]|1" ;;
        *)        echo "🚨 [CRITICAL] [UNKNOWN SEVERITY: $1]|3" ;;
    esac
}

# Telegram API hard limit on `text` parameter: 4096 chars after entity
# parsing (per https://core.telegram.org/bots/api#sendmessage). A body
# exceeding this is rejected with HTTP 400; curl exits non-zero; the
# retry loop tries again with the same body; alert is silently dropped.
# Same family as the silent-on-corrupt-input pattern (locked across 6
# implementations as of 2026-05-11) applied to outbound API contracts:
# malformed-by-size input maps to silently no-op.
#
# Most current callers use `tail -3` or `tail -1` to bound subprocess
# output included in alert bodies. But: (a) future callers might forget
# the tail-bound, and (b) `tail -3` of a script with absurdly long lines
# can still exceed 4096. Truncate at the helper boundary so EVERY
# notify_telegram invocation is safe by construction.
readonly _NOTIFY_MAX_TEXT_LEN=4096

# Truncate `text` to ≤4096 chars, appending a visible marker so the
# operator knows truncation happened. Returns the (possibly-truncated)
# text on stdout. Pure function — sourceable for tests.
_notify_truncate_text() {
    local text="$1"
    local len=${#text}
    if [[ "$len" -le "$_NOTIFY_MAX_TEXT_LEN" ]]; then
        printf '%s' "$text"
        return 0
    fi
    # Reserve space for the marker. The marker itself is short + ASCII
    # so its length is stable; budget accordingly.
    local marker=$'\n…[truncated]'
    local marker_len=${#marker}
    local keep=$(( _NOTIFY_MAX_TEXT_LEN - marker_len ))
    printf '%s%s' "${text:0:$keep}" "$marker"
}

# Send a Telegram alert. Returns 0 always (calling script must not fail
# because of a notification path failure).
notify_telegram() {
    local severity="$1" subject="$2" body="${3:-}"
    local token="${TELEGRAM_BOT_TOKEN:-}"
    local chat="${TELEGRAM_CHAT_ID:-}"
    [[ -z "$token" || -z "$chat" ]] && return 0

    local tier prefix retries
    tier=$(_notify_select_tier "$severity")
    prefix="${tier%|*}"
    retries="${tier##*|}"

    local text="${prefix} ${subject}"
    if [[ -n "$body" ]]; then
        text="${text}
${body}"
    fi
    # Telegram-size truncation: see _NOTIFY_MAX_TEXT_LEN above. Without
    # this, an oversized message returns HTTP 400 on every retry and
    # the alert silently drops.
    text=$(_notify_truncate_text "$text")

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
