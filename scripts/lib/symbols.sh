#!/usr/bin/env bash
# scripts/lib/symbols.sh — single source of truth reader for bash scripts.
#
# Usage (from any script in scripts/ or deploy/):
#   source "$(dirname "$0")/lib/symbols.sh"             # if caller in scripts/
#   source "$(dirname "$0")/../scripts/lib/symbols.sh"  # if caller in deploy/
#   SYMBOLS=$(get_symbols universe)              # space-separated, uppercase
#   SYMBOLS_LC=$(get_symbols deployed lower)     # space-separated, lowercase
#
# Available groups: universe, deployed, persistent_losers, regime_flippers
# (full list authoritative in configs/symbols.yaml).
#
# Requires: python3 (stdlib only — no pyyaml needed; we parse the simple
# flat-list YAML format manually to keep this dependency-free).
# Source: configs/symbols.yaml relative to repo root.

_SYMBOLS_YAML="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/configs/symbols.yaml"

get_symbols() {
    local group="${1:?get_symbols: group name required (universe | deployed | persistent_losers | regime_flippers)}"
    local case="${2:-upper}"
    if [[ ! -f "$_SYMBOLS_YAML" ]]; then
        echo "get_symbols: $_SYMBOLS_YAML not found" >&2
        return 1
    fi
    python3 - "$group" "$case" "$_SYMBOLS_YAML" <<'EOF' || { echo "get_symbols: failed to parse $_SYMBOLS_YAML" >&2; return 1; }
import sys

group, case, path = sys.argv[1], sys.argv[2], sys.argv[3]
in_group = False
syms = []
groups_seen = []
with open(path) as f:
    for raw in f:
        line = raw.rstrip("\n")
        # Strip inline comments (after first '#' that isn't inside a string).
        if "#" in line:
            line = line[: line.index("#")].rstrip()
        if not line.strip():
            continue
        # Top-level key (no leading whitespace + ends with ':')
        if line[0] not in (" ", "\t") and line.rstrip().endswith(":"):
            key = line.rstrip()[:-1]
            groups_seen.append(key)
            in_group = (key == group)
            continue
        # List item under current group
        if in_group:
            stripped = line.strip()
            if stripped.startswith("- "):
                sym = stripped[2:].strip()
                if sym:
                    syms.append(sym)
if not in_group and group not in groups_seen:
    sys.stderr.write(f"get_symbols: group '{group}' not in {groups_seen}\n")
    sys.exit(2)
print(" ".join(s.lower() if case == "lower" else s for s in syms))
EOF
}
