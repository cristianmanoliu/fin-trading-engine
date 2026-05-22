package indicators

import (
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

func TestVWAPInitialState(t *testing.T) {
	v := &VWAP{}
	if got := v.Value(); got != 0 {
		t.Errorf("expected 0 before any ticks, got %v", got)
	}
}

func TestVWAPSingleSessionMath(t *testing.T) {
	// Three ticks at the same UTC day. VWAP = Σ(price×vol) / Σ(vol).
	v := &VWAP{}
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: day, Price: 100.0, Volume: 2.0})
	v.Update(models.Tick{Timestamp: day.Add(time.Minute), Price: 110.0, Volume: 3.0})
	v.Update(models.Tick{Timestamp: day.Add(2 * time.Minute), Price: 105.0, Volume: 5.0})

	// Σ(p·v) = 200 + 330 + 525 = 1055; Σ(v) = 10; VWAP = 105.5
	got := v.Value()
	want := 105.5
	if got != want {
		t.Errorf("VWAP: got %v want %v", got, want)
	}
}

func TestVWAPSessionResetAtMidnightUTC(t *testing.T) {
	v := &VWAP{}

	// Day 1: two ticks → VWAP forms.
	day1 := time.Date(2026, 5, 6, 23, 30, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: day1, Price: 100.0, Volume: 1.0})
	v.Update(models.Tick{Timestamp: day1.Add(15 * time.Minute), Price: 200.0, Volume: 1.0})
	if got := v.Value(); got != 150.0 {
		t.Fatalf("day-1 VWAP: got %v want 150.0", got)
	}

	// Cross midnight UTC → session resets.
	day2 := time.Date(2026, 5, 7, 0, 5, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: day2, Price: 50.0, Volume: 2.0})
	// After reset, only the day-2 tick contributes: VWAP = 50.0 (not blended with day 1).
	if got := v.Value(); got != 50.0 {
		t.Errorf("post-reset VWAP: got %v want 50.0 (day 1 ticks should be discarded)", got)
	}
}

func TestVWAPDoesNotResetWithinSameUTCDay(t *testing.T) {
	v := &VWAP{}
	// Two ticks within the same UTC day (one near start, one near end).
	t1 := time.Date(2026, 5, 6, 0, 1, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 6, 23, 59, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: t1, Price: 100.0, Volume: 1.0})
	v.Update(models.Tick{Timestamp: t2, Price: 200.0, Volume: 1.0})
	if got := v.Value(); got != 150.0 {
		t.Errorf("same-day VWAP: got %v want 150.0 (no reset within UTC day)", got)
	}
}

func TestVWAPHandlesYearBoundary(t *testing.T) {
	// julianDay uses year×1000 + yearDay so Dec 31 and Jan 1 must compare different.
	v := &VWAP{}
	dec31 := time.Date(2025, 12, 31, 23, 30, 0, 0, time.UTC)
	jan01 := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: dec31, Price: 100.0, Volume: 1.0})
	v.Update(models.Tick{Timestamp: jan01, Price: 50.0, Volume: 1.0})
	if got := v.Value(); got != 50.0 {
		t.Errorf("year-boundary reset: got %v want 50.0 (day rolls Dec 31 → Jan 1)", got)
	}
}

func TestVWAPZeroVolumeTickDoesNotMoveAverage(t *testing.T) {
	v := &VWAP{}
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	v.Update(models.Tick{Timestamp: day, Price: 100.0, Volume: 1.0})
	v.Update(models.Tick{Timestamp: day, Price: 999.0, Volume: 0.0}) // zero vol — should be no-op
	if got := v.Value(); got != 100.0 {
		t.Errorf("zero-vol tick: got %v want 100.0", got)
	}
}
