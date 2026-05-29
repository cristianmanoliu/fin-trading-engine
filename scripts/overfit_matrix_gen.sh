#!/usr/bin/env bash
# overfit_matrix_gen.sh — Generate the months × configs NET returns matrix for
# backtest-overfitting / PBO / DSR analysis.
#
# For each of 34 configs × 57 symbols: run a continuous 5-year backtest with
# --journal-dir, then reduce all per-config journals into a single CSV matrix.
#
# Usage:
#   bash scripts/overfit_matrix_gen.sh [out_csv]
#
# Env overrides (useful for smoke-testing a tiny slice):
#   SYMBOLS="BTCUSDT ETHUSDT"    — run on a subset of symbols
#   MANIFEST=/tmp/mini.csv       — use a smaller config manifest
#   OUT_CSV=/tmp/matrix.csv      — override output path
#
# Output: results/overfit_returns_matrix_2026-05-29.csv (or OUT_CSV)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ── Defaults ────────────────────────────────────────────────────────────────
MANIFEST="${MANIFEST:-${ROOT}/results/overfit_configs_2026-05-29.csv}"
OUT_CSV="${OUT_CSV:-${1:-${ROOT}/results/overfit_returns_matrix_2026-05-29.csv}}"

# ── Compile binary once ──────────────────────────────────────────────────────
BINARY=$(mktemp /tmp/overfit-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/overfit-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

echo "→ Compiling backtest binary..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Binary: $BINARY"

# ── Symbols ──────────────────────────────────────────────────────────────────
source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

# ── Date range ───────────────────────────────────────────────────────────────
START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

# ── Merged CSVs (one per symbol, reused across all configs) ──────────────────
MERGED_DIR="${WORKDIR}/merged"
mkdir -p "$MERGED_DIR"

echo "→ Merging per-symbol 5y CSVs (ragged history expected)..."
for sym in $SYMBOLS; do
    merged="${MERGED_DIR}/${sym}.csv"
    found=0
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        last=$(( y == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            mm=$(printf '%02d' "$m")
            csv="${ROOT}/data/${sym}-1m-${y}-${mm}.csv"
            if [[ -f "$csv" ]]; then
                cat "$csv" >> "$merged"
                found=1
            fi
        done
    done
    if [[ $found -eq 0 ]]; then
        # No data for this symbol — skip (do NOT create an empty file; runner
        # checks for non-empty merged file below)
        echo "  skip (no data): $sym"
    fi
done
echo "→ Merge complete."

# ── Verify configs/default.yaml has stake_usd (required for pnl_usd output) ──
if ! grep -q 'stake_usd' "${ROOT}/configs/default.yaml"; then
    echo "→ stake_usd missing from configs/default.yaml — injecting 1000"
    SED_STAKE="s|^backtest:$|  stake_usd: 1000\nbacktest:|"
else
    SED_STAKE=""
fi

# ── BASE SED to normalise the config for all runs ────────────────────────────
# Mirror continuous_sweep.sh: set ema_mode true, disable other modes.
# target_rr is set via YAML (no --target-rr CLI flag in cmd/backtest — it is
# a config-only field; default.yaml already has 6.0 but we pin it explicitly).
SED_BASE="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 6.0|"
if [[ -n "$SED_STAKE" ]]; then
    SED_BASE="${SED_BASE}; ${SED_STAKE}"
fi

# ── BASE CLI flags (LIVE production config, applied to every run) ─────────────
# extra_flags from the manifest are appended AFTER these so they win (last-wins
# on repeated flags — verified for Go's flag package).
# NOTE: --target-rr is NOT a CLI flag in cmd/backtest; target_rr is set via sed
# in the YAML config above (pinned to 6.0 for every run).
BASE_FLAGS="--signal-tf 4H --side-filter short --max-hold-hours 504 \
  --ema-fast-period 9 --ema-slow-period 21 \
  --funding-csv-dir ${ROOT}/data/funding \
  --fee-bps 10 --stop-slippage-bps 5 \
  --exact-fills --include-boundary --pessimistic-ambiguous"

# ── Per-symbol runner (called via xargs -P) ───────────────────────────────────
# Exported as a function; env vars must be exported separately.
run_symbol_for_config() {
    local sym=$1
    local label=$2
    local extra_flags=$3      # may be empty
    local jdir=$4             # --journal-dir destination

    local merged="${MERGED_DIR}/${sym}.csv"
    [[ -s "$merged" ]] || return 0  # no data for this symbol — skip silently

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${label}-${sym}.XXXXXXXX")
    # Set csv_path to the merged CSV (same sed pattern as continuous_sweep.sh)
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${merged}|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

    # Run backtest — errors are non-fatal; suppress all stdout/stderr.
    # shellcheck disable=SC2086
    "$BINARY" --config "$cfg" \
        --symbol "$sym" \
        $BASE_FLAGS \
        $extra_flags \
        --journal-dir "$jdir" \
        >/dev/null 2>&1 || true

    rm -f "$cfg"
}
export -f run_symbol_for_config
export BINARY WORKDIR MERGED_DIR SED_BASE BASE_FLAGS ROOT

# ── Journals root ─────────────────────────────────────────────────────────────
JROOT="${WORKDIR}/journals"
mkdir -p "$JROOT"

# ── Iterate configs from manifest ─────────────────────────────────────────────
echo "→ Processing configs from: $MANIFEST"
echo "→ Parallelism: $NCPU"

first=1
while IFS=, read -r label extra_flags; do
    # Skip header row
    if [[ $first -eq 1 ]]; then
        first=0
        continue
    fi
    # Trim any trailing carriage returns (Windows line endings)
    label="${label%%$'\r'}"
    extra_flags="${extra_flags%%$'\r'}"

    jdir="${JROOT}/${label}"
    mkdir -p "$jdir"

    echo "→ Config: ${label} | extra_flags: [${extra_flags}]"

    # Parallelise across symbols using xargs.
    # The symbol is injected via -I{} into position $0 of bash -c; label/extra_flags/jdir
    # are passed as positional parameters $1/$2/$3 after the '--' separator so that
    # spaces in extra_flags are preserved correctly.
    printf '%s\n' $SYMBOLS \
        | xargs -P "$NCPU" -I{} bash -c \
            'run_symbol_for_config "{}" "$1" "$2" "$3"' \
            _ "$label" "$extra_flags" "$jdir"

done < "$MANIFEST"

echo "→ All config/symbol runs complete."

# ── Reduce journals → matrix CSV ──────────────────────────────────────────────
echo "→ Reducing journals to matrix CSV: $OUT_CSV"
python3 "${ROOT}/scripts/overfit_reduce_journals.py" \
    "$JROOT" \
    "$MANIFEST" \
    "$OUT_CSV"

echo "→ Done. Matrix written to: $OUT_CSV"
