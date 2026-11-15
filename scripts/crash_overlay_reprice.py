"""Re-price crash-overlay journals ($500 stake run) with Kraken book costs.
Backtest already charges fee 10bp RT + 5bp slip on STOP exits; entries slip 0.
Scenario adds: entry_bp on every trade, (stop_bp - 5) on STOP exits. Stake scales linearly."""
import json, glob, sys, collections
W = sys.argv[1]
T = [json.loads(l) for f in glob.glob(f"{W}/j/*/*.jsonl") for l in open(f)]
T = [t for t in T if t["event"] == "close"]
def extra(t, entry_bp, stop_bp):
    bp = entry_bp + (stop_bp - 5 if (t["outcome"] == "STOP" or t.get("slip_usd", 0) > 0) else 0)
    return t["notional_usd"] * bp / 1e4
# self-check: one STOP trade at $10k notional, 10/10 -> 10+5 = 15bp = $15
assert abs(extra({"notional_usd": 1e4, "outcome": "STOP"}, 10, 10) - 15) < 1e-9
assert abs(extra({"notional_usd": 1e4, "outcome": "TARGET"}, 10, 10) - 10) < 1e-9
# (label, stake multiple of $500, entry bp, stop bp) from the 2026-11-15 book screen
S = [("$500 as backtest", 1, 0, 5), ("$250 real (8bp, $10k median)", .5, 8, 8), ("$500 real (9.5bp, $20k median)", 1, 9.5, 9.5),
     ("$500 real (7bp, weekday guess)", 1, 7, 7), ("$1k real (11bp, $39k median)", 2, 11, 11)]
yrs = sorted({t["ts"][:4] for t in T})
print(f"{'scenario':32}" + "".join(f"{y:>8}" for y in yrs) + f"{'total':>9}{'per yr':>8}{'worst2y':>9}")
for lab, k, e, s in S:
    by = collections.defaultdict(float)
    for t in T:
        by[t["ts"][:4]] += k * (t["pnl_usd"] - extra(t, e, s))
    tot = sum(by.values()); w2 = min(by[a] + by[b] for a, b in zip(yrs, yrs[1:]))
    print(f"{lab:32}" + "".join(f"{by[y]/1e3:8.0f}" for y in yrs) + f"{tot/1e3:9.0f}{tot/6.55/1e3:8.1f}{w2/1e3:9.0f}")
# break-even entry=stop bp at $500
for bp in range(0, 21):
    if sum(t["pnl_usd"] - extra(t, bp, max(bp, 5)) for t in T) < 0:
        print(f"break-even: between {bp-1} and {bp} bp (entry = stop slip)"); break
print(f"trades {len(T)}, STOP share {sum((t["outcome"]=="STOP" or t.get("slip_usd",0)>0) for t in T)/len(T):.0%}, median notional ${sorted(t['notional_usd'] for t in T)[len(T)//2]:,.0f}")
