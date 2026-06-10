#!/usr/bin/env python3
"""oi_divergence_study.py — candidate #1: open-interest / price divergence.

Pure CSV study (no engine). Reads data/metrics/<SYM>.csv (sum_open_interest) +
local 1m klines. Per `results/strategy_candidates_2026-06-10.md` (#1) + Phase 2.

THE BET
-------
Δprice × ΔOI sign combinations carry directional information the price series alone
does not:
  price UP   + OI UP    = new longs opening      -> continuation (LONG bias)
  price UP   + OI DOWN   = shorts covering        -> weak rally / fade (SHORT bias)
  price DOWN + OI UP    = new shorts opening      -> continuation down (SHORT bias)
  price DOWN + OI DOWN   = longs liquidating/exit -> capitulation / fade (LONG bias)
Measured on a 4H lookback (matching the live signal TF), forward return over H.

================================  PRE-REGISTRATION  ============================
  Bar:               4H (resample metrics + price to 4H grid).
  Signal:            sign(Δprice_4H) × sign(ΔOI_4H) over the prior 4H. The 4 combos
                     above map to a LONG/SHORT bias each (continuation logic).
  Forward horizon H: 4h, 12h, 24h.
  Cost:              15 bp round-trip; screen pass = 2x = 30 bp gross move.
  Honesty:           per-combo mean/median/win + by-year + drop-top-5%.
  ACCEPT:            a combo with gross>=30bp, median net>0, survives top-5%,
                     positive majority of years -> corr-to-LIVE gate.
===============================================================================
"""
import os, glob, csv, math, sys, argparse, datetime
from collections import defaultdict
import numpy as np

METRICS_DIR = "data/metrics"
DATA = "data"
MIN_MS = 60_000
BAR_MS = 4 * 3600_000          # 4H
HORIZONS_H = [4, 12, 24]
COST_BP = 15.0
PASS_BP = 30.0


