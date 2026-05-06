package models

import "time"

// Direction is the trade side.
type Direction int

const (
	Neutral Direction = 0
	Long    Direction = 1
	Short   Direction = -1
)

func (d Direction) String() string {
	switch d {
	case Long:
		return "LONG"
	case Short:
		return "SHORT"
	default:
		return "NEUTRAL"
	}
}

// Signal is a trade instruction produced by the strategy engine.
type Signal struct {
	Symbol     string
	Side       Direction
	EntryPrice float64
	StopLoss   float64
	TakeProfit float64
	Timestamp  time.Time
	Reason     string
}
