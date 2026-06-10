#!/usr/bin/env python3
"""ethbtc_rv_study.py — batch-3 candidate #22: ETH/BTC majors-only RV pair.

Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#22).
Grid (locked, 6 cells): trend sign(trailing k-day ratio return), k in {7,14,30},
daily rebalance; MR fade z=(ratio-SMA30)/SD30, entry |z| in {1.5,2.0,2.5}, exit
z crosses 0. Dollar-neutral $1/$1; pair return = pos*(r_eth - r_btc) daily.
Costs (locked): 15bp x |delta pos| (full pair open+close cycle = 30bp) + per-leg
funding from data/funding CSVs (long pays positive funding, short receives).
Bar (locked): grid-mean ann.Sharpe > 0.8 AND best cell passes honesty block
(median daily net > 0 ... evaluated on in-position days, drop-top-5% positive,
>=5/7 years positive). Sharpe per-period, NO compounding.

Timestamps: right-edge labels throughout (#23 lesson).
"""
import os
import pickle

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CACHE = "/tmp/b3_closes_15m.pkl"
COST_PER_LEGCHANGE = 0.0015  # 15bp per unit |delta pos|; flip=30bp


def daily_funding(sym):
    fp = os.path.join(ROOT, "data", "funding", f"{sym}.csv")
    df = pd.read_csv(fp)
    ts = pd.to_datetime(df["funding_time_ms"], unit="ms", utc=True)
    s = pd.Series(pd.to_numeric(df["funding_rate"]), index=ts)
    # right-edge daily sum: funding paid during day D lands on label D+1 00:00
    return s.resample("1D", label="right", closed="right").sum()


def main():
    with open(CACHE, "rb") as f:
        closes = pickle.load(f)
    closes = closes.copy()
    closes.index = closes.index + pd.Timedelta(minutes=15)  # right-edge labels
    px = closes[["ETHUSDT", "BTCUSDT"]].dropna()
    daily = px.resample("1D", label="right", closed="right").last().dropna()
    r = daily.pct_change()
    ratio = daily["ETHUSDT"] / daily["BTCUSDT"]
    pair_ret = r["ETHUSDT"] - r["BTCUSDT"]  # long-ETH-short-BTC daily return

    f_eth = daily_funding("ETHUSDT").reindex(daily.index).fillna(0.0)
    f_btc = daily_funding("BTCUSDT").reindex(daily.index).fillna(0.0)
    # pos=+1 (long ETH / short BTC): pay f_eth, receive f_btc -> pnl += -f_eth + f_btc
    fund_pnl_unit = -f_eth + f_btc

    cells = []

    def run_cell(name, pos):
        pos = pos.reindex(daily.index).fillna(0.0)
        # position decided at close t earns return t -> t+1
        held = pos.shift(1).fillna(0.0)
        gross = held * pair_ret
        fund = held * fund_pnl_unit
        tcost = COST_PER_LEGCHANGE * pos.diff().abs().fillna(abs(pos.iloc[0]))
        net = (gross + fund - tcost).dropna()
        in_pos = net[held.reindex(net.index).fillna(0.0) != 0]
        if len(in_pos) < 50:
            return
        sharpe = net.mean() / net.std(ddof=1) * np.sqrt(365) if net.std(ddof=1) > 0 else 0.0
        yearly = net.groupby(net.index.year).sum()
        cut = np.quantile(in_pos, 0.95)
        drop5 = in_pos[in_pos <= cut]
        cells.append({
            "cell": name, "days_in_pos": len(in_pos),
            "ann_sharpe": round(sharpe, 3),
            "mean_bp_d": round(net.mean() * 1e4, 2),
            "median_bp_inpos": round(np.median(in_pos) * 1e4, 2),
            "drop5_mean_bp": round(drop5.mean() * 1e4, 2),
            "total_ret_pct": round(net.sum() * 100, 1),
            "pos_years": f"{int((yearly > 0).sum())}/{len(yearly)}",
        })
        print(cells[-1], flush=True)

    # trend cells
    for k in (7, 14, 30):
        sig = np.sign(ratio / ratio.shift(k) - 1.0)
        run_cell(f"trend_k{k}", sig)

    # MR cells
    sma = ratio.rolling(30).mean()
    sd = ratio.rolling(30).std()
    z = (ratio - sma) / sd
    for zt in (1.5, 2.0, 2.5):
        pos = pd.Series(np.nan, index=ratio.index)
        pos[z > zt] = -1.0   # ratio rich -> short ETH / long BTC
        pos[z < -zt] = 1.0
        # exit when z crosses 0: flatten there, else carry position
        crossed = (np.sign(z) != np.sign(z.shift(1))) & z.shift(1).notna()
        pos[crossed & pos.isna()] = 0.0
        pos = pos.ffill().fillna(0.0)
        run_cell(f"mr_z{zt}", pos)

    out = pd.DataFrame(cells)
    out.to_csv(os.path.join(ROOT, "results", "ethbtc_rv_cells_2026-06-10.csv"),
               index=False)
    print("\n=== #22 ETH/BTC RV — locked grid ===")
    print(out.to_string(index=False))
    gm = out["ann_sharpe"].mean()
    print(f"\nGrid-mean ann.Sharpe = {gm:.3f}  (bar: > 0.8)")
    best = out.loc[out["ann_sharpe"].idxmax()]
    print(f"Best cell: {best['cell']} Sharpe {best['ann_sharpe']} "
          f"median_inpos {best['median_bp_inpos']}bp drop5 {best['drop5_mean_bp']}bp "
          f"years {best['pos_years']}")


if __name__ == "__main__":
    main()
