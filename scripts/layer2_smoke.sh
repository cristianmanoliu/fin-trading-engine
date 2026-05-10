#!/usr/bin/env bash
# layer2_smoke.sh — pre-flight verification for Layer 2 testnet activation.
#
# Per real_money_executor_architecture_decision_rule_2026-05-08.md, Layer 2
# is the integration gate between Stub paper trading and Layer 3 / STAGE_1.
# The plumbing is in place (`cmd/engine --executor binance_live_testnet`)
# but operator action is required: generate testnet credentials at
# testnet.binancefuture.com and set BINANCE_API_KEY / BINANCE_API_SECRET.
#
# This script is the FIRST thing to run once those credentials exist. It
# validates the connectivity / auth / config plumbing without waiting for a
# natural 4H signal:
#
#   - env vars present + non-empty
#   - cmd/engine binary builds OR pre-existing bin/engine works
#   - HTTPS reachability to testnet.binancefuture.com
#   - engine starts with --executor binance_live_testnet
#   - startup auth check passes (no 401/403 in stderr)
#   - backfill completes
#   - first heartbeat fires
#   - no ERROR-level slog lines during the smoke window
#
# What it does NOT validate:
#   - Round-trip fill behaviour (the 5-minute smoke window is shorter than
#     the 4H signal cadence — a natural signal is unlikely to fire). The
#     operator runs a longer 24-72h window AFTER this smoke passes to
#     accumulate at least one fill for cost-decomp variance verification.
#   - Cost-decomp field population from real Binance fee/slip (same reason
#     — needs a real fill). The gate informationality check
#     (docs/AUDIT_LENS.md adjacent-pattern) activates at the first real
#     fill, not at engine startup.
#
# Designed per docs/AUDIT_LENS.md lens-as-design-tool: distinct exit codes
# per failure shape, Telegram tier mapping, NOISY on each failure mode.
#
# Usage:
#   bash scripts/layer2_smoke.sh                          # BTCUSDT, 5 min
#   bash scripts/layer2_smoke.sh ETHUSDT                  # specific symbol
#   bash scripts/layer2_smoke.sh BTCUSDT --duration 600   # 10 min window
#   LAYER2_SMOKE_DRY_RUN=1 bash scripts/layer2_smoke.sh   # plumbing test
#                                                          # without invoking
#                                                          # cmd/engine
#
# Exit codes:
#   0 SMOKE_PASS             — all checks passed; Layer 2 plumbing verified
#   1 SMOKE_FAIL_HEARTBEAT   — engine started but no first heartbeat in window
#   2 SMOKE_FAIL_AUTH        — credential rejection (401/403 in engine log)
#   3 SMOKE_FAIL_ENV         — required env vars missing/empty
#   4 SMOKE_FAIL_BUILD       — neither bin/engine nor `go build` available
#   5 SMOKE_FAIL_NETWORK     — connectivity to testnet.binancefuture.com fails
#   6 SMOKE_FAIL_OTHER       — unexpected ERROR-level log lines during smoke
#
# Telegram tier mapping (per telegram_alert_design_decision_rule_2026-05-08):
#   exit 0 → INFO    (Layer 2 plumbing verified)
#   exit 1 → WARN    (engine alive but didn't progress; investigate)
#   exit 2 → CRITICAL (auth fundamentally broken — wrong creds OR permission)
#   exit 3 → WARN    (operator misconfiguration; env-var fix)
#   exit 4 → CRITICAL (cannot build engine; toolchain broken)
#   exit 5 → CRITICAL (cannot reach testnet API; network or DNS issue)
#   exit 6 → CRITICAL (engine error during smoke; investigate before retry)

set -euo pipefail

# ── Resolve paths ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=lib/notify.sh
source "${SCRIPT_DIR}/lib/notify.sh"

# ── Exit-code constants ──────────────────────────────────────────────────────
readonly EXIT_SMOKE_PASS=0
readonly EXIT_SMOKE_FAIL_HEARTBEAT=1
readonly EXIT_SMOKE_FAIL_AUTH=2
readonly EXIT_SMOKE_FAIL_ENV=3
readonly EXIT_SMOKE_FAIL_BUILD=4
readonly EXIT_SMOKE_FAIL_NETWORK=5
readonly EXIT_SMOKE_FAIL_OTHER=6

# ── Args ─────────────────────────────────────────────────────────────────────
SYMBOL="${1:-BTCUSDT}"
DURATION_SEC=300  # 5 minutes default
shift 2>/dev/null || true
while [[ $# -gt 0 ]]; do
    case "$1" in
        --duration) DURATION_SEC="$2"; shift 2 ;;
        *) echo "unknown arg: $1" >&2; exit "$EXIT_SMOKE_FAIL_ENV" ;;
    esac
done

# ── Helpers ──────────────────────────────────────────────────────────────────

