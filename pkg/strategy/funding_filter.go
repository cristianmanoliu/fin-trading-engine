package strategy

import (
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// FundingRateReader returns the prevailing per-8h funding rate at a given time.
// Implemented by funding.Historical. The strategy package depends on the abstraction
// rather than the concrete type to avoid a strategy → funding compile dependency
// when the filter is unused.
type FundingRateReader interface {
	RateAt(t time.Time) float64
}

// FundingFilter gates SHORT entry signals by current funding-regime.
//
// Mechanism rationale: per-quarter analysis (2026-05-06) showed strategy NET is
// negatively correlated with funding rate (Pearson r=-0.175, t=-6.03, p<0.0001
// over 1153 cells). The 4H short P4 strategy loses money in BULL regimes (positive
// funding) and gains in BEAR/neutral (negative funding). Filtering shorts when
// funding is above a threshold is intended to avoid the worst regime.
//
// Threshold semantics: MaxBpsPerDay is in basis points per DAY (3 funding events,
// each 8h). A reading of e.g. +0.000298 per-8h is +8.94 bps/day. To exclude only
// extreme bull regimes, use ~5 bps/day. To exclude all positive funding, use ~0.
//
// Filter is short-only by design — long-side filters would require a different
// threshold direction. Use nil receiver or zero-valued field to disable.
type FundingFilter struct {
	Reader       FundingRateReader
	MaxBpsPerDay float64 // skip Short signals when current_8h_rate × 3 × 10000 > MaxBpsPerDay
}

// Allows reports whether a signal of the given side at the given time passes
// the filter. Returns true when the filter is unconfigured or doesn't apply.
func (f *FundingFilter) Allows(side models.Direction, t time.Time) bool {
	if f == nil || f.Reader == nil || f.MaxBpsPerDay <= 0 {
		return true
	}
	if side != models.Short {
		return true
	}
	rate8h := f.Reader.RateAt(t)
	dailyBps := rate8h * 3 * 10000
	return dailyBps <= f.MaxBpsPerDay
}
