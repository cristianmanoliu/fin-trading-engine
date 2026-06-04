import numpy as np
from backtest_overfit_analysis import cscv_pbo, deflated_sharpe, sharpe


def test_sharpe_zero_variance():
    assert sharpe(np.ones(10)) == 0.0


def test_pbo_genuine_best_is_low():
    # One column dominates every period → IS-best is also OOS-best → PBO ≈ 0.
    # slope > 0.5 was removed: when n* is always the same config, slope measures
    # IS/OOS sampling anticorrelation within that single config (regression to mean),
    # which is negative even for a genuinely best strategy.  The meaningful check
    # is oos_loss == 0.0: the dominant config never produces net-negative OOS returns.
    rng = np.random.default_rng(0)
    T, N = 64, 10
    M = rng.normal(0, 1, (T, N))
    M[:, 0] += 5.0  # config 0 genuinely best
    pbo, slope, oos_loss, _, nstar_list = cscv_pbo(M, S=16)
    assert pbo < 0.05
    assert oos_loss == 0.0
    # config 0 dominates → it is IS-best in every fold
    assert all(n == 0 for n in nstar_list)


def test_pbo_pure_noise_is_mid():
    # IID noise, no real edge → IS-best is random OOS → PBO ≈ 0.5.
    rng = np.random.default_rng(1)
    M = rng.normal(0, 1, (64, 20))
    pbo, _, _, _, _ = cscv_pbo(M, S=16)
    assert 0.35 < pbo < 0.65


def test_dsr_monotonic_decreasing_in_N():
    # More trials → lower deflated Sharpe.
    rng = np.random.default_rng(2)
    r = rng.normal(0.1, 1.0, 64)
    all_sr = rng.normal(0.0, 0.3, 34)
    dsr_small, *_ = deflated_sharpe(r, all_sr, N=34)
    dsr_big, *_ = deflated_sharpe(r, all_sr, N=102)
    assert dsr_big <= dsr_small


def test_psr_geq_dsr():
    # PSR (benchmark 0) is always >= DSR (benchmark SR0>=0).
    rng = np.random.default_rng(3)
    r = rng.normal(0.1, 1.0, 64)
    all_sr = rng.normal(0.0, 0.3, 34)
    dsr, psr, *_ = deflated_sharpe(r, all_sr, N=34)
    assert psr >= dsr
