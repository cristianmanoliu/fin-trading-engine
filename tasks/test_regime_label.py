# tasks/test_regime_label.py
import unittest
from regime_label import label_timeline

def mk(closes):
    # synthetic: date index d0001.. , given closes
    return [(f"2020-01-{i+1:02d}", c) for i, c in enumerate(closes)]

class TestRegimeLabel(unittest.TestCase):
    def test_flat_when_insufficient_history(self):
        # y=z=2: first 2 days cannot fire either trigger
        rows = mk([100.0, 101.0, 102.0])
        out = label_timeline(rows, x_pct=5, y_days=2, z_days=2)
        self.assertEqual(out[0][1], "FLAT")
        self.assertEqual(out[1][1], "FLAT")

    def test_short_on_drop(self):
        # day2 close 90 vs day0 close 100 over y=2 → -10% ≤ -5% → SHORT
        rows = mk([100.0, 95.0, 90.0])
        out = label_timeline(rows, x_pct=5, y_days=2, z_days=2)
        self.assertEqual(out[2][1], "SHORT")

    def test_long_on_rise(self):
        rows = mk([100.0, 105.0, 112.0])  # day2 vs day0 = +12% ≥ +5% → LONG
        out = label_timeline(rows, x_pct=5, y_days=2, z_days=2)
        self.assertEqual(out[2][1], "LONG")

    def test_flat_in_between(self):
        rows = mk([100.0, 100.5, 101.0])  # +1% over 2d, neither trigger
        out = label_timeline(rows, x_pct=5, y_days=2, z_days=2)
        self.assertEqual(out[2][1], "FLAT")

    def test_tie_break_short_wins(self):
        # asymmetric windows: down over short window, up over long window.
        # day idx3 close=130: over z=3 (vs idx0=100) = +30% ≥5% LONG;
        #                     over y=1 (vs idx2=140) = -7.1% ≤-5% SHORT.
        rows = mk([100.0, 110.0, 140.0, 130.0])
        out = label_timeline(rows, x_pct=5, y_days=1, z_days=3)
        self.assertEqual(out[3][1], "SHORT")  # tie-break: SHORT wins

    def test_causality_no_lookahead(self):
        # labeling must not depend on future rows: truncating after day i
        # yields the same label for day i.
        rows = mk([100.0, 95.0, 90.0, 120.0, 50.0])
        full = label_timeline(rows, x_pct=5, y_days=2, z_days=2)
        trunc = label_timeline(rows[:3], x_pct=5, y_days=2, z_days=2)
        self.assertEqual(full[2][1], trunc[2][1])

if __name__ == "__main__":
    unittest.main()
