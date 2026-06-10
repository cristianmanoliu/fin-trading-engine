#!/usr/bin/env python3
"""btc_leadlag_study.py — batch-3 candidate #23: BTC→alt lead-lag spillover.

Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#23).
EVENT STUDY ONLY. Screen: |conditional mean forward return| in BTC-move direction
must exceed 15bp (taker RT + slip) at >=1 horizon with |t|>3 (event-level t) AND
survive the honesty block. Fail -> NO-GO, no strategy build.

Grid (locked): z in {2,3} x horizons {15m, 1H, 4H} = 6 cells.
Events: BTC trailing 1H return z-scored on rolling 30d sigma; events deduped so no
two events are within 4h (longest horizon) — avoids overlap inflation.

Data: local 1m CSVs (data/<SYM>-1m-YYYY-MM.csv symlinks), all 57 symbols.
Cache: /tmp/b3_closes_15m.pkl (15m close matrix, reused by later batch-3 studies).
"""
import glob
import os
import pickle
import sys

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CACHE = "/tmp/b3_closes_15m.pkl"
COST_BP = 15.0
HORIZONS = {"15m": 1, "1H": 4, "4H": 16}  # in 15m steps
ZTHRESH = [2.0, 3.0]


def load_closes_15m():
    if os.path.exists(CACHE):
        with open(CACHE, "rb") as f:
            return pickle.load(f)
    syms = sorted({os.path.basename(p).split("-1m-")[0]
                   for p in glob.glob(os.path.join(ROOT, "data", "*-1m-*.csv"))})
    series = {}
    for i, sym in enumerate(syms):
        files = sorted(glob.glob(os.path.join(ROOT, "data", f"{sym}-1m-*.csv")))
        parts = []
        for fp in files:
            try:
                df = pd.read_csv(fp, usecols=[0, 4], header=0,
                                 names=["open_time", "close"], skiprows=1)
            except Exception:
                continue
            parts.append(df)
        if not parts:
            continue
        df = pd.concat(parts, ignore_index=True)
        df = df.dropna()
        df["open_time"] = pd.to_numeric(df["open_time"], errors="coerce")
        df = df.dropna().drop_duplicates("open_time").sort_values("open_time")
        ts = pd.to_datetime(df["open_time"], unit="ms", utc=True)
        s = pd.Series(df["close"].values, index=ts)
        series[sym] = s.resample("15min").last()
        print(f"  [{i+1}/{len(syms)}] {sym}: {len(series[sym])} 15m bars", flush=True)
    closes = pd.DataFrame(series)
    with open(CACHE, "wb") as f:
        pickle.dump(closes, f)
    return closes


def main():
    closes = load_closes_15m()
    # CRITICAL alignment fix: resample('15min').last() labels bars at the LEFT
    # edge — the bar labeled 10:00 closes at ~10:15. Shift labels to the RIGHT
    # edge so every timestamp IS the moment of its price. Without this, the
    # first run booked contemporaneous co-movement as "forward" return
    # (t=25-37, net 140-197bp — a pure look-ahead artifact).
    closes = closes.copy()
    closes.index = closes.index + pd.Timedelta(minutes=15)
    btc = closes["BTCUSDT"].dropna()
    # hourly closes: right-edge-labeled 15m series -> bucket (10:00,11:00]
    # contains 10:15..11:00, last = price AT 11:00, labeled 11:00.
    btc_1h = btc.resample("1h", label="right", closed="right").last().dropna()
    r1h = btc_1h.pct_change()
    sigma = r1h.rolling(720, min_periods=240).std()
    z = r1h / sigma
    alts = [c for c in closes.columns if c != "BTCUSDT"]

    rows = []
    for zt in ZTHRESH:
        ev = z[abs(z) >= zt].dropna()
        # dedupe: no two events within 4h
        kept, last_t = [], None
        for t, val in ev.items():
            if last_t is None or (t - last_t) >= pd.Timedelta(hours=4):
                kept.append((t, np.sign(val)))
                last_t = t
        for hname, hsteps in HORIZONS.items():
          for delay_bars in (0, 1):  # 0 = fill at signal close; 1 = +15m delay
            obs = []           # (year, dir-adjusted fwd return) trade-level
            ev_means = []      # per-event cross-alt mean (event-level t)
            ev_years = []
            for t, s in kept:
                # right-edge labels: t IS the moment of the event-hour close
                t15 = t + pd.Timedelta(minutes=15 * delay_bars)
                rets = []
                for a in alts:
                    col = closes[a]
                    try:
                        p0 = col.at[t15]
                    except KeyError:
                        continue
                    t1 = t15 + pd.Timedelta(minutes=15 * hsteps)
                    try:
                        p1 = col.at[t1]
                    except KeyError:
                        continue
                    if pd.isna(p0) or pd.isna(p1) or p0 <= 0:
                        continue
                    rets.append(s * (p1 / p0 - 1.0))
                if rets:
                    rets = np.array(rets)
                    obs.append((t.year, rets))
                    ev_means.append(rets.mean())
                    ev_years.append(t.year)
            if not ev_means:
                continue
            ev_means = np.array(ev_means)
            all_obs = np.concatenate([r for _, r in obs])
            yr = pd.Series(ev_means, index=ev_years).groupby(level=0).mean()
            n_pos_years = int((yr > 0).sum())
            mean_bp = all_obs.mean() * 1e4
            med_bp = np.median(all_obs) * 1e4
            t_ev = ev_means.mean() / (ev_means.std(ddof=1) / np.sqrt(len(ev_means)))
            cut = np.quantile(all_obs, 0.95)
            drop5_bp = all_obs[all_obs <= cut].mean() * 1e4
            rows.append({
                "z": zt, "horizon": hname, "delay": delay_bars,
                "n_events": len(ev_means),
                "n_obs": len(all_obs), "mean_bp": round(mean_bp, 2),
                "median_bp": round(med_bp, 2), "t_event": round(t_ev, 2),
                "drop5_bp": round(drop5_bp, 2),
                "net_bp": round(mean_bp - COST_BP, 2),
                "pos_years": f"{n_pos_years}/{len(yr)}",
            })
            print(rows[-1], flush=True)

    out = pd.DataFrame(rows)
    out.to_csv(os.path.join(ROOT, "results", "btc_leadlag_cells_2026-06-10.csv"),
               index=False)
    print("\n=== #23 BTC→alt lead-lag — locked grid results ===")
    print(out.to_string(index=False))
    passing = out[(out["net_bp"].abs() > 0) & (out["mean_bp"] > COST_BP)
                  & (out["t_event"].abs() > 3) & (out["median_bp"] > 0)
                  & (out["drop5_bp"] > 0)]
    print(f"\nScreen-passing cells (mean>15bp, |t|>3, median>0, drop5>0): {len(passing)}")


if __name__ == "__main__":
    sys.exit(main())