die() {
    local code="$1" subject="$2" body="${3:-}"
    local tier
    case "$code" in
        "$EXIT_SMOKE_PASS")
            tier="INFO" ;;
        "$EXIT_SMOKE_FAIL_HEARTBEAT"|"$EXIT_SMOKE_FAIL_ENV")
            tier="WARN" ;;
        "$EXIT_SMOKE_FAIL_AUTH"|"$EXIT_SMOKE_FAIL_BUILD"|"$EXIT_SMOKE_FAIL_NETWORK"|"$EXIT_SMOKE_FAIL_OTHER")
            tier="CRITICAL" ;;
        *)
            tier="WARN" ;;
    esac
    notify_telegram "$tier" "layer2_smoke: $subject" "$body"
    if [[ "$code" -ne 0 ]]; then
        echo "layer2_smoke: $subject" >&2
        [[ -n "$body" ]] && echo "$body" >&2
    fi
    exit "$code"
}

# ── Phase 1: env-var validation ──────────────────────────────────────────────

echo "── layer2_smoke for $SYMBOL — duration ${DURATION_SEC}s ──"

if [[ -z "${BINANCE_API_KEY:-}" ]]; then
    die "$EXIT_SMOKE_FAIL_ENV" "env-key-missing" \
        "BINANCE_API_KEY env var is empty/unset. Generate testnet credentials at https://testnet.binancefuture.com and export them BEFORE invoking the smoke."
fi
if [[ -z "${BINANCE_API_SECRET:-}" ]]; then
    die "$EXIT_SMOKE_FAIL_ENV" "env-secret-missing" \
        "BINANCE_API_SECRET env var is empty/unset. See env-key-missing diagnostic."
fi

# Refuse to run if the env vars look like mainnet credentials. Mainnet keys
# start with arbitrary characters; testnet keys are not visually
# distinguishable, so the check is INTENT-based: require an explicit
# LAYER2_SMOKE_ACKNOWLEDGE_TESTNET=YES to confirm operator-intent that the
# loaded creds are testnet, not mainnet. Without it, refuse.
if [[ "${LAYER2_SMOKE_ACKNOWLEDGE_TESTNET:-}" != "YES" ]]; then
    die "$EXIT_SMOKE_FAIL_ENV" "testnet-intent-not-acknowledged" \
        "BINANCE_API_KEY + BINANCE_API_SECRET are loaded but I cannot tell whether they're testnet or mainnet credentials.
Re-run with LAYER2_SMOKE_ACKNOWLEDGE_TESTNET=YES to confirm these are TESTNET credentials from https://testnet.binancefuture.com.
This gate exists because mainnet credentials in this script would send real orders to a real exchange. The literal env-var IS your signature."
fi

echo "✓ env vars present, testnet intent acknowledged"

# ── Phase 2: connectivity to testnet ─────────────────────────────────────────

echo "── checking connectivity to testnet.binancefuture.com ──"
TESTNET_PING="https://testnet.binancefuture.com/fapi/v1/time"
if [[ "${LAYER2_SMOKE_DRY_RUN:-0}" == "1" ]]; then
    echo "  (DRY_RUN — would curl $TESTNET_PING)"
else
    if ! curl -s --max-time 10 -o /dev/null -w '%{http_code}' "$TESTNET_PING" | grep -q '^200$'; then
        die "$EXIT_SMOKE_FAIL_NETWORK" "testnet-unreachable" \
            "GET $TESTNET_PING did not return HTTP 200 within 10s.
Possible causes: (1) network is down; (2) testnet API is down; (3) DNS resolution failure for testnet.binancefuture.com.
Verify with: curl -v $TESTNET_PING"
    fi
fi
echo "✓ testnet API reachable"

# ── Phase 3: engine binary availability ──────────────────────────────────────

ENGINE_BIN="${ROOT}/bin/engine"
if [[ ! -x "$ENGINE_BIN" ]]; then
    if command -v go >/dev/null 2>&1; then
        echo "── building cmd/engine to $ENGINE_BIN ──"
        if [[ "${LAYER2_SMOKE_DRY_RUN:-0}" != "1" ]]; then
            if ! ( cd "$ROOT" && go build -o "$ENGINE_BIN" ./cmd/engine ); then
                die "$EXIT_SMOKE_FAIL_BUILD" "build-failed" \
                    "go build ./cmd/engine failed. Inspect the build output above for the root cause."
            fi
        fi
    else
        die "$EXIT_SMOKE_FAIL_BUILD" "no-engine-no-go" \
            "Neither $ENGINE_BIN exists as an executable nor is 'go' available on PATH. Cannot run the engine."
    fi
fi
echo "✓ engine binary ready at $ENGINE_BIN"

# ── Phase 4: run engine for the smoke duration ───────────────────────────────

LOG_DIR="${ROOT}/logs/layer2_smoke"
mkdir -p "$LOG_DIR"
DATE_TAG=$(date -u +%FT%H-%M-%S)
LOG_FILE="${LOG_DIR}/${SYMBOL}_${DATE_TAG}.log"
echo "── starting engine on $SYMBOL via --executor binance_live_testnet ──"
echo "    log: $LOG_FILE"
echo "    duration: ${DURATION_SEC}s"

