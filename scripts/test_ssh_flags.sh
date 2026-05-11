#!/usr/bin/env bash
# test_ssh_flags.sh — convention lint for ssh-hang-resistance flags.
#
# Operational scripts that invoke ssh MUST include both:
#   -o BatchMode=yes     → fail rather than prompt for password (cron-safe)
#   -o ConnectTimeout=N  → fail fast on dead/unreachable host (no 75s+ hang)
#
# Without these, a dead Hetzner connection can hang weekly_audit (cron) or
# post_deploy_check (operator after redeploy) for the OS TCP retransmit
# default. F2 closed the missing-flags case for weekly_audit's
# journal_validate ssh; F7 propagated the convention to all sibling
# operational scripts. This test pins the convention so a future ssh
# call added without the flags fails CI.
#
# Excluded: deploy/sync.sh / deploy/redeploy.sh use rsync wrappers (not
# raw ssh). Comment lines are skipped. String literals in alert text are
# skipped (operator-instruction text inside Telegram message bodies).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Files to lint. Every operational script in CI's strict-shellcheck list
# that calls ssh must comply. Adding a new file to this list is the
# operator's signal that they want the convention enforced there too.
OPERATIONAL_FILES=(
    "scripts/weekly_audit.sh"
    "scripts/forward_paper_status.sh"
    "scripts/post_deploy_check.sh"
    "scripts/run_drift_check.sh"
    "scripts/paper_live_trades.sh"
    "scripts/stage_promotion.sh"
    "scripts/lag_summary.sh"
)

PASS=0
FAIL=0
FAIL_DETAILS=()

# Walk each file, find every line that invokes ssh as a command (not
# inside a comment, not inside a string literal that just mentions the
# word). Each ssh-invoking line MUST contain BatchMode=yes AND
# ConnectTimeout= (any value — stage_promotion:433 uses 5s, others 10s).
for file in "${OPERATIONAL_FILES[@]}"; do
    full_path="${REPO_ROOT}/${file}"
    [[ -f "$full_path" ]] || { echo "  ✗ $file: missing" >&2; FAIL=$((FAIL + 1)); FAIL_DETAILS+=("$file: missing"); continue; }

    # Match lines where `ssh ` appears as an actual command:
    #   - not inside a single-line comment (line starts with whitespace + #)
    #   - not preceded by a non-shell-significant char that suggests it's
    #     in a string literal (quoted text). We use a heuristic: skip lines
    #     where `ssh ` is preceded by `:`, `'`, or appears after `"...:`.
    while IFS= read -r line; do
        # Skip pure-comment lines.
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        # Skip lines that don't contain `ssh ` followed by a non-flag char.
        [[ "$line" =~ (^|[^_[:alnum:]])ssh[[:space:]] ]] || continue
        # Skip false-positive shapes — known patterns where `ssh ` appears
        # in string-literal contexts (alert text, echo'd help, descriptive
        # messages) rather than as a real invocation. Each pattern below
        # corresponds to a real false-positive observed in this codebase;
        # extend the list as new shapes appear. A principled quote-parity
        # tracker would be more robust but adds complexity disproportionate
        # to the number of false-positive shapes.

        # 1) "Run: ssh ..." / "Try: ssh ..." — operator-instruction text inside
        #    Telegram alert bodies / printf strings.
        [[ "$line" =~ ^[[:space:]]*(Run|Execute|Try|See|To)[[:space:]]*:[[:space:]]+ssh ]] && continue

        # 2) "ssh exit=$VAR" — string-interpolation inside multi-line alert body.
        [[ "$line" =~ ^[[:space:]]*ssh[[:space:]]+(exit|failed|succeeded|to)[[:space:]] ]] && continue
        [[ "$line" =~ ^[[:space:]]*ssh[[:space:]]+(exit|failed|succeeded|to)= ]] && continue

        # 3) Indented "  ssh root@... <command>" inside a notify_telegram body.
        #    Real invocations have command-position context (preceded by `$(`,
        #    `if `, `||`, `&&`, `!`, `;`, `|`); string-literal lines have only
        #    leading whitespace.
        if [[ "$line" =~ ^[[:space:]]+ssh[[:space:]]+root@ ]]; then
            # Skip unless this line ALSO has command-position context on it.
            if [[ ! "$line" =~ (\$\(|^[[:space:]]*if[[:space:]]|^[[:space:]]*!|\|\||&&|\;) ]]; then
                continue
            fi
        fi

        # 4) `echo "...ssh ..."` / `printf "...ssh ..."` / `warn|ok|crit "...ssh ..."`
        #    — the ssh is inside the quoted format/message text, not invoked.
        #    Includes the post_deploy_check `warn "... (ssh exit=$SSH_EXIT) ..."`
        #    diagnostic shape introduced by the PD-1/3/4/5/7 audit fixes.
        [[ "$line" =~ (echo|printf|warn|ok|crit)[[:space:]]+\".*ssh[[:space:]] ]] && continue

        # 5) `VAR="...ssh ..."` — assignment with a string literal that
        #    happens to mention ssh (e.g. operator-friendly error message).
        [[ "$line" =~ =\"[^\"]*ssh[[:space:]] ]] && continue

        # Now check the convention.
        has_batchmode=0
        has_timeout=0
        [[ "$line" =~ BatchMode=yes ]] && has_batchmode=1
        [[ "$line" =~ ConnectTimeout= ]] && has_timeout=1

        if [[ "$has_batchmode" -eq 1 ]] && [[ "$has_timeout" -eq 1 ]]; then
            PASS=$((PASS + 1))
        else
            FAIL=$((FAIL + 1))
            FAIL_DETAILS+=("$file: missing flags on: $(echo "$line" | sed 's/^[[:space:]]*//' | cut -c1-100)")
        fi
    done < "$full_path"
done

echo "─── ssh-flag lint: $PASS compliant, $FAIL non-compliant ───"
if [[ $FAIL -gt 0 ]]; then
    echo "FAILED:"
    for detail in "${FAIL_DETAILS[@]}"; do
        echo "  ✗ $detail"
    done
    echo
    echo "Fix: prepend '-o BatchMode=yes -o ConnectTimeout=10' to each ssh invocation."
    echo "See F2 fix in scripts/weekly_audit.sh and F7 fix across operational scripts."
    exit 1
fi
echo "all ssh invocations comply"
