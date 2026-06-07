# tasks/test_walk_forward.py
import unittest
from walk_forward import no_consecutive_losing, fit_best

class TestWalkForward(unittest.TestCase):
    def test_no_consecutive_losing_true(self):
        self.assertTrue(no_consecutive_losing([10, -5, 20, -3]))

    def test_no_consecutive_losing_false(self):
        self.assertFalse(no_consecutive_losing([10, -5, -3, 20]))

    def test_no_consecutive_losing_edges(self):
        self.assertTrue(no_consecutive_losing([]))
        self.assertTrue(no_consecutive_losing([-1]))
        self.assertFalse(no_consecutive_losing([-1, -1]))

    def test_fit_best_picks_max(self):
        grid = [(5, 7, 7), (10, 14, 14)]
        fit_years = ["2020", "2021"]
        def stub_annual(x, y, z):
            return {"2020": 100.0, "2021": 100.0} if (x, y, z) == (10, 14, 14) else {"2020": 1.0, "2021": 1.0}
        best = fit_best(stub_annual, fit_years, grid)
        self.assertEqual(best, (10, 14, 14))

if __name__ == "__main__":
    unittest.main()
