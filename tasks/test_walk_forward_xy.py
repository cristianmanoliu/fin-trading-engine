# tasks/test_walk_forward_xy.py
from walk_forward_xy import no_consecutive_losing, crit3_stable

def test_no_consecutive_losing_true():
    assert no_consecutive_losing([100, -50, 200, -10]) is True

def test_no_consecutive_losing_false():
    assert no_consecutive_losing([100, -50, -10, 200]) is False

def test_crit3_stable_same_x_two_picks():
    # 2 distinct picks, same X=5 → stable.
    assert crit3_stable([(5, 7), (5, 7), (5, 14), (5, 14)]) is True

def test_crit3_unstable_different_x():
    # same count of distinct but X varies → unstable.
    assert crit3_stable([(5, 7), (10, 7), (5, 7), (5, 7)]) is False

def test_crit3_unstable_three_picks():
    assert crit3_stable([(5, 7), (5, 14), (5, 30), (5, 7)]) is False
