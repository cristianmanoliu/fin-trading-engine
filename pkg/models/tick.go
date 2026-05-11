package models

import "time"

// Tick represents a single raw trade event from the exchange.
type Tick struct {
	Symbol    string
	Timestamp time.Time
	Price     float64
	Volume    float64

	// LocalReceiptTS records the wall-clock moment WE first observed this
	// tick (vs Timestamp which is the exchange-side trade time). Optional —
	// zero value when unset (e.g., CSV replay, synthetic ticks). When set,
	// (LocalReceiptTS - Timestamp) is the source-to-receipt lag, which
	// matters for measuring the REST-poll lag risk documented in CLAUDE.md
	// "Known unmodeled risks." Heartbeat.Observe consumes this field to
	// maintain a rolling lag distribution.
	LocalReceiptTS time.Time
}
