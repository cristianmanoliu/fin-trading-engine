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
// Two wire formats supported:
//   - EMA shadow (legacy):  "label:ema_fast-ema_slow-max_hold_hours"
//   - BB shadow (Cat A):    "label:bb:bb_period-bb_std-max_hold_hours"
//
// Multiple specs comma-separated. Example:
//
//	"alt5-15-504:5-15-504,bb20:bb:20-2.0-504"
//
// Use cases:
//   - Test parameter variants without changing live deploy
//   - Test ENTIRELY DIFFERENT entry mechanisms (e.g., Bollinger breakdown) forward
//   - Compare candidate configs on identical forward market data
//   - Eliminate retroactive overfitting via pre-registered alternatives
type ShadowSpec struct {
	Label string // used for journal subdirectory: shadow/<Label>/
	Type  string // "ema" (default) or "bb"

	// EMA fields (used when Type == "ema")
	EMAFastPeriod int // 0 → backtest defaults to 9
	EMASlowPeriod int // 0 → backtest defaults to 21

	// BB fields (used when Type == "bb")
	BollingerPeriod  int     // 0 → defaults to 20
	BollingerStdMult float64 // 0 → defaults to 2.0

	MaxHoldHours float64 // 0 → no cap (common to both types)
}

// ParseShadowSpecs parses a comma-separated list of shadow specs.
// Returns nil + nil error for empty input (shadow mode disabled).
//
// Two formats supported (chosen by colon count after label):
//   - EMA legacy: "label:ema_fast-ema_slow-max_hold"  (1 colon after label)
//   - BB:         "label:bb:bb_period-bb_std-max_hold" (2 colons; explicit type)
//
// label: alphanumeric+underscore identifier for journal directory
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
		colonParts := strings.Split(p, ":")
		switch len(colonParts) {
		case 2:
			// Legacy EMA format: label:ema_fast-ema_slow-max_hold
			label := strings.TrimSpace(colonParts[0])
			spec, err := parseEMAShadow(i, label, colonParts[1])
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		case 3:
			// Type-prefixed format: label:type:params
			label := strings.TrimSpace(colonParts[0])
			typ := strings.TrimSpace(colonParts[1])
			switch typ {
			case "ema":
				spec, err := parseEMAShadow(i, label, colonParts[2])
				if err != nil {
					return nil, err
				}
				specs = append(specs, spec)
			case "bb":
				spec, err := parseBBShadow(i, label, colonParts[2])
				if err != nil {
					return nil, err
				}
				specs = append(specs, spec)
			default:
				return nil, fmt.Errorf("shadow spec %d (%s): unknown type %q (expected 'ema' or 'bb')", i, label, typ)
			}
		default:
			return nil, fmt.Errorf("shadow spec %d: expected 'label:params' or 'label:type:params', got %q", i, p)
		}
	}
	return specs, nil
}

func parseEMAShadow(i int, label, paramsStr string) (ShadowSpec, error) {
	params := strings.Split(strings.TrimSpace(paramsStr), "-")
	if len(params) != 3 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): EMA expects 3 dash-separated params (fast-slow-max_hold), got %d", i, label, len(params))
	}
	fast, err := strconv.Atoi(params[0])
	if err != nil || fast <= 0 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): ema_fast must be positive integer, got %q", i, label, params[0])
	}
	slow, err := strconv.Atoi(params[1])
	if err != nil || slow <= 0 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): ema_slow must be positive integer, got %q", i, label, params[1])
	}
	mh, err := strconv.ParseFloat(params[2], 64)
	if err != nil || mh < 0 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): max_hold must be non-negative float, got %q", i, label, params[2])
	}
	return ShadowSpec{
		Label:         label,
		Type:          "ema",
		EMAFastPeriod: fast,
		EMASlowPeriod: slow,
		MaxHoldHours:  mh,
	}, nil
}

func parseBBShadow(i int, label, paramsStr string) (ShadowSpec, error) {
	params := strings.Split(strings.TrimSpace(paramsStr), "-")
	if len(params) != 3 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): BB expects 3 dash-separated params (period-std-max_hold), got %d", i, label, len(params))
	}
	period, err := strconv.Atoi(params[0])
	if err != nil || period <= 1 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): bollinger_period must be integer > 1, got %q", i, label, params[0])
	}
	std, err := strconv.ParseFloat(params[1], 64)
	if err != nil || std <= 0 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): bollinger_std must be positive float, got %q", i, label, params[1])
	}
	mh, err := strconv.ParseFloat(params[2], 64)
	if err != nil || mh < 0 {
		return ShadowSpec{}, fmt.Errorf("shadow spec %d (%s): max_hold must be non-negative float, got %q", i, label, params[2])
	}
	return ShadowSpec{
		Label:            label,
		Type:             "bb",
		BollingerPeriod:  period,
		BollingerStdMult: std,
		MaxHoldHours:     mh,
	}, nil
}
