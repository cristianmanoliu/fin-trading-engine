#!/usr/bin/env python3
# walk_forward.py — expanding-window walk-forward verdict for the BTC regime gate.
# Reads tasks/regime_gate_results/episodes_*.csv (produced by the grid runner),
# fits (X,Y,Z) on early years, scores on the next unseen year, and compares the
# OOS gate P&L to the always-short baseline on the SAME years. Applies the three
# locked success criteria (spec §8).
#
# Usage: python3 tasks/walk_forward.py [--resdir tasks/regime_gate_results]
import argparse
import csv
import os

GRID = [(x, y, z) for x in (5, 10, 15, 20) for y in (7, 14, 30) for z in (7, 14, 30)]
FOLDS = [  # (fit_years, test_year)
    (["2020", "2021", "2022"], "2023"),
    (["2020", "2021", "2022", "2023"], "2024"),
    (["2020", "2021", "2022", "2023", "2024"], "2025"),
    (["2020", "2021", "2022", "2023", "2024", "2025"], "2026"),
]


def _path(resdir, x, y, z, baseline):
    suf = "_baseline" if baseline else ""
    return os.path.join(resdir, f"episodes_X{x}_Y{y}_Z{z}{suf}.csv")


def annual_pnls(resdir, x, y, z, baseline=False):
    out = {}
    p = _path(resdir, x, y, z, baseline)
    if not os.path.exists(p):
        return out
    with open(p, newline="") as f:
        for rec in csv.DictReader(f):
            yr = rec["year"]
            out[yr] = out.get(yr, 0.0) + float(rec["net_pnl"])
    return out


def fit_best(annual_fn, fit_years, grid):
    best = None
    best_sum = None
    for (x, y, z) in grid:
        ap = annual_fn(x, y, z)
        s = sum(ap.get(yr, 0.0) for yr in fit_years)
        if best_sum is None or s > best_sum:
            best_sum = s
            best = (x, y, z)
    return best


def no_consecutive_losing(year_pnls):
    for i in range(1, len(year_pnls)):
        if year_pnls[i] < 0 and year_pnls[i - 1] < 0:
            return False
    return True


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--resdir", default="tasks/regime_gate_results")
    args = ap.parse_args(argv)
    resdir = args.resdir

    annual_fn = lambda x, y, z: annual_pnls(resdir, x, y, z, baseline=False)

    rows = []
    oos_seq = []
    picks = []
    for fit_years, test_year in FOLDS:
        x, y, z = fit_best(annual_fn, fit_years, GRID)
        picks.append((x, y, z))
        gate_test = annual_pnls(resdir, x, y, z, baseline=False).get(test_year, 0.0)
        base_test = annual_pnls(resdir, x, y, z, baseline=True).get(test_year, 0.0)
        gate_fit = sum(annual_fn(x, y, z).get(yr, 0.0) for yr in fit_years)
        rows.append({
            "test_year": test_year, "pick_X": x, "pick_Y": y, "pick_Z": z,
            "gate_oos_pnl": round(gate_test, 2),
            "baseline_oos_pnl": round(base_test, 2),
            "gate_insample_fit_pnl": round(gate_fit, 2),
        })
        oos_seq.append((test_year, gate_test, base_test))

    wf = os.path.join(resdir, "walk_forward.csv")
    with open(wf, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    gate_total = sum(g for _, g, _ in oos_seq)
    base_total = sum(b for _, _, b in oos_seq)
    gate_years = [g for _, g, _ in oos_seq]

    crit1 = gate_total > base_total
    crit2 = no_consecutive_losing(gate_years)
    distinct = set(picks)
    crit3 = len(distinct) <= 2 and len({p[0] for p in picks}) == 1

    verdict = "POSITIVE" if (crit1 and crit2 and crit3) else "NEGATIVE"

    st = os.path.join(resdir, "summary.txt")
    with open(st, "w") as f:
        f.write("BTC-anchored regime gate — walk-forward verdict\n")
        f.write("=" * 64 + "\n")
        f.write(f"{'fold':<6}{'pick(X,Y,Z)':<16}{'gate_OOS':>12}{'baseline_OOS':>14}\n")
        for r in rows:
            pk = f"({r['pick_X']},{r['pick_Y']},{r['pick_Z']})"
            f.write(f"{r['test_year']:<6}{pk:<16}{r['gate_oos_pnl']:>12.0f}{r['baseline_oos_pnl']:>14.0f}\n")
        f.write("-" * 64 + "\n")
        f.write(f"OOS gate total:     {gate_total:>12.0f}\n")
        f.write(f"OOS baseline total: {base_total:>12.0f}\n")
        f.write(f"OOS gate per-year:  {[round(g) for g in gate_years]}\n")
        f.write(f"picks:              {picks}\n\n")
        f.write(f"Crit 1 (beat baseline OOS):        {'PASS' if crit1 else 'FAIL'}\n")
        f.write(f"Crit 2 (no consecutive losing yr): {'PASS' if crit2 else 'FAIL'}\n")
        f.write(f"Crit 3 (parameter stability):      {'PASS' if crit3 else 'FAIL'}  (distinct picks: {len(distinct)})\n")
        f.write("=" * 64 + "\n")
        f.write(f">>> VERDICT: {verdict}\n")
    print(open(st).read())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
