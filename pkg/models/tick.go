package models

import "time"

// Tick represents a single raw trade event from the exchange.
type Tick struct {
	Symbol    string
	Timestamp time.Time
	Price     float64
	Volume    float64
}
