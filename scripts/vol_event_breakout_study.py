#!/usr/bin/env python3
"""vol_event_breakout_study.py — batch-3 candidate #24: vol-event two-sided breakout.

Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#24).
The direction-free expression of the 20-candidate search's one robust finding
(gates mark WHEN 60-330bp moves happen, not WHICH WAY).

Gate families (locked, one at a time, no compositing):
  macro      — CPI/FOMC/NFP timestamps (calendar from macro_event_study.py)
  oi_z       — |z|>2 spike of 1h dlog(OI), z on rolling 30d (5-min metrics archive,
               available 2021-12+), deduped >=4h
  settlement — EXTREME_LOW funding settlements (#11 gate: bottom 10% of negative
               rates, per-symbol); brackets armed 2h BEFORE settlement (the #11
               pre-window). NB: realized rate used to select windows (as in #11) —
               in live the premium index approximates it in advance; documented.

Mechanism (locked): at gate fire, p0 = last 1m close; A = last completed
ATR(4H,14); brackets p0 +/- 0.75A armed for 4h (implementation constant, untuned).
First 1m close beyond a bracket fills AT the bracket. Stop = opposite bracket
(R = 1.5A), target = fill +/- 2R, time exit 24h. 1m closes, deployed-16.
Costs: 15bp RT on filled leg; unfilled events cost nothing.

Verdict bar (locked): mean AND median net per filled event > 0, drop-top-5% > 0,
>=5/7 yrs (>=4/5 for oi_z, data from 2021-12), across >=2 of 3 gate families.
"""
import glob
import os
import sys

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "scripts"))
from macro_event_study import build_calendar, event_ms  # noqa: E402

SYMS = ("ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT "
        "ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT APTUSDT DOTUSDT "
        "FILUSDT").split()
COST = 0.0015
K_BRACKET = 0.75
ARM_MS = 4 * 3600_000
HOLD_MS = 24 * 3600_000
EXTREME_PCT = 0.10


def load_1m(sym):
    parts = []
    for fp in sorted(glob.glob(os.path.join(ROOT, "data", f"{sym}-1m-*.csv"))):
        try:
            parts.append(pd.read_csv(fp, usecols=[0, 2, 3, 4], header=0,
                                     names=["t", "high", "low", "close"],
                                     skiprows=1))
        except Exception:
            continue
    df = pd.concat(parts, ignore_index=True)
    for c in df.columns:
        df[c] = pd.to_numeric(df[c], errors="coerce")
    df = (df.dropna().drop_duplicates("t").sort_values("t")
            .reset_index(drop=True))
    return (df["t"].values.astype(np.int64), df["high"].values,
            df["low"].values, df["close"].values)


def atr_4h(t, h, l, c):
    """Right-edge 4H ATR(14): series indexed by bar END ms."""
    ts = pd.to_datetime(t, unit="ms", utc=True)
    df = pd.DataFrame({"h": h, "l": l, "c": c}, index=ts)
    o4 = df.resample("4h", label="right", closed="right").agg(
        {"h": "max", "l": "min", "c": "last"}).dropna()
    pc = o4["c"].shift(1)
    tr = pd.concat([o4["h"] - o4["l"], (o4["h"] - pc).abs(),
                    (o4["l"] - pc).abs()], axis=1).max(axis=1)
    atr = tr.rolling(14).mean()
    return atr.index.view(np.int64) // 10**6, atr.values


def simulate(t, c, atr_t, atr_v, ev_ms_list):
    """Two-sided bracket sim on 1m closes. Returns list of (year, net, whipsaw)."""
    out = []
    for ems in ev_ms_list:
        i0 = np.searchsorted(t, ems, side="right") - 1
        if i0 < 1:
            continue
        ai = np.searchsorted(atr_t, ems, side="right") - 1
        if ai < 0 or np.isnan(atr_v[ai]):
            continue
        p0, A = c[i0], atr_v[ai]
        if p0 <= 0 or A <= 0:
            continue
        up, dn = p0 + K_BRACKET * A, p0 - K_BRACKET * A
        j_end = np.searchsorted(t, ems + ARM_MS, side="right")
        w = c[i0 + 1:j_end]
        hit_up = np.argmax(w >= up) if (w >= up).any() else -1
        hit_dn = np.argmax(w <= dn) if (w <= dn).any() else -1
        if hit_up < 0 and hit_dn < 0:
            continue  # no fill, no cost
        if hit_dn < 0 or (hit_up >= 0 and hit_up <= hit_dn):
            side, fill, stop = 1.0, up, dn
            fi = i0 + 1 + hit_up
        else:
            side, fill, stop = -1.0, dn, up
            fi = i0 + 1 + hit_dn
        R = 1.5 * A
        target = fill + side * 2.0 * R
        k_end = np.searchsorted(t, t[fi] + HOLD_MS, side="right")
        seg = c[fi + 1:k_end]
        if side > 0:
            s_hit = np.argmax(seg <= stop) if (seg <= stop).any() else -1
            t_hit = np.argmax(seg >= target) if (seg >= target).any() else -1
        else:
            s_hit = np.argmax(seg >= stop) if (seg >= stop).any() else -1
            t_hit = np.argmax(seg <= target) if (seg <= target).any() else -1
        whip = False
        if s_hit >= 0 and (t_hit < 0 or s_hit <= t_hit):
            px, whip = stop, True
        elif t_hit >= 0:
            px = target
        elif len(seg):
            px = seg[-1]
        else:
            px = fill
        net = side * (px - fill) / fill - COST
        yr = pd.Timestamp(ems, unit="ms", tz="UTC").year
        out.append((yr, net, whip))
    return out


