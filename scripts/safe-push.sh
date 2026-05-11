#!/usr/bin/env bash
# safe-push.sh — Local-validation gauntlet then push, then watch CI to green.
#
# Today's session (2026-05-11) hit two CI failures from pushes that passed
# locally on macOS but failed on Ubuntu CI:
#   - BSD-vs-GNU stat order (commit cc7cda6 → fixed in 80eba2e)
#   - Ruff F401 unused-imports on new Python files (052a838 → 7c2434e)
#
# Both were locally-green but CI-fails. The `feedback_ci_green_before_ship`
# memory entry says: "after `git push`, work is NOT shipped until CI reports
# green." This wrapper makes that rule mechanical:
#
#   1. Run the full local-validation gauntlet that mirrors CI's checks:
#        - go test ./...                          (matches CI go-test job)
#        - python3 scripts/test_*.py              (matches CI python-test job)
#        - bash scripts/test_*.sh                 (matches CI bash-test job)
#        - ruff check --select F scripts/         (matches CI ruff job)
#        - shellcheck on scripts/*.sh if installed (matches CI shellcheck job)
#   2. If ALL pass → git push
#   3. After push → watch CI to completion via `gh run watch`
#   4. If CI green → success. If CI red → surface conclusion + run ID.
#
# Usage:
#   bash scripts/safe-push.sh                    # default branch (current)
#   bash scripts/safe-push.sh --skip-ci-watch   # push only, don't wait for CI
#                                                 (last-resort when CI is flaky)
#   bash scripts/safe-push.sh --dry-run          # gauntlet only, no push
#
# Exit codes:
#   0  All passed AND CI green (or --skip-ci-watch given)
#   1  Local gauntlet failed (something needs fixing before push)
#   2  Push succeeded but CI failed (something needs fixing now)
#   3  Setup error (missing gh CLI, not in repo, etc.)
#
# The wrapper is OPTIONAL — `git push` still works. But preferring this
# wrapper for every push closes the locally-green/CI-red gap that's bitten
# twice today.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT" || { echo "ERROR: cannot cd to repo root: $REPO_ROOT" >&2; exit 3; }

SKIP_CI_WATCH=0
DRY_RUN=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-ci-watch) SKIP_CI_WATCH=1; shift ;;
        --dry-run)       DRY_RUN=1; shift ;;
        -h|--help)
            sed -n '2,/^set/p' "$0" | sed 's/^# \?//' | head -n -1
            exit 0
            ;;
        *)
            echo "ERROR: unknown flag: $1" >&2
            echo "Run with --help for usage." >&2
            exit 3
            ;;
    esac
done

# Cosmetic helpers — paste-ready output for the operator (or me).
sep() { printf '─%.0s' $(seq 1 70); echo; }
section() { echo; sep; echo "  $1"; sep; }
ok()   { echo "  ✓ $1"; }
fail() { echo "  ✗ $1" >&2; }

# Track gauntlet failures across all steps; report all at the end rather
# than bailing on first failure (operator can fix multiple issues in one pass).
GAUNTLET_FAILED=0
FAILED_STEPS=()

run_step() {
    local name="$1"; shift
    local cmd=("$@")
    section "$name"
    if "${cmd[@]}"; then
        ok "$name passed"
    else
        fail "$name FAILED — fix before push"
        GAUNTLET_FAILED=1
        FAILED_STEPS+=("$name")
    fi
}

# ── Step 1: Go test ─────────────────────────────────────────────────────────
run_step "go test ./..." go test ./...

# ── Step 2: Python tests ───────────────────────────────────────────────────
# Run each test file in sequence so a failure shows the specific file.
section "Python test_*.py"
PY_FAIL=0
for f in scripts/test_*.py; do
    [[ -f "$f" ]] || continue
    if python3 "$f" > /tmp/safe-push-py.log 2>&1; then
        ok "$f"
    else
        fail "$f — see /tmp/safe-push-py.log"
        tail -20 /tmp/safe-push-py.log >&2
        PY_FAIL=1
    fi
done
if [[ "$PY_FAIL" -eq 0 ]]; then
    ok "Python test suite passed"
else
    GAUNTLET_FAILED=1
    FAILED_STEPS+=("Python test_*.py")
fi

# ── Step 3: Bash tests ─────────────────────────────────────────────────────
section "Bash test_*.sh"
BASH_FAIL=0
for f in scripts/test_*.sh; do
    [[ -f "$f" ]] || continue
    if bash "$f" > /tmp/safe-push-bash.log 2>&1; then
        ok "$f"
    else
        fail "$f — see /tmp/safe-push-bash.log"
        tail -20 /tmp/safe-push-bash.log >&2
        BASH_FAIL=1
    fi
done
if [[ "$BASH_FAIL" -eq 0 ]]; then
    ok "Bash test suite passed"
else
    GAUNTLET_FAILED=1
    FAILED_STEPS+=("Bash test_*.sh")
fi

# ── Step 4: Ruff (matches CI's `ruff check --select F scripts/`) ───────────
section "Ruff (pyflakes F-rules on scripts/)"
if command -v ruff > /dev/null 2>&1; then
    if ruff check --select F scripts/; then
        ok "ruff passed"
    else
        fail "ruff FAILED — try: ruff check --select F scripts/ --fix"
        GAUNTLET_FAILED=1
        FAILED_STEPS+=("ruff")
    fi
else
    echo "  (ruff not installed locally; skipping — CI will still enforce)"
fi

