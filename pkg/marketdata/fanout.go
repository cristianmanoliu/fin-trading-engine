package marketdata

import (
	"context"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// FanOutCandles broadcasts each candle from `in` to `n` newly-created output
// channels. Used by cmd/engine to feed identical candle streams to the live
// Runner and any number of shadow Runners.
//
// Buffering: each output has a buffer of 256 candles, sized to absorb brief
// consumer stalls without dropping. If a consumer falls badly behind for
// >256 candles (~21 days at 4H), the broadcast goroutine will BLOCK on that
// consumer rather than drop — the failure mode is back-pressure, not data
// loss. This matches the strategy package's invariant that no candle is ever
// silently dropped.
//
// Lifecycle: the broadcast goroutine exits when `in` closes OR ctx is cancelled.
// All output channels are closed before exit.
func FanOutCandles(ctx context.Context, in <-chan models.Candle, n int) []<-chan models.Candle {
	if n <= 0 {
		return nil
	}
	outs := make([]chan models.Candle, n)
	for i := range outs {
		outs[i] = make(chan models.Candle, 256)
	}
	go func() {
		defer func() {
			for _, o := range outs {
				close(o)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case c, ok := <-in:
				if !ok {
					return
				}
				for _, o := range outs {
					select {
					case o <- c:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	result := make([]<-chan models.Candle, n)
	for i, o := range outs {
		result[i] = o
	}
	return result
}

// FanOutTicks does the same for tick streams. Buffer 1024 (vs candles' 256)
// because tick rate is much higher (~1/sec vs ~1/4h).
func FanOutTicks(ctx context.Context, in <-chan models.Tick, n int) []<-chan models.Tick {
	if n <= 0 {
		return nil
	}
	outs := make([]chan models.Tick, n)
	for i := range outs {
		outs[i] = make(chan models.Tick, 1024)
	}
	go func() {
		defer func() {
			for _, o := range outs {
				close(o)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case t, ok := <-in:
				if !ok {
					return
				}
				for _, o := range outs {
					select {
					case o <- t:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	result := make([]<-chan models.Tick, n)
	for i, o := range outs {
		result[i] = o
	}
	return result
}
