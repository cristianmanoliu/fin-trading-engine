#!/usr/bin/env bash
set -euo pipefail

# Build the backtest binary (for the reconciliation comparison), then run the
# journal report tool. All flags are passed through to journal_report.
#
# Usage:
#   ./scripts/paper_live_report.sh [--journal-dir ./logs/journal] [--config-dir ./configs]

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BT_BIN="${ROOT}/bin/backtest"

echo "→ Building binaries..."
(cd "${ROOT}" && go build -o "${BT_BIN}" ./cmd/backtest)
(cd "${ROOT}" && go build -o "${ROOT}/bin/journal_report" ./cmd/journal_report)

echo ""
"${ROOT}/bin/journal_report" --backtest-bin "${BT_BIN}" "$@"