def load_oi_4h(sym):
    """4H-bar end-of-bar OI: bar_ms -> last sum_open_interest in that bar."""
    path = os.path.join(METRICS_DIR, f"{sym}.csv")
    out = {}
    if not os.path.exists(path):
        return out
    with open(path) as f:
        r = csv.DictReader(f)
        for row in r:
            try:
                t = datetime.datetime.strptime(row["create_time"], "%Y-%m-%d %H:%M:%S")
                ts = int(t.replace(tzinfo=datetime.timezone.utc).timestamp() * 1000)
                oi = float(row["sum_open_interest"])
            except (ValueError, KeyError):
                continue
            out[(ts // BAR_MS) * BAR_MS] = oi   # last write = bar-end
    return out


def load_close_4h(sym):
    out = {}
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    ot = int(row[0]); c = float(row[4])
                except (ValueError, IndexError):
                    continue
                out[(ot // BAR_MS) * BAR_MS] = c
    return out


COMBO_BIAS = {        # (sign dprice, sign doi) -> +1 long / -1 short
    (1, 1): +1,       # price up + OI up: continuation long
    (1, -1): -1,      # price up + OI down: short-cover fade
    (-1, 1): -1,      # price down + OI up: continuation short
    (-1, -1): +1,     # price down + OI down: capitulation long
}


def analyze(sym, buckets):
    oi = load_oi_4h(sym)
    cl = load_close_4h(sym)
    if len(oi) < 100 or len(cl) < 100:
        return
    bars = sorted(set(oi) & set(cl))
    for i in range(1, len(bars)):
        b0, b1 = bars[i - 1], bars[i]
        if b1 - b0 != BAR_MS:        # require contiguous 4H bars
            continue
        dp = cl[b1] - cl[b0]
        do = oi[b1] - oi[b0]
        if dp == 0 or do == 0 or cl[b0] <= 0:
            continue
        combo = (1 if dp > 0 else -1, 1 if do > 0 else -1)
        bias = COMBO_BIAS[combo]
        for H in HORIZONS_H:
            bf = b1 + H * 3600_000
            cf = cl.get((bf // BAR_MS) * BAR_MS)
            if cf is None or cf <= 0:
                continue
            fwd = math.log(cf / cl[b1]) * 1e4
            net = bias * fwd - COST_BP
            yr = datetime.datetime.fromtimestamp(b1 / 1000, datetime.timezone.utc).year
            buckets[(combo, H)].append((net, yr, abs(fwd)))


def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--limit", type=int, default=0)
    args = ap.parse_args()
    syms = sorted(os.path.basename(p)[:-4] for p in glob.glob(os.path.join(METRICS_DIR, "*.csv")))
    if args.limit:
        syms = syms[: args.limit]
    if not syms:
        print("No metrics — run scripts/download_metrics.py first."); sys.exit(1)

    print("=" * 84)
    print(f"OI/PRICE DIVERGENCE (#1) — {len(syms)} symbols, 4H bars")
    print(f"cost {COST_BP}bp; screen {PASS_BP}bp; combos = sign(dPrice)xsign(dOI)")
    print("=" * 84)
    buckets = defaultdict(list)
    for i, s in enumerate(syms, 1):
        analyze(s, buckets)
        print(f"  [{i}/{len(syms)}] {s}", file=sys.stderr)

    names = {(1, 1): "P+OI+ contLong", (1, -1): "P+OI- coverFade",
             (-1, 1): "P-OI+ contShort", (-1, -1): "P-OI- capitLong"}
    print(f"{'combo':<16}{'H':>4}{'n':>8}{'gross':>8}{'mean_net':>9}{'med':>8}"
          f"{'win':>6}{'t':>6}{'drop5':>8}  screen")
    print("-" * 84)
    best = None
    for (combo, H) in sorted(buckets, key=lambda k: (k[0], k[1])):
        rows = buckets[(combo, H)]
        if len(rows) < 60:
            continue
        net = np.array([r[0] for r in rows]); yrs = np.array([r[1] for r in rows])
        gross = np.mean([r[2] for r in rows])
        srt = np.sort(net); k5 = max(1, int(len(srt) * 0.05))
        med = float(np.median(srt)); wr = float((srt > 0).mean())
        t = srt.mean() / (srt.std(ddof=1) / math.sqrt(len(srt)))
        drop5 = srt[:-k5].mean()
        screen = "PASS" if gross >= PASS_BP else ("cost" if gross >= COST_BP else "DEAD")
        print(f"{names[combo]:<16}{H:>4}{len(net):>8}{gross:>8.2f}{srt.mean():>9.2f}"
              f"{med:>8.2f}{wr:>6.0%}{t:>6.2f}{drop5:>8.2f}  {screen}")
        byyear = defaultdict(list)
        for v, y in zip(net, yrs):
            byyear[int(y)].append(v)
        score = srt.mean() if (gross >= PASS_BP and med > 0 and drop5 > 0) else -1e9
        if best is None or score > best[0]:
            best = (score, names[combo], H, dict(n=len(net), mean=srt.mean(), med=med,
                                                 wr=wr, gross=gross, drop5=drop5, byyear=byyear))
    print()
    print("=" * 84)
    print("VERDICT (candidate #1 OI divergence)")
    if not best or best[0] < -1e8:
        print("  NO-GO: no combo clears the 30bp screen with positive median + tail survival.")
        print("  OI-divergence direction carries no fee-clearing forward edge at 4H.")
    else:
        _, nm, H, st = best
        ry = py = 0
        for y in sorted(st["byyear"]):
            a = np.array(st["byyear"][y])
            if len(a) < 30: continue
            ry += 1; py += 1 if a.mean() > 0 else 0
        v = ("CANDIDATE -> corr-to-LIVE" if py >= (ry+1)//2 and st["wr"] > 0.5 else "MARGINAL")
        print(f"  {nm} H={H}h gross={st['gross']:.1f} mean={st['mean']:.2f} med={st['med']:.2f} "
              f"win={st['wr']:.0%} drop5={st['drop5']:.2f} yrs={py}/{ry} -> {v}")
    print("=" * 84)


if __name__ == "__main__":
    main()
