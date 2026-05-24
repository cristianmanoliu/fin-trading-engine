# Binomial monitor — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE writing any code.
**Scope:** `scripts/binomial_monitor.py` + `scripts/test_binomial_monitor.py`

---

## What is being built

A standalone interpretive utility: given observed wins + n + null hypothesis WR, compute:
1. One-tailed binomial p-value: P(wins ≤ observed | n, null_p)
2. Critical-n curve: smallest n at which current WR becomes statistically suspicious at
   specified α (default 0.05 and 0.01)
3. Bayes factor: P(data | H0=null_p) / P(data | H1=backtest_p)

CLI: `python3 scripts/binomial_monitor.py --wins W --trades N --null-p P [--backtest-p P2] [--alpha A]`

---

## What this tool does NOT do

- Does NOT trigger any operator action (no kill, no promote, no alert)
- MUST NOT be invoked from `scripts/weekly_audit.sh` or any decision-grade pipeline
- MUST NOT replace `forward_paper_resolution.py:62` N_TRADES_FLOOR=50 constant
  (that constant is a locked pre-registered threshold; this tool is interpretive context
  that may inform future reviews of that constant, not override it)
- MUST NOT emit Telegram notifications

---

## Interpretation contract (locked)

| p-value range | Interpretation | Operator action |
|---------------|---------------|-----------------|
| ≥ 0.05 | Not suspicious | None — continue monitoring |
| 0.01–0.05 | Mildly suspicious but below floor | Note; do not act before n=50 |
| < 0.01 | Suspicious | Note; do not act before n=50; drift detector governs decisions |

**The drift detector (`scripts/live_vs_backtest_drift.py` exit code) governs all
decision-grade kill/promote triggers. This tool produces interpretive context only.**

Applying the tool to LIVE n=27 is reportable but NOT actionable per
`results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md` Rule 3
(n < 50 → CONTINUE regardless of observed WR).

---

## Verification contract (pinned before code is written)

These must pass for tool to be trusted:

```
# Sanity 1: null hypothesis — WR at breakeven, n=27, observe 4 wins
# P(wins ≤ 4 | n=27, p=0.143) must be > 0.30 (breakeven is the null)
python3 scripts/binomial_monitor.py --wins 4 --trades 27 --null-p 0.143

# Sanity 2: current LIVE situation
# P(wins ≤ 2 | n=27, p=0.206) should be ~0.03 (suspicious vs backtest WR but below floor)
python3 scripts/binomial_monitor.py --wins 2 --trades 27 --null-p 0.206

# Sanity 3: known n where n=100 and WR=7% under p=0.20 backtest expectation is clearly suspicious
# P(wins ≤ 7 | n=100, p=0.20) should be < 0.001
python3 scripts/binomial_monitor.py --wins 7 --trades 100 --null-p 0.20

# Sanity 4: error handling — wins > trades must exit non-zero
python3 scripts/binomial_monitor.py --wins 50 --trades 10 --null-p 0.20; test $? -ne 0
```

---

## Audit-lens compliance (per docs/AUDIT_LENS.md)

Applied before code is written:

1. **Missing-input → silent-success**: negative n, wins > n, p outside [0,1], missing
   required args → exit non-zero with explicit error message. No silent defaults that
   compute "OK" from invalid inputs.
2. **Writer-equals-model**: tool takes its own inputs from operator CLI — no self-referential
   model gates.
3. **Sibling-bug propagation**: `binom_pmf` / `binom_cdf` implementations verified against
   `math.comb` reference (`math.comb(n,k) * p**k * (1-p)**(n-k)`) in tests.
4. **Telegram-tier dual-sense**: no Telegram. Text output only.
5. **Operator-action-path**: interpretation contract above makes non-actionability explicit.

---

## Files this rule governs

- `scripts/binomial_monitor.py` — the tool
- `scripts/test_binomial_monitor.py` — pinned numerical tests
