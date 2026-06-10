#!/usr/bin/env python3
"""failed_pump_study.py — batch-3 candidate #25: failed-pump cascade short.

Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#25).
Universe: de-survivorship — ALL 732 ever-listed perps (data/listing/klines daily,
data/listing/funding). Pump: close[t]/close[t-2]-1 >= P, P in {25%,50%}.
Failure trigger: first day within 10d post-pump closing below prior day's low.
Entry: next open. Stop: max high from pump window through trigger day.
Target: entry - 1R (= stop distance), capped at -33% absolute (pre-reg phrase
"-33% of entry-to-stop distance gain" was ambiguous; this merges both readings
conservatively — documented deviation). Max hold 7d, exit at close.
Same-day ambiguity resolved PESSIMISTICALLY: stop checked before target.
Costs: 70bp round-trip (35bp/side, illiquid-alt lesson) + actual funding
(short RECEIVES positive funding) summed over the hold.
Data ending mid-trade (delisting): forced exit at last close — kept (that IS
the de-survivorship point).
Verdict bar (locked): honesty block AND mean net > 0 with t > 2 in both cells
or in primary cell P=25%.
"""
import glob
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# F6 overrides: PUMP_KDIR / PUMP_FDIR point at the full-history fetch;
# PUMP_SKIP_FIRST_DAYS=90 runs the ex-listing-window slice; PUMP_OUT_TAG
# suffixes the output CSVs so the original locked artifacts stay untouched.
KDIR = os.environ.get("PUMP_KDIR", os.path.join(ROOT, "data", "listing", "klines"))
FDIR = os.environ.get("PUMP_FDIR", os.path.join(ROOT, "data", "listing", "funding"))
SKIP_FIRST_DAYS = int(os.environ.get("PUMP_SKIP_FIRST_DAYS", "0"))
OUT_TAG = os.environ.get("PUMP_OUT_TAG", "")
COST = 0.0070
MAXHOLD = 7
TRIGWIN = 10


def load_funding(sym):
    fp = os.path.join(FDIR, f"{sym}.csv")
    if not os.path.exists(fp):
        return None
    try:
        with open(fp) as fh:
            ncols = fh.readline().count(",") + 1
        # listing schema: calc_time,funding_interval_hours,last_funding_rate
        # fullhist schema: calc_time,funding_rate — sniff by column count so a
        # mismatch can't silently zero the funding join (audit-lens class bug)
        names = (["calc_time", "funding_interval_hours", "last_funding_rate"]
                 if ncols == 3 else ["calc_time", "last_funding_rate"])
        df = pd.read_csv(fp, header=None, names=names, dtype=str,
                         on_bad_lines="skip")
        ct = pd.to_numeric(df["calc_time"], errors="coerce")
        rate = pd.to_numeric(df["last_funding_rate"], errors="coerce")
        ok = ct.notna() & rate.notna()
        ts = pd.to_datetime(ct[ok], unit="ms", utc=True)
        # NB: pass .values — Series(series, index=...) REINDEXES, silently
        # producing an empty join (the bug the first run shipped with)
        return pd.Series(rate[ok].values, index=ts).sort_index()
    except Exception:
        return None


