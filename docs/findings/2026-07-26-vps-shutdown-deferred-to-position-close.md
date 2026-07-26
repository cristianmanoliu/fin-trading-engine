# VPS shutdown plan — DEFERRED to 2026-08-14 (7 positions still open)

**Date:** 2026-07-26
**Decision: do NOT stop the engines today.** Operator asked to proceed with the
VPS shutdown; the locked rule forbids it while positions are open. Deferring is
the rule-compliant action, not an omission.

## Why not today

`results/real_money_protocol_decision_rule_2026-05-08.md` §"When kill fires":

> - **Immediately stop opening new positions**
> - **Allow existing positions to close** at their stops/targets — do NOT
>   panic-close (preserves trade-level economics; mass closure crystallizes
>   arbitrary losses)
> - Mark the milestone KILLED
> - No automatic resumption within this milestone

Measured on the VPS today (`178.105.24.230`, up 83 days, 16 engines running):

```
ALL-TIME across every journal: opens=124 closes=117 -> 7 still open
```

Those 7 opened between **2026-07-17 and 2026-07-24**. With the live config's
`--max-hold-hours 504` (21 days), each force-closes at open+21d even if it never
touches a stop or target:

| opened | force-closes by |
|---|---|
| 2026-07-17 | 2026-08-07 |
| 2026-07-18 | 2026-08-08 |
| 2026-07-19 | 2026-08-09 |
| 2026-07-22 | 2026-08-12 |
| 2026-07-23 | 2026-08-13 |
| 2026-07-24 | **2026-08-14** |

**All positions are closed by 2026-08-14 without any intervention.**

## Why this is not just rule-lawyering

Those 7 closes are the final data points of the forward-paper cohort (n=116
closed today → 123 at completion). Abandoning them:

- leaves the KILL verdict resting on a truncated sample
- discards the trade-level economics the rule explicitly protects — a
  mass-closure marks 7 positions at an arbitrary instant rather than at their
  own stops/targets
- removes the "what did we learn from the kill" input that any next-milestone
  pre-registration would need (the rule requires a revised protocol
  "reflecting whatever we learned from the kill")

The cost of waiting is one Hetzner CX23 for ~19 days. The cost of not waiting is
an incomplete terminal dataset for a strategy that ran 78 days.

## First instruction is also not cleanly actionable

The rule's first bullet is "stop opening new positions". There is **no
documented flag** to disable signal generation while leaving position management
running (`grep` over CLAUDE.md and OPERATOR_HANDBOOK.md found none). Options
were:

1. Stop the engines → violates bullet 2. Rejected.
2. Add a `--no-new-positions` flag → writing and deploying NEW code to a KILLED
   system, to save ~19 days of paper trades that cost nothing. Rejected as
   disproportionate and risky.
3. Accept that up to a few more paper positions may open before 08-14. **Chosen.**

Option 3's cost is bounded and paper-only: at the observed ~1.18 trades/day
across 16 symbols, a handful more opens may occur, each also capped at 504h. They
do not change the verdict (already decided on 116 trades and two independent kill
criteria) and they extend the terminal dataset rather than corrupting it.

## The shutdown, when it is due (on or after 2026-08-14)

**1. Verify every position is closed** — do not skip this:
```bash
ssh root@178.105.24.230 '
total_o=0; total_c=0
for f in /var/log/paper-live/journal/*.jsonl; do
  o=$(grep -o "\"event\":\"open\"" "$f" | wc -l)
  c=$(grep -o "\"event\":\"close\"" "$f" | wc -l)
  total_o=$((total_o+o)); total_c=$((total_c+c))
done
echo "opens=$total_o closes=$total_c open=$((total_o-total_c))"'
# proceed ONLY when open=0
```

**2. Pull the final journals before stopping anything** (the VPS is the only copy
of the terminal data):
```bash
cd ~/Main/code/active/fin-trading-engine && bash scripts/journal_fetch.sh
git add results/journal_cache/ && git commit -m "results: final forward-paper journals at KILL"
```

**3. Stop and disable:**
```bash
ssh root@178.105.24.230 'systemctl stop "paper-live@*.service"'
ssh root@178.105.24.230 'systemctl disable "paper-live@*.service"'
ssh root@178.105.24.230 'systemctl list-units "paper-live@*" --all --no-pager | tail -5'
```

**4. Unload the local drift cron** — it fires weekly against a dead deploy and
will keep emitting exit 4:
```bash
launchctl unload ~/Library/LaunchAgents/com.tradingengine.drift-check.plist
```

**5. Decide on the Hetzner box.** Keeping it costs ~€5/mo and preserves the
environment if a next milestone is ever pre-registered; destroying it is
final. Snapshot before destroying.

**Do NOT** run `cmd/kill_switch` — that is the real-money Path C close-all, and
real-money allocation was always ZERO. There is nothing for it to close.

## Note

`gate-readiness.sh`-style verification of position count belongs in a script if
this ever recurs, but with one KILL in the project's life it is not worth
building. The commands above are copy-pasteable.
