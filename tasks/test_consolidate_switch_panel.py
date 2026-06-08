# tasks/test_consolidate_switch_panel.py
from consolidate_switch_panel import panel_verdict

def row(cohort, in_F, flipped, regressed, crit3):
    return {"cohort": cohort, "in_F": in_F, "flipped": flipped,
            "regressed": regressed, "crit3": crit3}

def test_majority_of_F_flips_pass():
    rows = [
        row("a", True, True, False, True),
        row("b", True, True, False, True),
        row("c", True, False, False, True),   # in F, didn't flip
        row("d", False, False, False, True),  # baseline-clean
    ]
    v = panel_verdict(rows)
    assert v["F_size"] == 3
    assert v["flipped"] == 2
    assert v["verdict"] == "PASS"   # 2/3 > 1.5 strict majority

def test_minority_flips_fail():
    rows = [row("a", True, True, False, True),
            row("b", True, False, False, True),
            row("c", True, False, False, True)]
    v = panel_verdict(rows)
    assert v["verdict"] == "FAIL"   # 1/3 not majority

def test_regression_forces_fail():
    rows = [row("a", True, True, False, True),
            row("b", True, True, False, True),
            row("c", False, False, True, True)]  # baseline-clean REGRESSED
    v = panel_verdict(rows)
    assert v["regressions"] == ["c"]
    assert v["verdict"] == "FAIL"   # any regression = red flag = fail

def test_empty_F_voids():
    rows = [row("a", False, False, False, True),
            row("b", False, False, False, True)]
    v = panel_verdict(rows)
    assert v["verdict"] == "VOID"   # no problem to solve

def test_flip_without_crit3_does_not_count():
    rows = [row("a", True, True, False, False),  # flipped but UNSTABLE
            row("b", True, True, False, True),
            row("c", True, False, False, True)]
    v = panel_verdict(rows)
    assert v["flipped"] == 1   # 'a' excluded (crit3 fail)
    assert v["verdict"] == "FAIL"  # 1/3 not majority
