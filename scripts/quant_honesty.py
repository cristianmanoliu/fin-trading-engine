#!/usr/bin/env python3
"""quant_honesty.py — the screening + honesty battery this project cost 3.5 months to learn.

Portable. No project imports, stdlib + numpy only. Copy this one file into any
future quant project; nothing here is crypto- or perp-specific.

Two functions, in the order you should call them:

    screen(gross_r, fee_bps, slip_bps, stop_pct)  -- BEFORE writing any code
    honesty(trade_returns, by_year=...)           -- BEFORE believing any backtest

WHY THIS FILE EXISTS
--------------------
17 study scripts in this repo each reimplemented the same honesty block, with
drifting definitions (truncated vs rounded 5% index; one silently rescaled by
100x; several crash or return NaN below n=20). The checks themselves killed
four candidates that passed on mean and t-statistic -- #11 settlement drift,
#12 listing drift, #20 MVRV, and C4 -- so they are the most load-bearing code
the project produced and deserve one definition.

THE TWO LESSONS, IN ONE PARAGRAPH EACH
--------------------------------------
1. COST IS DETERMINISTIC, GROSS IS A RANDOM VARIABLE. Risk-based sizing forces
   leverage L = 1/stop_pct, fees are charged on notional (twice), so cost per
   trade in R = (fee_rt + slip) * L * 1e-4. That number is known to four
   decimals before you start. Gross edge arrives with a confidence interval
   often 15x wider than the cost it must beat. Backtest gross historically
   arrives at ~1/10 of its modeled value; cost arrives at 1.0x. Hence the 3x
   requirement in screen().

2. AGGREGATE P&L HIDES TAIL DEPENDENCE. Mean, total net, by-year positivity and
   Sharpe all survive a book whose entire edge is 5% of trades. Median and
   drop-top-5% do not. Every candidate this project killed late was killed by
   drop-top-5% after passing everything else.

Self-check: `python3 quant_honesty.py --selftest`
"""
import sys
import numpy as np

__all__ = ["screen", "honesty", "format_report"]


def screen(gross_r, fee_bps, slip_bps, stop_pct, mult=3.0):
    """The 30-second viability screen. Run BEFORE writing a line of code.

    gross_r  : expected gross edge per trade, in R (risk multiples)
    fee_bps  : round-trip fee in bp (both sides summed)
    slip_bps : expected slippage in bp
    stop_pct : typical stop distance as a FRACTION (0.018 = 1.8%), not percent
    mult     : required gross/cost ratio. 3.0 is the calibrated default --
               backtest gross arrives at ~1/10 of model, cost at 1.0x.

    Returns dict with `pass` (bool) and the arithmetic behind it.

    A strategy that fails this needs no backtest. This project ran 85 trials
    before writing this function; applied retroactively it rejects every one.
    """
    if stop_pct <= 0:
        raise ValueError("stop_pct must be > 0 and expressed as a fraction (0.018, not 1.8)")
    if stop_pct > 1:
        raise ValueError(f"stop_pct={stop_pct} > 1 -- pass a fraction (0.018), not a percent (1.8)")
    leverage = 1.0 / stop_pct
    cost_r = (fee_bps + slip_bps) * leverage * 1e-4
    required = mult * cost_r
    return {
        "leverage": leverage,
        "cost_r": cost_r,
        "gross_r": gross_r,
        "required_r": required,
        "ratio": (gross_r / cost_r) if cost_r > 0 else float("inf"),
        "pass": gross_r >= required,
    }


