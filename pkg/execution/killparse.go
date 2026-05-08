package execution

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// ParseClosePositionSpec parses the operator-facing "<sym>,<side>,<qty>;..."
// CSV format used by cmd/kill_switch into the []ClosePosition input that
// KillSwitch.KillAll expects.
//
// Format rules:
//   - Tuples are separated by ";" (semicolon).
//   - Within a tuple, fields are separated by "," (comma): symbol, side, qty.
//   - Symbol is uppercased.
//   - Side is case-insensitive "long" or "short" — anything else is an error
//     (NEUTRAL is rejected — close orders MUST be directional).
//   - Qty must parse as a positive float; non-positive values are rejected
//     (zero-qty close is meaningless; negative is operator typo).
//   - Whitespace around tuples + fields is tolerated.
//   - Trailing ";" is tolerated; empty tuples between ";" are ignored.
//
// Returns an error on the FIRST malformed tuple — fail fast so the operator
// fixes the typo before anything reaches the exchange.
func ParseClosePositionSpec(spec string) ([]ClosePosition, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, errors.New("empty position spec")
	}
	var out []ClosePosition
	for _, tuple := range strings.Split(spec, ";") {
		tuple = strings.TrimSpace(tuple)
		if tuple == "" {
			continue
		}
		fields := strings.Split(tuple, ",")
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid position spec %q: expected 3 fields (symbol,side,qty), got %d",
				tuple, len(fields))
		}
		symbol := strings.ToUpper(strings.TrimSpace(fields[0]))
		if symbol == "" {
			return nil, fmt.Errorf("empty symbol in %q", tuple)
		}
		var side models.Direction
		switch strings.ToUpper(strings.TrimSpace(fields[1])) {
		case "LONG":
			side = models.Long
		case "SHORT":
			side = models.Short
		default:
			return nil, fmt.Errorf("invalid side %q in %q: must be LONG or SHORT", fields[1], tuple)
		}
		qty, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid qty %q in %q: %w", fields[2], tuple, err)
		}
		if qty <= 0 {
			return nil, fmt.Errorf("non-positive qty %v in %q (close orders must close real exposure)", qty, tuple)
		}
		out = append(out, ClosePosition{
			Symbol:   symbol,
			Side:     side,
			Quantity: qty,
		})
	}
	if len(out) == 0 {
		return nil, errors.New("position spec contained no valid tuples (only delimiters?)")
	}
	return out, nil
}
