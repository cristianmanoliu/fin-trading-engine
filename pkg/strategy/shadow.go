package strategy

import (
	"fmt"
	"strconv"
	"strings"
)

// ShadowSpec describes one shadow strategy variant to run alongside the live
// configuration. Shadow strategies receive the same tick stream and candles
// but produce their own (hypothetical) trades to a separate journal path.
//
// Wire format: "label:ema_fast-ema_slow-max_hold_hours". Multiple specs
// comma-separated. Example: "alt1:5-15-336,alt2:5-15-504".
//
// Use cases:
//   - Test parameter variants without changing live deploy
//   - Compare candidate configs on identical forward market data
//   - Eliminate retroactive overfitting via pre-registered alternatives
type ShadowSpec struct {
	Label         string  // used for journal subdirectory: shadow/<Label>/
	EMAFastPeriod int     // 0 → backtest defaults to 9
	EMASlowPeriod int     // 0 → backtest defaults to 21
	MaxHoldHours  float64 // 0 → no cap
}

// ParseShadowSpecs parses a comma-separated list of shadow specs.
// Returns nil + nil error for empty input (shadow mode disabled).
//
// Spec format: "label:ema_fast-ema_slow-max_hold_hours"
//   label: alphanumeric+underscore identifier for journal directory
//   ema_fast, ema_slow: integer EMA periods (positive)
//   max_hold_hours: float (0 = no cap)
//
// Returns descriptive error on any malformed spec; the caller should fail-fast
// rather than silently dropping shadows.
func ParseShadowSpecs(s string) ([]ShadowSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	specs := make([]ShadowSpec, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		labelAndRest := strings.SplitN(p, ":", 2)
		if len(labelAndRest) != 2 {
			return nil, fmt.Errorf("shadow spec %d: expected 'label:ema_fast-ema_slow-max_hold', got %q", i, p)
		}
		label := strings.TrimSpace(labelAndRest[0])
		params := strings.Split(strings.TrimSpace(labelAndRest[1]), "-")
		if len(params) != 3 {
			return nil, fmt.Errorf("shadow spec %d (%s): expected 3 dash-separated params (ema_fast-ema_slow-max_hold), got %d", i, label, len(params))
		}
		fast, err := strconv.Atoi(params[0])
		if err != nil || fast <= 0 {
			return nil, fmt.Errorf("shadow spec %d (%s): ema_fast must be positive integer, got %q", i, label, params[0])
		}
		slow, err := strconv.Atoi(params[1])
		if err != nil || slow <= 0 {
			return nil, fmt.Errorf("shadow spec %d (%s): ema_slow must be positive integer, got %q", i, label, params[1])
		}
		mh, err := strconv.ParseFloat(params[2], 64)
		if err != nil || mh < 0 {
			return nil, fmt.Errorf("shadow spec %d (%s): max_hold must be non-negative float, got %q", i, label, params[2])
		}
		specs = append(specs, ShadowSpec{
			Label:         label,
			EMAFastPeriod: fast,
			EMASlowPeriod: slow,
			MaxHoldHours:  mh,
		})
	}
	return specs, nil
}
