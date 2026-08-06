package indicators

import (
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

func TestDailyLevelsInitialState(t *testing.T) {
	d := &DailyLevels{}
	if d.HasData() {
		t.Errorf("HasData: expected false before any ticks")
	}
	if levels := d.KeyLevels(nil); levels != nil {
		t.Errorf("KeyLevels: expected nil before PDH/PDL set, got %v", levels)
	}
}

func TestDailyLevelsTracksCurrentDayHighLow(t *testing.T) {
	// Within a single UTC day, no PDH/PDL until first day-roll fires.
	d := &DailyLevels{}
	day := time.Date(2026, 5, 6, 1, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day, Price: 100.0})
	d.Update(models.Tick{Timestamp: day.Add(time.Hour), Price: 150.0})    // new high
	d.Update(models.Tick{Timestamp: day.Add(2 * time.Hour), Price: 50.0}) // new low
	d.Update(models.Tick{Timestamp: day.Add(3 * time.Hour), Price: 110.0})

	if d.HasData() {
		t.Errorf("HasData: expected false (one day not complete)")
	}
	// PDH/PDL still 0 — currentHigh/currentLow tracked internally
}

func TestDailyLevelsRollsAtMidnightUTC(t *testing.T) {
	d := &DailyLevels{}

	// Day 1: build a high/low.
	day1 := time.Date(2026, 5, 6, 0, 30, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day1, Price: 100.0})
	d.Update(models.Tick{Timestamp: day1.Add(6 * time.Hour), Price: 150.0})
	d.Update(models.Tick{Timestamp: day1.Add(12 * time.Hour), Price: 80.0})
	d.Update(models.Tick{Timestamp: day1.Add(18 * time.Hour), Price: 120.0})
	// Day 1 high = 150, low = 80.

	if d.PDH != 0 || d.PDL != 0 {
		t.Errorf("before roll: expected PDH=PDL=0, got PDH=%v PDL=%v", d.PDH, d.PDL)
	}

	// Day 2: first tick triggers the roll.
	day2 := time.Date(2026, 5, 7, 0, 5, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day2, Price: 110.0})

	if d.PDH != 150.0 {
		t.Errorf("after roll: PDH got %v want 150.0", d.PDH)
	}
	if d.PDL != 80.0 {
		t.Errorf("after roll: PDL got %v want 80.0", d.PDL)
	}
	if !d.HasData() {
		t.Errorf("HasData: expected true after first roll")
	}
}

func TestDailyLevelsCurrentDayContinuesAfterRoll(t *testing.T) {
	d := &DailyLevels{}
	day1 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day1, Price: 100.0})
	d.Update(models.Tick{Timestamp: day1.Add(time.Hour), Price: 200.0})

	day2 := time.Date(2026, 5, 7, 1, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day2, Price: 150.0})
	d.Update(models.Tick{Timestamp: day2.Add(time.Hour), Price: 250.0})     // new day-2 high
	d.Update(models.Tick{Timestamp: day2.Add(2 * time.Hour), Price: 130.0}) // new day-2 low

	// PDH/PDL remain day 1 values; day 2 currentHigh/Low track separately.
	if d.PDH != 200.0 || d.PDL != 100.0 {
		t.Errorf("PDH/PDL should still be day-1 (200/100), got PDH=%v PDL=%v", d.PDH, d.PDL)
	}

	// Roll to day 3: PDH/PDL should now reflect day-2 extremes.
	day3 := time.Date(2026, 5, 8, 0, 5, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day3, Price: 200.0})
	if d.PDH != 250.0 {
		t.Errorf("after second roll: PDH got %v want 250.0 (day-2 high)", d.PDH)
	}
	if d.PDL != 130.0 {
		t.Errorf("after second roll: PDL got %v want 130.0 (day-2 low)", d.PDL)
	}
}

func TestDailyLevelsKeyLevelsIncludesZones(t *testing.T) {
	d := &DailyLevels{PDH: 100, PDL: 50}
	zones := []models.Zone{
		{Low: 40, High: 45},
		{Low: 110, High: 115},
	}
	got := d.KeyLevels(zones)
	want := []float64{100, 50, 40, 45, 110, 115}
	if len(got) != len(want) {
		t.Fatalf("KeyLevels: got %d levels, want %d (got %v)", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("KeyLevels[%d]: got %v want %v", i, got[i], v)
		}
	}
}

func TestDailyLevelsIgnoresZeroPDH(t *testing.T) {
	// Pre-roll: PDH = 0. KeyLevels should not include it.
	d := &DailyLevels{PDH: 0, PDL: 0}
	if got := d.KeyLevels(nil); got != nil {
		t.Errorf("zero PDH/PDL: expected nil KeyLevels, got %v", got)
	}
}

func TestDailyLevelsRollsAcrossYearBoundary(t *testing.T) {
	// julianDay encodes year×1000 + yearDay. Dec 31 = 2025365; Jan 1 = 2026001.
	// 2026001 > 2025365 so the day-change branch fires. Regression-guards a
	// future refactor that drops the year multiplier.
	d := &DailyLevels{}
	dec31 := time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: dec31, Price: 100.0})
	d.Update(models.Tick{Timestamp: dec31.Add(6 * time.Hour), Price: 200.0})
	d.Update(models.Tick{Timestamp: dec31.Add(8 * time.Hour), Price: 80.0})

	jan01 := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: jan01, Price: 150.0})

	if d.PDH != 200.0 {
		t.Errorf("PDH after year-boundary roll: got %v want 200.0", d.PDH)
	}
	if d.PDL != 80.0 {
		t.Errorf("PDL after year-boundary roll: got %v want 80.0", d.PDL)
	}
}

func TestDailyLevelsRollsAfterMultiDayGap(t *testing.T) {
	// Tick at Day-1, no ticks for several days, then resume on Day-5.
	// PDH/PDL should reflect Day-1's extremes (the immediately-previous *seen*
	// day), not the new tick price. Regression-guards a future bug that
	// uses tick.Price instead of currentHigh/currentLow on the roll branch.
	d := &DailyLevels{}
	day1 := time.Date(2026, 5, 6, 6, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day1, Price: 100.0})
	d.Update(models.Tick{Timestamp: day1.Add(2 * time.Hour), Price: 175.0}) // day-1 high
	d.Update(models.Tick{Timestamp: day1.Add(4 * time.Hour), Price: 60.0})  // day-1 low

	// Skip Day-2 through Day-4 entirely — first tick on Day-5.
	day5 := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	d.Update(models.Tick{Timestamp: day5, Price: 999.0})

	if d.PDH != 175.0 {
		t.Errorf("PDH after multi-day gap: got %v want 175.0 (day-1 high)", d.PDH)
	}
	if d.PDL != 60.0 {
		t.Errorf("PDL after multi-day gap: got %v want 60.0 (day-1 low)", d.PDL)
	}
}