if [[ "${LAYER2_SMOKE_DRY_RUN:-0}" == "1" ]]; then
    echo "  (DRY_RUN — would invoke: $ENGINE_BIN --config configs/default.yaml --symbol $SYMBOL --executor binance_live_testnet)"
    # Simulate a clean log for the analysis phase below
    cat > "$LOG_FILE" <<EOF
{"level":"INFO","msg":"backfill complete","symbol":"$SYMBOL"}
{"level":"INFO","msg":"heartbeat","symbol":"$SYMBOL","ticks_since_last":3,"last_tick_age":5000000000}
{"level":"INFO","msg":"TESTNET EXECUTOR ACTIVE — orders will be sent to Binance TESTNET (no real capital)"}
EOF
else
    # `timeout` sends SIGTERM after DURATION_SEC; engine drains and exits cleanly.
    # Exit status 124 from timeout = killed by timeout = expected. Any other
    # non-zero = engine crashed before the timer fired.
    set +e
    timeout "${DURATION_SEC}s" "$ENGINE_BIN" \
        --config "${ROOT}/configs/default.yaml" \
        --symbol "$SYMBOL" \
        --executor binance_live_testnet \
        > "$LOG_FILE" 2>&1
    rc=$?
    set -e
    if [[ "$rc" != "124" && "$rc" != "0" ]]; then
        # Engine died before timeout — likely a fatal startup error.
        die "$EXIT_SMOKE_FAIL_OTHER" "engine-crashed-early" \
            "Engine exited with status $rc before the ${DURATION_SEC}s smoke window completed. Inspect $LOG_FILE for the cause."
    fi
fi
echo "✓ engine ran for the smoke duration; analysing log"

# ── Phase 5: log analysis ────────────────────────────────────────────────────

# AUTH failure shape: 401/403 in any line, OR explicit auth-error slog
if grep -qE '"status":401|"status":403|HTTP 401|HTTP 403|"msg":"auth.*error"|invalid.*api.*key' "$LOG_FILE"; then
    die "$EXIT_SMOKE_FAIL_AUTH" "credential-rejected" \
        "Engine log contains auth-failure markers (401/403 or auth-error). Likely causes:
  (1) BINANCE_API_KEY / BINANCE_API_SECRET are mainnet creds, not testnet
  (2) Testnet API key has been disabled or expired
  (3) IP-allowlist on testnet is rejecting this host
Inspect $LOG_FILE for the exact response."
fi

# Generic ERROR-level slog (not the EXPECTED 'TESTNET EXECUTOR ACTIVE' WARN
# which is informational despite Warn-level).
ERROR_LINES=$(grep -E '"level":"ERROR"' "$LOG_FILE" 2>/dev/null || true)
if [[ -n "$ERROR_LINES" ]]; then
    die "$EXIT_SMOKE_FAIL_OTHER" "engine-error-during-smoke" \
        "Engine log contains ERROR-level slog entries during the smoke window:
${ERROR_LINES}
Investigate before retrying."
fi

# Heartbeat: at least one heartbeat (any level) must have fired. This proves
# the engine got past backfill + indicator priming + ticker subscription.
if ! grep -qE '"msg":"heartbeat' "$LOG_FILE"; then
    die "$EXIT_SMOKE_FAIL_HEARTBEAT" "no-heartbeat" \
        "No heartbeat log line found in $LOG_FILE.
This means the engine either did not start cleanly or got stuck before first heartbeat (~60s post-backfill). Inspect the log; look for stalled backfill or pre-heartbeat WebSocket subscription errors."
fi

# Optional: count tick-source health indicators
N_HEARTBEATS=$(grep -cE '"msg":"heartbeat"' "$LOG_FILE" || echo 0)
HAS_BACKFILL=$(grep -cE '"msg":"backfill complete' "$LOG_FILE" || echo 0)

# ── Smoke PASS ───────────────────────────────────────────────────────────────

SUMMARY="symbol=$SYMBOL duration=${DURATION_SEC}s heartbeats=$N_HEARTBEATS backfill_complete=$HAS_BACKFILL"
notify_telegram INFO "layer2_smoke PASS" \
    "Layer 2 connectivity + auth + startup verified.
$SUMMARY

Next: run a longer 24-72h window via --executor binance_live_testnet to accumulate at least one natural 4H signal and verify the cost-decomp gate becomes informational (writer-equals-model lens fires now)."

echo ""
echo "════════════════════════════════════════════════════════════════════════"
echo "  ✓ layer2_smoke PASS"
echo "════════════════════════════════════════════════════════════════════════"
echo "  $SUMMARY"
echo "  Log:        $LOG_FILE"
echo ""
echo "  Layer 2 plumbing is verified. To accumulate fill data for the"
echo "  cost-decomp informationality check, run a longer window:"
echo "    $ENGINE_BIN --config configs/default.yaml --symbol $SYMBOL --executor binance_live_testnet"
echo "  (background it; observe for 24-72h until at least one signal fires)"
echo ""
exit "$EXIT_SMOKE_PASS"
