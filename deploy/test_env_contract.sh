#!/usr/bin/env bash
# test_env_contract.sh — lock the /etc/paper-live/env format contract.
#
# Regression guard for the 2026-05-29 silent-Telegram bug: `export KEY=VAL` in
# the env file makes systemd EnvironmentFile= log "Ignoring invalid environment
# assignment" and DROP the var, so engine-side TELEGRAM_*/signal-context go
# silently no-op (commit d6d6061 introduced exactly this by adding `export` for
# cron inheritance). The contract:
#   - install.sh writes PLAIN KEY=VALUE (systemd EnvironmentFile-valid), and
#   - the cron installers source with `set -a` so cron CHILDREN still inherit.
# Prod-side complement: scripts/post_deploy_check.sh §8c. Fix/activation:
# results/telegram_env_systemd_fix_runbook_2026-05-30.md.
#
# Run: bash deploy/test_env_contract.sh   (exit 0 = contract holds, 1 = violated)
set -uo pipefail
cd "$(dirname "$0")/.."
FAIL=0
pass() { echo "  ok   $*"; }
fail() { echo "  FAIL $*"; FAIL=$((FAIL + 1)); }

echo "env-format contract"

# 1. install.sh must NOT write export-prefixed TELEGRAM/PAPER_LIVE vars
#    (systemd EnvironmentFile drops them).
if grep -qE '^export (TELEGRAM|PAPER_LIVE)' deploy/install.sh; then
    fail "install.sh writes export-prefixed env vars — systemd will drop them"
else
    pass "install.sh writes plain KEY=VALUE (systemd-valid)"
fi

# 2. each cron installer must source the env with `set -a` so the cron child
#    inherits the (now non-exported) plain vars.
for f in deploy/install_funding_cron.sh deploy/install_layer3_cron.sh deploy/install_daily_digest_cron.sh; do
    if grep -qE 'set -a.*\. /etc/paper-live/env' "$f"; then
        pass "$(basename "$f"): sources env with set -a"
    else
        fail "$(basename "$f"): sources env WITHOUT set -a — cron child won't inherit plain env"
    fi
done

# 3. functional: plain env + set -a actually exports to a child process.
tmp=$(mktemp)
printf 'X_CONTRACT_TOK=abc\n' > "$tmp"
got=$(set -a; . "$tmp"; set +a; bash -c 'printf "%s" "${X_CONTRACT_TOK:-}"')
rm -f "$tmp"
if [[ "$got" == "abc" ]]; then
    pass "set -a exports plain-env vars to a child process"
else
    fail "set -a chain did not export to child (got '$got')"
fi

echo ""
if [[ "$FAIL" -eq 0 ]]; then
    echo "PASS — env-format contract holds"
    exit 0
else
    echo "FAIL — $FAIL contract violation(s)"
    exit 1
fi
