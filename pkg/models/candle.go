package models

import "time"

// Timeframe represents a candle duration.
type Timeframe string

const (
	Timeframe5m  Timeframe = "5m"
	Timeframe30m Timeframe = "30m"
	Timeframe1H  Timeframe = "1H"
	Timeframe2H  Timeframe = "2H"
	Timeframe4H  Timeframe = "4H"
	Timeframe1D  Timeframe = "1D"
)

// Timeframes is the ordered set of timeframes the aggregator produces.
var Timeframes = []Timeframe{Timeframe5m, Timeframe30m, Timeframe1H, Timeframe2H, Timeframe4H, Timeframe1D}

// Candle is an OHLCV candle for a specific timeframe.
// Timestamps come from the exchange; never set using time.Now().
type Candle struct {
	Symbol    string
	Timeframe Timeframe
	OpenTime  time.Time
	CloseTime time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	// Closed is true when the candle period has ended and the candle is final.
	Closed bool
}
