#!/usr/bin/env python3
"""batch3_21_analysis.py — batch-3 candidate #21: RSI/MACD deep validation.

Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#21).
Cells: baseline (results/live_journals), MACD-12/26/9 + RSI-14
(results/batch3_journals/{macd,rsi}). Deployed-20 universe, 2020->2026-06,
live cost model. Locked criteria for CANDIDATE-CONFIRMED vs baseline:
  (a) positive mean AND median per-trade net
  (b) mean stays positive after drop-top-5% of trades
  (c) >=4/6 yearly windows net-positive (2020..2025; 2026 partial reported info-only)
  (d) >=5/7 calendar years positive
  (e) monthly-PnL corr to baseline < 0.7 OR total net > baseline
"""
import glob
import json
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CELLS = {
    "baseline": os.path.join(ROOT, "results", "live_journals"),
    "macd": os.path.join(ROOT, "results", "batch3_journals", "macd"),
    "rsi": os.path.join(ROOT, "results", "batch3_journals", "rsi"),
}


def load(cell_dir):
    rows = []
    for fp in glob.glob(os.path.join(cell_dir, "*.jsonl")):
        with open(fp) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if e.get("event") != "close":
                    continue
                rows.append({"symbol": e["symbol"], "ts": e["ts"],
                             "pnl": float(e["pnl_usd"])})
    df = pd.DataFrame(rows)
    df["ts"] = pd.to_datetime(df["ts"], utc=True)
    df["year"] = df["ts"].dt.year
    df["month"] = df["ts"].dt.to_period("M")
    return df.sort_values("ts")


def stats(df, name):
    pnl = df["pnl"].values
    cut = np.quantile(pnl, 0.95)
    drop5 = pnl[pnl <= cut]
    yearly = df.groupby("year")["pnl"].sum()
    full_years = yearly[yearly.index <= 2025]
    out = {
        "cell": name, "trades": len(pnl),
        "total": round(pnl.sum()), "mean": round(pnl.mean(), 1),
        "median": round(np.median(pnl), 1),
        "wr%": round((pnl > 0).mean() * 100, 1),
        "drop5_mean": round(drop5.mean(), 1),
        "drop5_total": round(drop5.sum()),
        "pos_years_full": f"{int((full_years > 0).sum())}/6",
        "pos_years_all": f"{int((yearly > 0).sum())}/{len(yearly)}",
        "t_mean": round(pnl.mean() / (pnl.std(ddof=1) / np.sqrt(len(pnl))), 2),
    }
    return out, yearly, df.groupby("month")["pnl"].sum()


def main():
    res, yearlies, monthlies = {}, {}, {}
    for name, d in CELLS.items():
        df = load(d)
        s, yr, mo = stats(df, name)
        res[name], yearlies[name], monthlies[name] = s, yr, mo

    tab = pd.DataFrame(res.values())
    print("=== #21 per-trade stats (deployed-20, 2020–2026-06, fee10/slip5/mh504) ===")
    print(tab.to_string(index=False))

    print("\n=== yearly net by cell ===")
    ytab = pd.DataFrame(yearlies).round(0)
    print(ytab.to_string())

    print("\n=== locked criteria vs baseline ===")
    base_mo = monthlies["baseline"]
    base_total = res["baseline"]["total"]
    for name in ("macd", "rsi"):
        r = res[name]
        mo = monthlies[name]
        joined = pd.concat([base_mo, mo], axis=1, keys=["b", "v"]).fillna(0.0)
        corr = joined["b"].corr(joined["v"])
        ya = yearlies[name]
        full = ya[ya.index <= 2025]
        crit = {
            "a_mean_median_pos": r["mean"] > 0 and r["median"] > 0,
            "b_drop5_pos": r["drop5_mean"] > 0,
            "c_windows_4of6": int((full > 0).sum()) >= 4,
            "d_years_5of7": int((ya > 0).sum()) >= 5,
            "e_corr_or_beats": (corr < 0.7) or (r["total"] > base_total),
        }
        verdict = "CANDIDATE-CONFIRMED" if all(crit.values()) else "NO-GO"
        print(f"\n{name}: corr_monthly_to_baseline={corr:.3f} "
              f"total={r['total']} vs baseline={base_total}")
        for k, v in crit.items():
            print(f"  {k}: {'PASS' if v else 'FAIL'}")
        print(f"  VERDICT: {verdict}")

    pd.DataFrame(res.values()).to_csv(
        os.path.join(ROOT, "results", "batch3_21_cells_2026-06-10.csv"), index=False)


if __name__ == "__main__":
    main()