# ── Step 5: Shellcheck (matches CI's two-pass strategy) ────────────────────
# CI runs shellcheck in TWO passes (.github/workflows/test.yml:123-138):
#   - Operational (warning severity): forward_paper_status, post_deploy_check,
#     weekly_audit, run_drift_check, paper_live_*, lib/, deploy/ — safety-
#     critical, must be clean at warning severity
#   - Whole-repo (error severity only): scripts/*.sh deploy/*.sh — catches
#     real bugs in historical/analysis scripts without forcing refactor
# The wrapper mirrors both passes; matches CI exactly so locally-green ⇒
# CI-green for shellcheck specifically.
section "Shellcheck — operational (warning severity)"
if command -v shellcheck > /dev/null 2>&1; then
    OP_FAIL=0
    # shellcheck disable=SC2086  # intentional glob expansion
    if ! shellcheck -S warning \
        scripts/forward_paper_status.sh \
        scripts/post_deploy_check.sh \
        scripts/weekly_audit.sh \
        scripts/run_drift_check.sh \
        scripts/paper_live_*.sh \
        scripts/lib/*.sh \
        deploy/*.sh 2>&1 | tee /tmp/safe-push-shellcheck-op.log; then
        OP_FAIL=1
    fi
    # `tee` returns 0 even if shellcheck fails; capture true exit via PIPESTATUS.
    if [[ "${PIPESTATUS[0]}" -ne 0 ]] || [[ "$OP_FAIL" -eq 1 ]]; then
        fail "operational shellcheck FAILED"
        GAUNTLET_FAILED=1
        FAILED_STEPS+=("shellcheck-operational")
    else
        ok "operational shellcheck passed"
    fi

    section "Shellcheck — whole repo (error severity only)"
    if ! shellcheck -S error scripts/*.sh deploy/*.sh 2>&1 | tee /tmp/safe-push-shellcheck-all.log; then
        :  # tee returns 0 even on shellcheck error; check below
    fi
    if [[ "${PIPESTATUS[0]}" -ne 0 ]]; then
        fail "whole-repo shellcheck FAILED (error severity)"
        GAUNTLET_FAILED=1
        FAILED_STEPS+=("shellcheck-whole")
    else
        ok "whole-repo shellcheck passed"
    fi
else
    echo "  (shellcheck not installed locally; skipping — CI will still enforce)"
fi

# ── Gauntlet summary ───────────────────────────────────────────────────────
section "Gauntlet summary"
if [[ "$GAUNTLET_FAILED" -eq 1 ]]; then
    fail "Gauntlet FAILED on: ${FAILED_STEPS[*]}"
    echo
    echo "  Fix the failures above before pushing. The locally-green/"
    echo "  CI-red gap is exactly what this wrapper prevents — pushing"
    echo "  now would just defer the failure to CI."
    exit 1
fi
ok "All gauntlet steps passed"

if [[ "$DRY_RUN" -eq 1 ]]; then
    echo
    echo "  --dry-run: skipping push. Run without --dry-run to push + watch CI."
    exit 0
fi

# ── Step 6: Push ───────────────────────────────────────────────────────────
section "git push"
if ! git push; then
    fail "git push FAILED — see output above"
    exit 1
fi
ok "Pushed"

# ── Step 7: Watch CI to completion (the new workflow rule) ─────────────────
if [[ "$SKIP_CI_WATCH" -eq 1 ]]; then
    echo
    echo "  --skip-ci-watch: NOT waiting for CI. The 'work shipped' check"
    echo "  is incomplete — run 'gh run watch' manually before considering"
    echo "  the push verified."
    exit 0
fi

section "Watch CI to completion"
if ! command -v gh > /dev/null 2>&1; then
    echo "  ERROR: gh CLI not found — cannot watch CI. Install gh or use"
    echo "  --skip-ci-watch (with manual follow-up). Push succeeded but"
    echo "  CI status is unknown." >&2
    exit 3
fi

# Wait for the just-pushed commit's CI run to appear. Sometimes the run
# takes 5-10s to be queued after push.
HEAD_SHA=$(git rev-parse HEAD)
echo "  Waiting for CI run on $HEAD_SHA to appear..."
ATTEMPTS=0
RUN_ID=""
while [[ -z "$RUN_ID" ]] && [[ "$ATTEMPTS" -lt 12 ]]; do
    RUN_ID=$(gh run list --limit 5 --workflow=tests \
        --json databaseId,headSha \
        --jq ".[] | select(.headSha == \"$HEAD_SHA\") | .databaseId" \
        | head -1)
    [[ -z "$RUN_ID" ]] && { sleep 5; ATTEMPTS=$((ATTEMPTS + 1)); }
done

if [[ -z "$RUN_ID" ]]; then
    fail "Could not find CI run for $HEAD_SHA after 60s. Check manually."
    exit 2
fi

ok "Watching run $RUN_ID"
# gh run watch blocks until the run completes; --exit-status returns non-zero
# if the run failed. We capture output but suppress noisy progress lines.
if gh run watch "$RUN_ID" --exit-status > /dev/null 2>&1; then
    section "✓ CI GREEN"
    echo "  Run: https://github.com/$(gh repo view --json nameWithOwner --jq .nameWithOwner)/actions/runs/$RUN_ID"
    exit 0
else
    section "✗ CI FAILED"
    echo "  Run: https://github.com/$(gh repo view --json nameWithOwner --jq .nameWithOwner)/actions/runs/$RUN_ID"
    echo "  Inspect with: gh run view $RUN_ID --log-failed"
    echo "  Fix the failure and re-run safe-push.sh."
    exit 2
fi
