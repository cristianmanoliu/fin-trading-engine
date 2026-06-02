# Audit lens: missing-input → silent-success

A reusable methodology for finding <abbr title="A bug where missing/empty/corrupt input silently maps to the success branch. Operator sees green when reality is 'no monitoring.'">fail-open</abbr> bugs in observability, decision-grade
evaluators, and operator-action paths. Distilled from 6 sessions (2026-05-05 →
2026-05-10) across which 57 bugs of this exact shape were found and closed.

For pattern-locked findings, see the catalog of audit-pass commits referenced
at the bottom. For doctrine on WHY this discipline exists, see the charter in
`results/INDEX.md`.

> 📖 **New to the jargon?** Hover over <abbr title="Hover over me to see this glossary tooltip.">underlined terms</abbr> for a one-line explanation, or read `docs/GLOSSARY.md` for the full plain-English reference.

---

## The pattern

> **Missing, empty, stale, or corrupt input silently maps to the wrong verdict
> — usually the success branch.**

The codebase has many tools that read external state (journal files, drift
history, `systemctl ExecStart`, SSH-fetched data, helper subprocesses) and
emit a <abbr title="The mechanical answer when a locked decision rule is applied to data. Verdicts in this project map to ADOPT/REJECT/HOLD/CONTRADICTION or CLEAN/INVESTIGATION/AUTO-KILL.">verdict</abbr>. Default values for absent inputs (`""`, `0`, empty list,
`|| true`) collapse into the success branch — operator sees green when reality
is "no monitoring."

Why it's the most fertile bug class in this codebase:

1. **Defaults are sticky.** `${VAR:-0}`, `dict.get(key, 0)`, `(.field // 0)`
   in jq, `awk`'s implicit-zero coercion — every language has them. Each is
   a fail-open hatch unless the absent case is treated distinctly.
2. **Verdict tools fan out.** A single tool's bad-input fallthrough doesn't
   bug just one user — every consumer downstream of the verdict (cron,
   Telegram tier mapping, kill mechanism) inherits the wrong answer.
3. **Test moats grow LATE.** Verification tools tend to ship with happy-path
   tests. Bad-input shapes (empty, malformed, partial, stale) get tested only
   after the first incident — and even then often only the specific shape
   that incident manifested.

---

## Three lens modes

The lens has three operational modes. All three were demonstrated within the
2026-05-10 session alone.

### Mode 1: lens-as-audit (retroactive)

Walk an existing tool and enumerate its input-failure shapes. For each shape,
ask: does it map to PASS / CONTINUE / DEPLOY-READY / CLEAN? If yes, that's a
fail-open.

**Yield:** very high on observability tooling and decision-grade evaluators
that emit verdicts (30+ findings closed in this mode across 2026-05-08–10).
Diminishing on small focused utility packages with good test coverage —
`pkg/aggregator` and `pkg/execution/tee.go` both audited clean on first pass.

### Mode 2: lens-as-design-tool (prospective)

Apply the lens BEFORE merging a new tool. Enumerate input-failure shapes at
design time; build distinct exit codes / tiers / sentinels into the first
commit instead of retrofitting them after an incident.

**Demonstrated dividend:** `scripts/layer3_verdict.sh` and
`scripts/forward_paper_resolution.py` shipped with input-shape guards designed
in (FATAL_NOT_A_DIR sentinels, distinct exit codes per failure shape,
mechanical tier mapping). Zero retroactive fail-opens needed across both.
21 regression tests passed first try on `forward_paper_resolution.py`.

### Mode 3: lens-as-self-correction (recursive)

The lens works on YOUR OWN code, including code committed minutes ago. When
documenting a path, claim, or verdict, **read the actual writer to verify
rather than assuming from context.**

Demonstrated on 2026-05-10 (morning) and again on 2026-05-10/11 (T2a build):
- During smoke-testing of `forward_paper_resolution.py`, the script fired
  spurious KILL on n=1 production data because Rule 1 had a redundant inline
  `single_sym>=50` check (one trade = 100% concentration always trips).
  Self-audit caught it BEFORE the second commit.
