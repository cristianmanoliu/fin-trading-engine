#!/usr/bin/env python3
"""failed_pump_followup.py — #25 follow-ups F1-F5.

Pre-reg: results/failed_pump_followup_decision_rule_2026-06-10.md (locked bars).
Input: results/failed_pump_trades_2026-06-10.csv (primary cell P=0.25 unless noted)
+ 1m CSVs (F1) + results/live_journals (F4).

F1 intraday re-sim (1m-available subset, true stop/target ordering)
F2 slippage stress (100bp / 200bp RT + breakeven)
F3 2026 decay z
F4 monthly corr to live book
F5 listing-age decomposition (report-only)
"""
import glob
import json
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
COST = 0.0070
HOLD_MS = 7 * 86400_000


def tstat(x):
    x = np.asarray(x, dtype=float)
    if len(x) < 2 or x.std(ddof=1) == 0:
        return float("nan")
    return x.mean() / (x.std(ddof=1) / np.sqrt(len(x)))


def load_1m_ohlc(sym):
    parts = []
    for fp in sorted(glob.glob(os.path.join(ROOT, "data", f"{sym}-1m-*.csv"))):
        try:
            parts.append(pd.read_csv(fp, usecols=[0, 1, 2, 3, 4], header=0,
                                     names=["t", "o", "h", "l", "c"],
                                     skiprows=1))
        except Exception:
            continue
    if not parts:
        return None
    df = pd.concat(parts, ignore_index=True)
    for col in df.columns:
        df[col] = pd.to_numeric(df[col], errors="coerce")
    df = df.dropna().drop_duplicates("t").sort_values("t").reset_index(drop=True)
    return (df["t"].values.astype(np.int64), df["o"].values, df["h"].values,
            df["l"].values, df["c"].values)


