package strategy

import (
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// FundingFilter is currently dormant in production (--funding-bps-per-day 0
// disables it via the MaxBpsPerDay <= 0 guard) but lives in code as a
// reactivation candidate if a future Cat F2 mechanism-grounded analysis
// finds the funding-regime correlation real (the original r=-0.175 was
// computed BEFORE the funding-loader bug fix and needs re-validation).
//
// These tests pin Allows() behavior across the full decision tree so that
// when (if) it's reactivated, it works correctly:
//   - nil receiver → unconditional pass (defensive)
//   - nil Reader → unconditional pass
//   - MaxBpsPerDay <= 0 → unconditional pass (the dormant config)
//   - non-Short side → unconditional pass (filter is short-only by design)
//   - Short side, daily bps ≤ threshold → pass
//   - Short side, daily bps > threshold → block
//   - Boundary at exactly threshold → pass (≤ comparison, not strict <)
//
// fakeFundingReader implements FundingRateReader for tests by returning a
// fixed per-8h rate independent of the input timestamp.
type fakeFundingReader struct {
	rate8h float64
}

func (f fakeFundingReader) RateAt(_ time.Time) float64 { return f.rate8h }

func TestFundingFilter_NilReceiver_AlwaysAllows(t *testing.T) {
	// A *FundingFilter that's nil must not panic and must always allow.
	// This is the path Runner takes when no filter is configured.
	var f *FundingFilter
	if !f.Allows(models.Short, time.Now()) {
		t.Error("nil FundingFilter blocked Short — defensive nil-receiver path broken")
	}
	if !f.Allows(models.Long, time.Now()) {
		t.Error("nil FundingFilter blocked Long")
	}
}

func TestFundingFilter_NilReader_AllowsAll(t *testing.T) {
	// FundingFilter with no Reader is unusable; must fall back to allow-all.
	f := &FundingFilter{Reader: nil, MaxBpsPerDay: 5}
	if !f.Allows(models.Short, time.Now()) {
		t.Error("FundingFilter with nil Reader blocked Short — should fall back to allow")
	}
}

func TestFundingFilter_DisabledThreshold_AllowsAll(t *testing.T) {
	// MaxBpsPerDay <= 0 is the canonical "filter disabled" state — this is
	// the live production config (--funding-bps-per-day 0). Must allow all
	// regardless of funding rate.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: 0.001}, // = +30 bps/day, very bullish
		MaxBpsPerDay: 0,                                // disabled
	}
	if !f.Allows(models.Short, time.Now()) {
		t.Error("disabled filter (MaxBpsPerDay=0) blocked Short — disable semantic broken")
	}

	// Negative threshold is also "disabled" via the <=0 guard.
	f.MaxBpsPerDay = -5
	if !f.Allows(models.Short, time.Now()) {
		t.Error("negative threshold should disable filter — blocked Short")
	}
}

func TestFundingFilter_NonShortSide_AlwaysAllows(t *testing.T) {
	// Filter is short-only by design (the documented rationale: positive
	// funding hurts shorts; long-side filtering would need different
	// threshold direction). Long and Neutral signals MUST pass through.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: 0.001}, // +30 bps/day, would block shorts
		MaxBpsPerDay: 5,
	}
	if !f.Allows(models.Long, time.Now()) {
		t.Error("Long signal blocked despite filter being short-only")
	}
	if !f.Allows(models.Neutral, time.Now()) {
		t.Error("Neutral signal blocked despite filter being short-only")
	}
}

func TestFundingFilter_ShortBelowThreshold_Allows(t *testing.T) {
	// Funding rate × 3 × 10000 ≤ MaxBpsPerDay → pass.
	// rate=0.0001 per-8h → 0.0001 × 3 × 10000 = 3 bps/day.
	// Threshold 5 → 3 ≤ 5 → allow.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: 0.0001},
		MaxBpsPerDay: 5,
	}
	if !f.Allows(models.Short, time.Now()) {
		t.Error("Short with 3 bps/day funding blocked at 5 bps threshold — should pass (3 ≤ 5)")
	}
}

func TestFundingFilter_ShortAboveThreshold_Blocks(t *testing.T) {
	// rate=0.001 per-8h → 30 bps/day, threshold 5 → 30 > 5 → block.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: 0.001},
		MaxBpsPerDay: 5,
	}
	if f.Allows(models.Short, time.Now()) {
		t.Error("Short with 30 bps/day funding allowed at 5 bps threshold — should block")
	}
}

func TestFundingFilter_ShortAtBoundary_Allows(t *testing.T) {
	// Comparison is ≤ (not strict <). At exactly threshold, allow.
	// rate per-8h that lands exactly at 5 bps/day: 5 / 3 / 10000 ≈ 0.0001667.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: 5.0 / 3.0 / 10000.0},
		MaxBpsPerDay: 5,
	}
	if !f.Allows(models.Short, time.Now()) {
		t.Error("Short at exactly threshold (5 bps/day = 5 bps/day): blocked, should allow (≤ not strict <)")
	}
}

func TestFundingFilter_NegativeFundingRate_AlwaysAllowsShort(t *testing.T) {
	// Negative funding (BEAR regime) is the favorable case for shorts —
	// definitely below any positive threshold, so allow.
	f := &FundingFilter{
		Reader:       fakeFundingReader{rate8h: -0.001}, // -30 bps/day, very bearish
		MaxBpsPerDay: 5,
	}
	if !f.Allows(models.Short, time.Now()) {
		t.Error("Short with negative (favorable) funding blocked — only positive funding should filter shorts")
	}
}