- During cron firing verification, the just-committed handbook commit
  `138a63b` claimed `<date>-resolution.txt` lived in `forward_paper_snapshots/`
  when the actual writer puts it in `decision_snapshots/`. Caught when the
  documented path produced "no such file"; fixed in `d4eaf84` 12 minutes
  later.
- During `stage_promotion.sh` local smoke, phase1 happily created a real
  in-progress artifact in production `results/` — the locked decision-rule
  doc `forward_paper_completion_review_decision_rule_2026-05-08.md` itself
  contains the literal `Composite verdict: ALL_GREEN` inside its template
  scaffold. The grep check matched the rule's DESCRIPTION of an ALL_GREEN
  verdict rather than an actual verdict. Fixed by excluding
  `*_decision_rule_*`, `*_template_*`, `*_verdict_*` from gate-doc candidates;
  regression test pins the fix (`test_phase1_rule_doc_doesnt_false_positive`).
- During `stage_promotion.sh` test debugging, `/bin/ls "$pattern"` with the
  glob INSIDE a quoted variable expansion silently fails — `/bin/ls` doesn't
  glob and the shell can't expand a glob that lives inside a quoted
  expansion. Standardized to `find` across all multi-file lookups.
- During same debugging, `grep "..." *.jsonl | python3` with `set -euo
  pipefail` crashes the script when grep returns zero matches (legitimate
  state for "no closes yet"). Rewrote to pure python with `pathlib.glob`,
  which returns empty list cleanly on no-match.

Pattern principle: **knowing the design is not the same as knowing the
implementation.**

---

## Adjacent pattern: writer-equals-model (the gate-informationality lens)

A distinct pathology from the main "missing-input → silent-success" pattern,
discovered on 2026-05-10 during T1b cost-trajectory analysis:

> **When the verdict-value WRITER is the same as the model the verdict is
> meant to CHALLENGE, the gate is informationally null. PASS is guaranteed
> by construction regardless of reality.**

Different from missing-input → silent-success: the values ARE present, ARE
parseable, ARE within threshold — but the writer's behavior makes "within
threshold" mechanical rather than empirical.

### Discovered instance (2026-05-10, T1b)

Forward-paper Day 5, n=15 closed trades across live + 3 shadow cohorts.
`scripts/realized_cost_trajectory.py` reports:

    fee   = 10.00 bp  / modeled 10.0bp / kill 12.0bp  [PASS]
    slip  = 5.00 bp   / modeled 5.0bp  / kill 25.0bp  [PASS]   (n_losers=15)

Both gates PASS. But every single trade reports fee=10.00 / slip=5.00 with
zero variance. Root cause: the Stub executor applies flat-rate `FeeBps` and
`StopSlippageBps` deterministically per trade — paper-mode realized cost IS
the modeled cost, not an empirical measurement. The CLAUDE.md PROMOTE
criterion "Realized round-trip taker fees ≤ 12 bp" cannot fire during the
entire 127-day forward-paper window because its writer guarantees compliance.

### Why this lens is distinct

The missing-input lens asks: "if the input is absent/empty/corrupt, where
does the verdict default to?"

The writer-equals-model lens asks: "what produces the verdict's values, and
is its behavior independent of what the verdict is testing?"

A gate can be CLEAN under the missing-input lens (no fail-open paths) and
still be informationally null under the writer-equals-model lens (the writer
guarantees PASS).

### How to apply

For each verdict gate in the codebase:

1. **Identify the writer.** Trace each value the gate reads back to its
   producer. Is it (a) a deterministic model output, (b) an empirical
   observation, or (c) a derived metric (some of both)?

2. **Compare to the threshold.** If the threshold tests a property that the
   writer guarantees, the gate is informationally null in that phase.
   Example: testing "realized fee ≤ 12 bp" when the writer's only behavior
   is to write exactly 10 bp.

3. **Identify the activation point.** When does the writer change such that
   the gate becomes informational? For trade-engine, paper→Layer-2-testnet
   flips the cost-stack writer from Stub (model) to BinanceLive (real
   exchange). At STAGE_1 the same writer is on real money. Each transition
   is an activation point for the gate.

4. **Document the plumbing-vs-signal distinction.** During the null phase,
   the gate IS still useful as a plumbing test — verifies the schema
   populates, the reader parses, the threshold evaluates. Discovering a
   broken cost-decomp path at STAGE_1 day would be much worse than
   discovering it during paper. But "PASS" during paper does NOT mean "the
   strategy's cost stack holds in production" — it means "the gate would
   not have surfaced a divergence even if one existed."