def main():
    tr = pd.read_csv(os.path.join(ROOT, "results",
                                  "failed_pump_trades_2026-06-10.csv"))
    pri = tr[tr["P_cell"] == 0.25].copy()
    print(f"primary cell trades: {len(pri)}")

    # ---- F1: intraday re-sim on 1m-available subset -------------------------
    syms_1m = {os.path.basename(p).split("-1m-")[0]
               for p in glob.glob(os.path.join(ROOT, "data", "*-1m-*.csv"))}
    sub = pri[pri["sym"].isin(syms_1m)].copy()
    print(f"\nF1 subset (1m available): n={len(sub)} across "
          f"{sub['sym'].nunique()} syms")
    res_1m = []
    for sym, g in sub.groupby("sym"):
        data = load_1m_ohlc(sym)
        if data is None:
            continue
        t, o, h, l, c = data
        for _, row in g.iterrows():
            e_ms = int(row["entry_ts"])
            i0 = np.searchsorted(t, e_ms, side="left")
            if i0 >= len(t) or t[i0] - e_ms > 86400_000:
                continue  # no 1m coverage of entry day
            entry = o[i0]
            stop, target = row["stop"], row["target"]
            if entry <= 0 or stop <= entry:
                continue
            j_end = np.searchsorted(t, e_ms + HOLD_MS, side="right")
            hh, ll = h[i0:j_end], l[i0:j_end]
            s_hit = np.argmax(hh >= stop) if (hh >= stop).any() else -1
            t_hit = np.argmax(ll <= target) if (ll <= target).any() else -1
            if s_hit >= 0 and (t_hit < 0 or s_hit < t_hit):
                px, outc = stop, "STOP"
            elif t_hit >= 0:
                px, outc = target, "TARGET"
            elif s_hit >= 0 and s_hit == t_hit:  # same 1m bar: pessimistic
                px, outc = stop, "STOP"
            else:
                px, outc = c[j_end - 1] if j_end > i0 else entry, "TIME"
            gross = (entry - px) / entry
            net = gross + row["fund"] - COST
            res_1m.append({"sym": sym, "net": net, "net_daily": row["net"],
                           "outcome_1m": outc, "outcome_d": row["outcome"]})
    r1 = pd.DataFrame(res_1m)
    informative_only = len(r1) < 50
    m1, md1 = r1["net"].mean(), r1["net"].median()
    md_sub, mm_sub = r1["net_daily"].median(), r1["net_daily"].mean()
    print(f"F1: 1m-sim   mean {m1*100:.2f}%  median {md1*100:.2f}%  "
          f"t {tstat(r1['net']):.2f}  stops {(r1['outcome_1m']=='STOP').mean()*100:.1f}%")
    print(f"F1: daily-subset mean {mm_sub*100:.2f}%  median {md_sub*100:.2f}%  "
          f"stops {(r1['outcome_d']=='STOP').mean()*100:.1f}%")
    f1_pass = (m1 > 0) and (md1 > 0) and (m1 >= mm_sub - 0.015)
    print(f"F1 bar: {'INFORMATIVE-ONLY (n<50)' if informative_only else ('PASS' if f1_pass else 'FAIL')}")

    # ---- F2: slippage stress ------------------------------------------------
    gf = pri["gross"] + pri["fund"]
    print("\nF2 slippage stress (primary cell, full n):")
    for rt in (0.0070, 0.0100, 0.0200):
        x = gf - rt
        print(f"  RT {rt*1e4:.0f}bp: mean {x.mean()*100:.2f}%  "
              f"median {x.median()*100:.2f}%  t {tstat(x):.2f}")
    be = gf.mean()
    print(f"  breakeven RT cost: {be*1e4:.0f}bp")
    f2_pass = (gf - 0.0200).mean() > 0
    print(f"F2 bar (mean>0 at 200bp RT): {'PASS' if f2_pass else 'FAIL'}")

    # ---- F3: 2026 decay z ---------------------------------------------------
    n26 = pri[pri["year"] == 2026]["net"]
    pre = pri[pri["year"] < 2026]["net"]
    se26 = n26.std(ddof=1) / np.sqrt(len(n26))
    z = (n26.mean() - pre.mean()) / se26
    print(f"\nF3: 2026 mean {n26.mean()*100:.2f}% (n={len(n26)}) vs pre-2026 "
          f"{pre.mean()*100:.2f}%  z={z:.2f}")
    print(f"F3 flag (z<=-2 = decay): {'DECAY FLAG' if z <= -2 else 'CONSISTENT'}")

    # ---- F4: monthly corr to live book --------------------------------------
    pri["month"] = pd.to_datetime(pri["entry_ts"], unit="ms").dt.to_period("M")
    m25 = pri.groupby("month")["net"].sum()
    rows = []
    for fp in glob.glob(os.path.join(ROOT, "results", "live_journals", "*.jsonl")):
        with open(fp) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if e.get("event") == "close":
                    rows.append((e["ts"], float(e["pnl_usd"])))
    lj = pd.DataFrame(rows, columns=["ts", "pnl"])
    lj["month"] = pd.to_datetime(lj["ts"]).dt.tz_localize(None).dt.to_period("M")
    mlive = lj.groupby("month")["pnl"].sum()
    joined = pd.concat([m25, mlive], axis=1, keys=["b25", "live"]).fillna(0.0)
    corr = joined["b25"].corr(joined["live"])
    print(f"\nF4: monthly corr #25 vs live book = {corr:.3f} "
          f"({len(joined)} months)")
    print(f"F4 bar (|corr|<0.5): {'PASS' if abs(corr) < 0.5 else 'FAIL'}")

    # ---- F5: listing-age decomposition (report-only) ------------------------
    age_d = (pri["entry_ts"] - pri["first_kline_ts"]) / 86400_000
    pri["age_bucket"] = pd.cut(age_d, [0, 90, 365, 1e9],
                               labels=["<90d", "90d-1y", ">1y"])
    print("\nF5 listing-age decomposition:")
    print(pri.groupby("age_bucket", observed=True)["net"]
             .agg(["count", "mean", "median"]).round(4))


if __name__ == "__main__":
    main()
