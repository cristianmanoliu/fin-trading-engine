#!/usr/bin/env python3
# tasks/walk_forward_xy.py — Z-free expanding-window walk-forward for the
# regime-SWITCH test. Reads episodes_X<X>_Y<Y>[_baseline].csv from a resdir,
# fits (X,Y) on early years, scores the next unseen year, applies criteria.
# Crit2 = no consecutive losing OOS years (PRIMARY). Crit3 = (X,Y) stability.
# Crit1 = beat-baseline-total (computed, reported-not-gated per spec §6).
#
# Usage: python3 tasks/walk_forward_xy.py --resdir <dir>
import argparse
import csv
import os

GRID = [(x, y) for x in (5, 10, 15, 20) for y in (7, 14, 30)]
FOLDS = [
    (["2020", "2021", "2022"], "2023"),
    (["2020", "2021", "2022", "2023"], "2024"),
    (["2020", "2021", "2022", "2023", "2024"], "2025"),
    (["2020", "2021", "2022", "2023", "2024", "2025"], "2026"),
]


def _path(resdir, x, y, baseline):
    suf = "_baseline" if baseline else ""
    return os.path.join(resdir, f"episodes_X{x}_Y{y}{suf}.csv")


def annual_pnls(resdir, x, y, baseline=False):
    out = {}
    p = _path(resdir, x, y, baseline)
    if not os.path.exists(p):
        return out
    with open(p, newline="") as f:
        for rec in csv.DictReader(f):
            out[rec["year"]] = out.get(rec["year"], 0.0) + float(rec["net_pnl"])
    return out


def fit_best(annual_fn, fit_years, grid):
    best, best_sum = None, None
    for (x, y) in grid:
        s = sum(annual_fn(x, y).get(yr, 0.0) for yr in fit_years)
        if best_sum is None or s > best_sum:
            best_sum, best = s, (x, y)
    return best


def no_consecutive_losing(year_pnls):
    for i in range(1, len(year_pnls)):
        if year_pnls[i] < 0 and year_pnls[i - 1] < 0:
            return False
    return True


def crit3_stable(picks):
    distinct = set(picks)
    return len(distinct) <= 2 and len({p[0] for p in picks}) == 1


def evaluate(resdir):
    annual_fn = lambda x, y: annual_pnls(resdir, x, y, baseline=False)
    # Guard: an empty/misconfigured resdir yields all-zero sums → fit picks (5,7),
    # gate_years=[0,0,0,0], and no_consecutive_losing([0,0,0,0]) is True → a SPURIOUS
    # crit2 PASS with no data. If NO episodes_* CSV exists at all, fail loud.
    if not any(os.path.exists(_path(resdir, x, y, b))
               for (x, y) in GRID for b in (False, True)):
        raise SystemExit(f"walk_forward_xy: no episodes_* CSVs in {resdir} — nothing to evaluate")
    picks, oos = [], []
    for fit_years, test_year in FOLDS:
        x, y = fit_best(annual_fn, fit_years, GRID)
        picks.append((x, y))
        g = annual_pnls(resdir, x, y, baseline=False).get(test_year, 0.0)
        b = annual_pnls(resdir, x, y, baseline=True).get(test_year, 0.0)
        oos.append((test_year, g, b))
    gate_years = [g for _, g, _ in oos]
    base_years = [b for _, _, b in oos]
    return {
        "picks": picks,
        "oos": oos,
        "gate_total": sum(gate_years),
        "base_total": sum(base_years),
        "gate_years": gate_years,
        "base_years": base_years,
        # $1 margin, NOT raw `>` — degenerate ties differ only by FP summation
        # noise (~1e-11) and would false-POSITIVE (alt10-30-504 bug, commit 590ef03).
        "crit1": (sum(gate_years) - sum(base_years)) >= 1.0,
        "crit2_switch": no_consecutive_losing(gate_years),
        "crit2_baseline": no_consecutive_losing(base_years),
        "crit3": crit3_stable(picks),
    }


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--resdir", required=True)
    args = ap.parse_args(argv)
    r = evaluate(args.resdir)
    st = os.path.join(args.resdir, "summary.txt")
    # summary.txt only (no walk_forward.csv) — the panel consolidator parses the
    # terminal IN_F/FLIPPED/REGRESSED/CRIT3 line from summary.txt.
    with open(st, "w") as f:
        f.write("BTC regime-SWITCH (full-stop) — walk-forward verdict\n")
        f.write("=" * 64 + "\n")
        f.write(f"{'fold':<6}{'pick(X,Y)':<12}{'switch_OOS':>12}{'baseline_OOS':>14}\n")
        for (ty, g, b), pk in zip(r["oos"], r["picks"]):
            f.write(f"{ty:<6}{str(pk):<12}{g:>12.0f}{b:>14.0f}\n")
        f.write("-" * 64 + "\n")
        f.write(f"OOS switch total:   {r['gate_total']:>12.0f}\n")
        f.write(f"OOS baseline total: {r['base_total']:>12.0f}\n")
        f.write(f"switch per-year:    {[round(g) for g in r['gate_years']]}\n")
        f.write(f"baseline per-year:  {[round(b) for b in r['base_years']]}\n")
        f.write(f"picks:              {r['picks']}\n\n")
        f.write(f"Crit 1 (beat baseline, REPORTED): {'YES' if r['crit1'] else 'NO'}\n")
        f.write(f"Crit 2 SWITCH   (no consec loss): {'PASS' if r['crit2_switch'] else 'FAIL'}\n")
        f.write(f"Crit 2 BASELINE (no consec loss): {'PASS' if r['crit2_baseline'] else 'FAIL'}\n")
        f.write(f"Crit 3 (X,Y stability):           {'PASS' if r['crit3'] else 'FAIL'}\n")
        f.write("=" * 64 + "\n")
        # in-set-F = baseline FAILS crit2; flipped = switch PASS while baseline FAIL
        in_F = not r["crit2_baseline"]
        flipped = in_F and r["crit2_switch"]
        regressed = r["crit2_baseline"] and not r["crit2_switch"]
        f.write(f">>> IN_F: {in_F}  FLIPPED: {flipped}  REGRESSED: {regressed}  CRIT3: {'PASS' if r['crit3'] else 'FAIL'}\n")
    print(open(st).read())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