def main():
    trades = {0.25: [], 0.50: []}
    files = sorted(glob.glob(os.path.join(KDIR, "*-1d.csv")))
    for fp in files:
        sym = os.path.basename(fp).replace("-1d.csv", "")
        # header presence varies by download vintage; read headerless with
        # fixed names, then coerce — header rows become NaN and drop out
        df = pd.read_csv(fp, header=None,
                         names=["open_time", "open", "high", "low", "close",
                                "volume", "close_time", "qv", "count",
                                "tbv", "tbqv", "ignore"],
                         dtype=str, on_bad_lines="skip")
        for col in ("open_time", "open", "high", "low", "close"):
            df[col] = pd.to_numeric(df[col], errors="coerce")
        df = df.dropna(subset=["open_time", "open", "high", "low", "close"])
        df = df.drop_duplicates("open_time").sort_values("open_time").reset_index(drop=True)
        if len(df) < 10:
            continue
        df["ts"] = pd.to_datetime(df["open_time"], unit="ms", utc=True)
        o = df["open"].values; h = df["high"].values
        l = df["low"].values; c = df["close"].values
        ts = df["ts"].values
        fund = load_funding(sym)
        n = len(df)
        for P in (0.25, 0.50):
            i = max(2, SKIP_FIRST_DAYS)
            while i < n:
                if c[i - 2] <= 0 or c[i] / c[i - 2] - 1.0 < P:
                    i += 1
                    continue
                pump_day = i
                # find failure trigger within TRIGWIN days
                trig = -1
                for d in range(pump_day + 1, min(pump_day + 1 + TRIGWIN, n)):
                    if c[d] < l[d - 1]:
                        trig = d
                        break
                if trig < 0 or trig + 1 >= n:
                    i = pump_day + 1
                    continue
                e = trig + 1
                entry = o[e]
                stop = max(h[pump_day - 2:trig + 1])
                if entry <= 0 or stop <= entry:
                    i = pump_day + 1
                    continue
                risk = stop - entry
                target = max(entry - risk, entry * 0.67)
                exit_px, exit_d, outcome = None, None, None
                for d in range(e, min(e + MAXHOLD, n)):
                    if h[d] >= stop:          # pessimistic: stop first
                        exit_px, exit_d, outcome = stop, d, "STOP"
                        break
                    if l[d] <= target:
                        exit_px, exit_d, outcome = target, d, "TARGET"
                        break
                if exit_px is None:
                    d = min(e + MAXHOLD, n) - 1
                    exit_px, exit_d = c[d], d
                    outcome = "TIME" if d == e + MAXHOLD - 1 else "DELIST"
                gross = (entry - exit_px) / entry  # short return
                f_pnl = 0.0
                if fund is not None and len(fund):
                    t0, t1 = pd.Timestamp(ts[e], tz="UTC"), pd.Timestamp(ts[exit_d], tz="UTC") + pd.Timedelta(days=1)
                    f_pnl = float(fund[(fund.index >= t0) & (fund.index < t1)].sum())
                net = gross + f_pnl - COST
                trades[P].append({
                    "sym": sym, "year": pd.Timestamp(ts[e]).year,
                    "net": net, "gross": gross, "fund": f_pnl,
                    "outcome": outcome,
                    "P_cell": P,
                    "entry_ts": int(ts[e].astype("datetime64[ms]").astype(np.int64)),
                    "exit_ts": int(ts[exit_d].astype("datetime64[ms]").astype(np.int64)),
                    "entry": float(entry), "stop": float(stop),
                    "target": float(target),
                    "first_kline_ts": int(ts[0].astype("datetime64[ms]").astype(np.int64)),
                })
                i = exit_d + 1  # no overlapping trades per symbol
            # reset loop var for next P handled by fresh while
    rows = []
    for P, tl in trades.items():
        t = pd.DataFrame(tl)
        if t.empty:
            continue
        x = t["net"].values
        cut = np.quantile(x, 0.95)
        drop5 = x[x <= cut]
        yearly = t.groupby("year")["net"].mean()
        rows.append({
            "P": P, "n": len(x), "syms": t["sym"].nunique(),
            "mean%": round(x.mean() * 100, 2),
            "median%": round(np.median(x) * 100, 2),
            "wr%": round((x > 0).mean() * 100, 1),
            "t": round(x.mean() / (x.std(ddof=1) / np.sqrt(len(x))), 2),
            "drop5%": round(drop5.mean() * 100, 2),
            "fund%": round(t["fund"].mean() * 100, 3),
            "pos_years": f"{int((yearly > 0).sum())}/{len(yearly)}",
            "stops%": round((t["outcome"] == "STOP").mean() * 100, 1),
        })
        print(rows[-1], flush=True)
        print(t.groupby("year")["net"].agg(["count", "mean", "median"]).round(4))
    out = pd.DataFrame(rows)
    out.to_csv(os.path.join(ROOT, "results",
                            f"failed_pump_cells{OUT_TAG}_2026-06-10.csv"),
               index=False)
    all_trades = pd.DataFrame(trades[0.25] + trades[0.50])
    all_trades.to_csv(os.path.join(ROOT, "results",
                                   f"failed_pump_trades{OUT_TAG}_2026-06-10.csv"),
                      index=False)
    print("\n=== #25 failed-pump cascade short — locked cells ===")
    print(out.to_string(index=False))


if __name__ == "__main__":
    main()