### Audit results across current gates

Quick survey of the locked PROMOTE criteria for this pathology:

| Gate | Writer during paper | Independent of model? | Information-bearing? |
|---|---|:---:|:---:|
| WR % live vs backtest | Real Binance prices → real signals → real outcomes | YES (writer is market, model is strategy) | YES |
| Realized fee bps | Stub `FeeBps` flat-rate | NO | NO (during paper) |
| Realized slip bps | Stub `StopSlippageBps` flat-rate | NO | NO (during paper) |
| Per-symbol concentration | Real signal-attribution by symbol | YES | YES |
| Trade count threshold | Pure count of real signals | YES | YES |
| Calendar days threshold | Pure elapsed time | YES | YES |
| BTC-HODL benchmark | Real BTC prices vs strategy NET | YES | YES |
| Drift detector (Welch t-test) | Real outcome distribution vs backtest distribution | YES (samples differ by signal-firing-time even with shared cost model) | YES |

Only the cost-stack gates carry the pathology in the current criterion set.
After Layer 2 testnet activation, the cost-stack writer flips from Stub to
real Binance and the gate becomes information-bearing.

### When this lens fires

The yield is sparse compared to the main missing-input lens — most gates in
this codebase are correctly designed with independent writers. But when it
DOES fire, the cost is high: an entire phase of "monitoring" might produce
no signal, masquerading as steady-state PASS.

Audit-time: enumerate writers for any new criterion set BEFORE locking the
thresholds. If writer ≡ model, the threshold IS the model and the gate is
a self-affirming tautology.

### Cross-reference

- `CLAUDE.md ## Forward-paper go/no-go criteria` — paper-mode caveat on
  the cost-stack gates (added 2026-05-10 with explicit activation-point
  language)
- `gate_informationality_2026-05-10.md` (project memory) — detailed instance
  + the 5 criterion-families audited for the same shape
- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` —
  defines the Layer 2/3 activation gates that flip the writer

---

## How to apply

For each external-state read in the tool under audit:

1. **Enumerate the shapes of input failure.** At minimum:
   - missing file
   - empty file
   - malformed lines (some / all)
   - network failure
   - all-skipped-malformed (parse layer succeeds but yields nothing)
   - stale-file-present-but-old (freshness, not just existence)
   - partial data (open events with no closes; closes pre-schema; etc.)

2. **For each shape, trace the verdict.** What value does the tool emit?
   Does it map to PASS / CONTINUE / CLEAN / DEPLOY-READY / 0?

3. **If yes — that's a fail-open.** Fix lives in three usual places:
   - **Distinct <abbr title="A tool's commitment to map specific failure shapes to specific exit codes. 0=success, 1=data shortage, 2=trigger missing, 3=misconfig, etc. Lets cron/wrapper/Telegram tier discriminate.">exit codes</abbr> / tiers.** Promote bad-input to a separate exit
     code (Telegram-routable separately from "data shortage").
   - **Freshness gates.** Most-recent-event < N days, not just "no fires
     recorded." Absence is not the same as silence.
   - **Counter discipline.** Track `total_parseable` separately from
     `events_of_interest`. Both at zero must surface differently than zero
     events found in N parseable lines.

4. **Add a regression test that DELIBERATELY constructs the bad-input
   shape.** Without the test, the next refactor reopens the door.

---

## <abbr title="When verdict tools feed Telegram tier mapping, bad-input crashes can route to the wrong tier. A Python TypeError on a corrupt journal entry exits 1 = CRITICAL kill page when it should be a parse-error WARN.">Telegram-tier dual sense</abbr>

When the verdict feeds a Telegram tier system (e.g., `weekly_audit.sh`
mapping kill-script exit 1 → CRITICAL "STOP THE PROTOCOL"), bad-input
*crashes* silently route into the wrong tier. A `datetime.strptime`
ValueError on a corrupt journal entry exits Python with code 1 = CRITICAL
kill page.

Audit also for:
- Which exit codes does the cron map to which Telegram tier?
- Could any uncaught exception path land in the wrong code?
- Does a transient API blip fire CRITICAL when it should fire WARN?

Specific examples closed:
- `f87042e` — corrupt-ts crash → exit 1 → false CRITICAL kill page;
  DEFERRED → exit 0 → false CRITICAL "PROMOTION READY" page on transient
  Binance API blips
- `ed1f360` — Python TypeError on null `fee_usd` routed exit 1 → wrong
  cron tier; fixed via `_num()` coercion helper

---

## Operator-action paths require especially aggressive lens

Silent fallthrough at the moment of operator action is the worst-case
scenario. The operator just fired the emergency button (kill-switch, drift
override, manual stage rollback) and is reading output to confirm — a
quietly-successful path masking partial-failed reality at that moment is
catastrophic.

`6dc3836` (cmd/kill_switch PARTIAL fills) is the canonical example:
post-panic-kill, the operator could miss residual real-money exposure on
Binance because the green "All positions closed cleanly" line fired despite
PARTIAL residuals in the per-symbol output above. Fix: distinct exit codes
per outcome class (CLOSED → 0, FAILED → 1, PARTIAL → 2) so the cron / Tier
system / wrapper can react differently.

Apply the lens **especially hard** on:
- `cmd/kill_switch`
- Stage promotion CLIs
- Manual drift override paths
- Anything triggered by the operator under stress

---

## Pattern observations

### Always check the sibling

Same-shape bugs co-exist in paired implementations. Audit one, check the
other immediately.

The journal-write-failure shape was confirmed three times in 2026-05-10:
- BinanceLive (`028e6a2`) — real-money executor
- Stub (`4154374`) — paper executor (sibling of BinanceLive)
- SignalContextWriter (`f760327`) — strategy-layer signal recorder

Each one had:
- `slog.Warn` + drop on append failure (instead of elevation)
- `s.journalFile` left non-nil after Write error → next call retries the
  dead handle and fails identically
- Operator sees recurring "MAY BE INVISIBLE" stream while engine keeps
  trading without journaling

By the third instance, the lens is genuinely **predictive**: you can name
the bug shape AND its location before opening the file.

### Second-pass yield is non-zero

Re-auditing a recently-audited file surfaces real bugs at high yield-per-time.

- `pkg/marketdata/binance.go`: first pass closed 2 (`3eb5ebc`); second pass
  closed 1 more (`5b41a07`).
- `cmd/engine`: first pass closed 6 (`c142e6e`); second pass closed 1 HIGH
  + 2 MED (`bb92810` + `fe4bf21`).

The first-pass auditor's mental model converges on certain code paths and
misses others. A second pass with a fresh frame finds what the first missed.

### Yield diminishes on small focused utility packages

Pure-logic helpers with good test coverage saturate near zero findings:
`pkg/execution/tee.go` and `pkg/aggregator` both clean on first pass. The
high-yield targets are observability tooling (verdicts), real-money
executors, orchestration with many startup paths, and external-state-reading
I/O.

---

## Prevention layer

Static analysis catches about half this class at lint time:
- **shellcheck** (`SC2086`, `SC2155`, `SC2087`) for shell scripts
- **ruff** (`F841`, `F401`, several others) for Python

Both added to `.github/workflows/test.yml`'s `lint` job on 2026-05-09
(commit `d4bba05`). Future occurrences fail CI before they merge.

The lens is needed for the OTHER half — the cases where syntax is correct
but the verdict mapping is wrong.

---

## Representative bug catalog

Selected commits to study for examples of each lens mode in action.

### Lens-as-audit (retroactive)

- `5b54e22` — 3 fail-opens in `stage_promotion_check` + `kill_protocol_check`
  (drift-clean staleness, empty history, recent-window-no-losers)
- `c88bc0e` — 6 fail-opens in 4 peer scripts (`live_vs_backtest_drift`,
  `forward_paper_status`, `btc_hodl_benchmark`, `post_deploy_check`) + drift
  heartbeat addition
- `098bc3d` — 4 fail-opens in Go safety-critical paths
- `f87042e` — 3 fail-opens with the Telegram-tier dual sense
- `2de1fe9` — 4 fail-opens in `forward_paper_status.sh`: JOURNAL_DIR override
  silently ignored on remote, typo'd path silently rendered as "(no data
  yet)", malformed helper output bypassed warning guard, top_syms sorted by
  signed PnL hiding biggest negative contributors
- `b3f5e1f` — Gate B daily-loss circuit breaker silently reset to $0 on
  engine restart (HIGH, real-money)
- `028e6a2` / `4154374` / `f760327` — journal-write-failure shape
  triple-paired (BinanceLive + Stub + SignalContextWriter)
- `c142e6e` — 6 startup `os.Exit(1)` sites silent on Telegram in `cmd/engine`
- `3eb5ebc` / `5b41a07` — `pkg/marketdata` first + second pass
- `bb92810` + `fe4bf21` — `cmd/engine` second-pass (1 HIGH funding-load
  silence + 2 MED config-fallback silences)
- `6dc3836` — `cmd/kill_switch` PARTIAL exit-code fix (HIGH, real-money
  operator path)
- `2cb6c12` — `btc_hodl_benchmark.py` 2 MED + first test scaffold
- `fb3feaa` — `cmd/dashboard` `/healthz` returned literal "OK" on stale/empty
  cache AND on a live drift exit-4 AUTO-KILL (first audit of this file;
  observability tooling). Fixed via `healthReport` → 503/DEGRADED + greppable
  `reason=` tokens (STALE/NO_DATA/DRIFT_KILL/DRIFT_MISSING/DRIFT_DEAD); +
  stale-banner extended from index-only to `/cohorts`+`/status`. Same
  distinct-status remedy as `6dc3836`.

### Lens-as-design-tool (prospective)

- `bd30f8c` — `layer3_verdict.sh` shipped lens-applied; missing/empty journal
  dirs exit 3 with explicit operator-readable diagnostics; pre-empts the
  "zero trades on either side = no violations = false PASS" failure mode
- `bd411bf` — `forward_paper_resolution.py` ships LIMBO rule mechanically
  wired; 21 regression tests pass first try

### Lens-as-self-correction (recursive)

- `bd411bf` smoke catch — Rule 1 redundant `single_sym>=50` fires KILL on
  n=1; caught BEFORE second commit
- `d4eaf84` handbook path bug — claimed `<date>-resolution.txt` lived in
  `forward_paper_snapshots/`; actual writer puts it in `decision_snapshots/`;
  caught when the documented path produced "no such file"

### Trajectory tooling sweep (2026-05-10, 10 commits)

- `e86c233` — `forward_paper_trajectory` silently dropped cohorts with no
  closes; bb20 invisible across all snapshots
- `ccf62fc` — same script exited 0 when zero cohorts parsed (upstream
  format-change → empty trajectory + green exit)
- `5cdae0b` — `realized_cost_trajectory` slip [PASS] when `n_losers=0` (gate
  unevaluated read as satisfied via `0.0 ≤ 25.0`); same shape as kill-bar
  mis-calibration
- `032bebd` — collapsed "all closes pre-decomp" into "no closes yet"
- `ea1fc67` / `a4cafa1` — bad `--cohort` exited 0 because main checked
  unfiltered list
- `4ff1526` — empty journal dir collapsed into exit 2 (legit "no closes
  yet")
- `ae3b712` — PnL regex truncated decimals
- `ed1f360` — Python TypeError on null `fee_usd`/`slip_usd` (Telegram-tier
  dual sense)
- `8b4e45f` — `subprocess.run(cmd, shell=True)` with f-string interpolation
  injection vector

---

## When NOT to apply

Historical analysis scripts (frozen research artifacts in `scripts/`) where
the cost-benefit doesn't justify modifying locked work. Lint at error
severity only for those; reserve warning severity gates for safety-critical
operational scripts.

The lens is for code that emits verdicts the project trusts. For frozen
research artifacts, the verdict was the analysis output (already written
into a `_verdict_*.md`); the script itself is just a reproduction harness.

---

## Cross-references

- `results/INDEX.md` — pre-registration discipline charter (the WHY)
- `docs/OPERATOR_HANDBOOK.md` — operational cadence (where audited tools
  surface to the operator)
- `.github/workflows/test.yml` — lint job (the half this lens doesn't cover)
- `CLAUDE.md ## Recent session logs` — chronological narrative of the audit
  passes