def honesty(returns, by_year=None, min_n=60):
    """The honesty battery. Run on per-trade returns BEFORE believing a backtest.

    returns  : 1-D array of per-trade returns (any consistent unit -- bp, R, $)
    by_year  : optional {year: [returns]} for era-robustness
    min_n    : power floor. Below this the result is UNRESOLVABLE, not negative.

    Returns dict. The three that matter, in order of how often they kill:
        drop5   -- mean after removing the top 5% of trades. THE decisive check.
        median  -- a positive mean with a negative median is a tail artifact.
        pos_yrs -- an edge living in one era is a regime bet, not an edge.

    Note `underpowered`: below min_n the honest verdict is "cannot tell", which
    is different from "no edge". Reporting an underpowered negative as a
    falsification is its own error.
    """
    a = np.asarray(returns, dtype=float)
    a = a[np.isfinite(a)]
    n = len(a)
    if n == 0:
        return {"n": 0, "underpowered": True, "mean": float("nan"),
                "median": float("nan"), "win": float("nan"), "drop5": float("nan"),
                "pos_yrs": 0, "n_yrs": 0, "tail_carried": False}

    desc = np.sort(a)[::-1]
    # rounded, not truncated: at n=378 truncation drops 18 and rounding 19.
    # Rounding is the honest reading of "top 5%"; truncation flatters the book.
    k = max(1, int(round(0.05 * n)))
    drop5 = float(np.mean(desc[k:])) if n > k else float("nan")
    mean = float(np.mean(a))

    pos_yrs = n_yrs = 0
    if by_year:
        yrs = [y for y, v in by_year.items() if len(v)]
        n_yrs = len(yrs)
        pos_yrs = sum(1 for y in yrs if np.mean(by_year[y]) > 0)

    return {
        "n": n,
        "underpowered": n < min_n,
        "mean": mean,
        "median": float(np.median(a)),
        "win": float(np.mean(a > 0) * 100),
        "drop5": drop5,
        "pos_yrs": pos_yrs,
        "n_yrs": n_yrs,
        # the signature failure: positive on aggregate, negative without its tail
        "tail_carried": bool(mean > 0 and np.isfinite(drop5) and drop5 <= 0),
    }


def format_report(h, unit="bp"):
    """One-screen human summary of an honesty() result."""
    if h["n"] == 0:
        return "  n=0 -- no trades"
    out = [
        f"  n={h['n']}{'  [UNDERPOWERED]' if h['underpowered'] else ''}",
        f"  mean={h['mean']:+.2f}{unit}  median={h['median']:+.2f}{unit}  win={h['win']:.0f}%",
        f"  drop-top-5%={h['drop5']:+.2f}{unit}",
    ]
    if h["n_yrs"]:
        out.append(f"  by-year positive {h['pos_yrs']}/{h['n_yrs']}")
    if h["tail_carried"]:
        out.append("  *** TAIL-CARRIED: positive mean, non-positive without top 5% ***")
    return "\n".join(out)


def _selftest():
    # screen: the live config this project actually ran (and lost on)
    s = screen(gross_r=0.0296, fee_bps=10, slip_bps=5, stop_pct=0.015)
    assert not s["pass"], "the live config must FAIL the screen -- it lost money"
    assert abs(s["leverage"] - 66.67) < 0.1, s["leverage"]

    # a strategy clearing the bar: wide stop -> low leverage -> low cost
    w = screen(gross_r=0.30, fee_bps=10, slip_bps=5, stop_pct=0.05)
    assert w["pass"], w

    # percent-vs-fraction is the easy way to be wrong by 100x
    try:
        screen(0.1, 10, 5, stop_pct=1.8)
    except ValueError:
        pass
    else:
        raise AssertionError("stop_pct > 1 must raise, not silently compute")

    # honesty: the signature tail-mirage -- great mean, edge is one trade
    h = honesty(np.array([500.0] + [-1.0] * 99))
    assert h["mean"] > 0 and h["drop5"] < 0 and h["tail_carried"], h

    # a genuinely broad edge must NOT be flagged
    g = honesty(np.array([2.0] * 100))
    assert g["drop5"] > 0 and not g["tail_carried"], g

    # rounding, not truncation
    assert max(1, int(round(0.05 * 378))) == 19

    # power floor is a distinct state from "no edge"
    assert honesty(np.array([1.0, 2.0]), min_n=60)["underpowered"]
    assert not honesty(np.ones(60), min_n=60)["underpowered"]

    # empty input must not raise
    assert honesty(np.array([]))["n"] == 0

    # by-year
    b = honesty(np.array([1.0, -1.0, 3.0]), by_year={2024: [1.0, -1.0], 2025: [3.0]})
    assert b["pos_yrs"] == 1 and b["n_yrs"] == 2, b

    print("selftest OK")


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
        sys.exit(0)
    print(__doc__)
    print("Run --selftest to verify. Import screen() and honesty() from your study.")
