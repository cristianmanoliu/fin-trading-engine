#!/usr/bin/env python3
# regime_label.py — pure causal regime labeler for the BTC-anchored gate.
#
# Given a daily anchor series and (X,Y,Z), stamp each day SHORT/LONG/FLAT using
# ONLY closes up to and including that day (no look-ahead). See spec
# results/btc_regime_gate_backtest_decision_rule_2026-06-07.md §5.
#
# CLI: python3 regime_label.py --anchor data/anchor/BTCUSDT-1d.csv \
#         --x 10 --y 14 --z 14 [--out timeline.csv]
# Prints/writes: date,label
import argparse
import csv
import sys


def load_anchor(path):
    rows = []
    with open(path, newline="") as f:
        r = csv.DictReader(f)
        for rec in r:
            rows.append((rec["date"], float(rec["close"])))
    rows.sort(key=lambda t: t[0])
    return rows


def label_timeline(rows, x_pct, y_days, z_days):
    thr = x_pct / 100.0
    closes = [c for _, c in rows]
    out = []
    for i, (d, _c) in enumerate(rows):
        label = "FLAT"
        short_fires = False
        long_fires = False
        if i >= y_days and closes[i - y_days] > 0:
            ret_y = (closes[i] - closes[i - y_days]) / closes[i - y_days]
            if ret_y <= -thr:
                short_fires = True
        if i >= z_days and closes[i - z_days] > 0:
            ret_z = (closes[i] - closes[i - z_days]) / closes[i - z_days]
            if ret_z >= thr:
                long_fires = True
        # Tie-break: SHORT wins (locked decision).
        if short_fires:
            label = "SHORT"
        elif long_fires:
            label = "LONG"
        out.append((d, label))
    return out


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--anchor", required=True)
    ap.add_argument("--x", type=float, required=True)
    ap.add_argument("--y", type=int, required=True)
    ap.add_argument("--z", type=int, required=True)
    ap.add_argument("--out", default="")
    args = ap.parse_args(argv)
    rows = load_anchor(args.anchor)
    tl = label_timeline(rows, args.x, args.y, args.z)
    fh = open(args.out, "w", newline="") if args.out else sys.stdout
    w = csv.writer(fh)
    w.writerow(["date", "label"])
    w.writerows(tl)
    if args.out:
        fh.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
