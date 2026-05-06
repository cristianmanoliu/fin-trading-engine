#!/usr/bin/env bash
# gen_configs.sh — generate per-symbol configs from the btcusdt.yaml template.
# Idempotent: skips symbols that already have a config file.
# Usage: ./scripts/gen_configs.sh ADAUSDT AVAXUSDT ...
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEMPLATE="${ROOT}/configs/btcusdt.yaml"

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 SYMBOL [SYMBOL ...]" >&2
    exit 1
fi

for sym in "$@"; do
    sym_upper="$(echo "$sym" | tr '[:lower:]' '[:upper:]')"
    sym_lower="$(echo "$sym" | tr '[:upper:]' '[:lower:]')"
    dest="${ROOT}/configs/${sym_lower}.yaml"

    if [[ -f "$dest" ]]; then
        echo "  skip ${sym_lower}.yaml (already exists)"
        continue
    fi

    sed \
        -e "s/symbol: BTCUSDT/symbol: ${sym_upper}/" \
        -e "s|BTCUSDT-1m-2024-01|${sym_upper}-1m-2024-01|" \
        "$TEMPLATE" > "$dest"

    echo "  created ${sym_lower}.yaml"
done