def gate_macro(sym, t):
    cal = build_calendar()
    return [event_ms(d, hm) for d, hm, _ in cal]


def gate_oi(sym, t):
    fp = os.path.join(ROOT, "data", "metrics", f"{sym}.csv")
    if not os.path.exists(fp):
        return []
    df = pd.read_csv(fp, usecols=["create_time", "sum_open_interest"])
    ts = pd.to_datetime(df["create_time"], utc=True)
    oi = pd.Series(pd.to_numeric(df["sum_open_interest"],
                                 errors="coerce").values, index=ts).dropna()
    oi_1h = oi.resample("1h", label="right", closed="right").last().dropna()
    dlog = np.log(oi_1h).diff()
    z = dlog / dlog.rolling(720, min_periods=240).std()
    ev = z[abs(z) >= 2.0].dropna()
    kept, last = [], None
    for tt in ev.index:
        if last is None or (tt - last) >= pd.Timedelta(hours=4):
            kept.append(int(tt.value // 10**6))
            last = tt
    return kept


def gate_settlement(sym, t):
    fp = os.path.join(ROOT, "data", "funding", f"{sym}.csv")
    df = pd.read_csv(fp)
    rate = pd.to_numeric(df["funding_rate"], errors="coerce")
    tms = pd.to_numeric(df["funding_time_ms"], errors="coerce")
    ok = rate.notna() & tms.notna()
    rate, tms = rate[ok].values, tms[ok].values.astype(np.int64)
    neg = rate[rate < 0]
    if len(neg) < 20:
        return []
    lo = np.quantile(neg, EXTREME_PCT)
    return [int(m - 2 * 3600_000) for m, r in zip(tms, rate) if r <= lo]


def main():
    fams = {"macro": gate_macro, "oi_z": gate_oi, "settlement": gate_settlement}
    results = {k: [] for k in fams}
    for sym in SYMS:
        t, h, l, c = load_1m(sym)
        at, av = atr_4h(t, h, l, c)
        for fam, fn in fams.items():
            evs = sorted(fn(sym, t))
            results[fam].extend(simulate(t, c, at, av, evs))
        print(f"{sym}: done "
              + " ".join(f"{k}={len(v)}" for k, v in results.items()),
              flush=True)

    rows = []
    for fam, tl in results.items():
        if not tl:
            continue
        df = pd.DataFrame(tl, columns=["year", "net", "whip"])
        x = df["net"].values
        cut = np.quantile(x, 0.95)
        yearly = df.groupby("year")["net"].mean()
        rows.append({
            "gate": fam, "fills": len(x),
            "mean_bp": round(x.mean() * 1e4, 1),
            "median_bp": round(np.median(x) * 1e4, 1),
            "t": round(x.mean() / (x.std(ddof=1) / np.sqrt(len(x))), 2),
            "drop5_bp": round(x[x <= cut].mean() * 1e4, 1),
            "whipsaw%": round(df["whip"].mean() * 100, 1),
            "pos_years": f"{int((yearly > 0).sum())}/{len(yearly)}",
        })
        print(rows[-1], flush=True)
        print(df.groupby("year")["net"].agg(["count", "mean"]).round(4))
    out = pd.DataFrame(rows)
    out.to_csv(os.path.join(ROOT, "results",
                            "vol_event_breakout_cells_2026-06-10.csv"),
               index=False)
    print("\n=== #24 vol-event two-sided breakout — gate families ===")
    print(out.to_string(index=False))


if __name__ == "__main__":
    main()
