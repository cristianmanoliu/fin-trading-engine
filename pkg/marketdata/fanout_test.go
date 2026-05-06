package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestFanOutCandles_BroadcastsToAllConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan models.Candle, 10)
	outs := FanOutCandles(ctx, in, 3)
	if len(outs) != 3 {
		t.Fatalf("want 3 outputs, got %d", len(outs))
	}

	c := models.Candle{Symbol: "BTC", Open: 100, Close: 110}
	in <- c

	// Each output should receive an identical copy.
	for i, o := range outs {
		select {
		case got := <-o:
			if got.Symbol != "BTC" || got.Open != 100 || got.Close != 110 {
				t.Errorf("out[%d] received wrong candle: %+v", i, got)
			}
		case <-time.After(time.Second):
			t.Errorf("out[%d] did not receive within 1s", i)
		}
	}
}

func TestFanOutCandles_ClosesOutputsWhenInputCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan models.Candle, 1)
	outs := FanOutCandles(ctx, in, 2)
	close(in)

	// All outputs should close (recv returns ok=false)
	for i, o := range outs {
		select {
		case _, ok := <-o:
			if ok {
				// Maybe drained one item; try one more
				_, ok2 := <-o
				if ok2 {
					t.Errorf("out[%d] still open after input closed", i)
				}
			}
		case <-time.After(time.Second):
			t.Errorf("out[%d] did not close within 1s", i)
		}
	}
}

func TestFanOutCandles_ClosesOutputsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	in := make(chan models.Candle, 1)
	outs := FanOutCandles(ctx, in, 2)

	cancel()

	for i, o := range outs {
		select {
		case _, ok := <-o:
			if ok {
				_, ok2 := <-o
				if ok2 {
					t.Errorf("out[%d] still open after ctx cancel", i)
				}
			}
		case <-time.After(time.Second):
			t.Errorf("out[%d] did not close within 1s of ctx cancel", i)
		}
	}
}

func TestFanOutCandles_ZeroConsumers(t *testing.T) {
	// n=0 should return nil cleanly (no panic)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan models.Candle)
	outs := FanOutCandles(ctx, in, 0)
	if outs != nil {
		t.Errorf("n=0 should return nil, got %v", outs)
	}
}

func TestFanOutTicks_BroadcastsToAllConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan models.Tick, 10)
	outs := FanOutTicks(ctx, in, 4)
	if len(outs) != 4 {
		t.Fatalf("want 4 outputs, got %d", len(outs))
	}

	tick := models.Tick{Symbol: "BTC", Price: 50000, Volume: 1.5}
	in <- tick

	for i, o := range outs {
		select {
		case got := <-o:
			if got.Symbol != "BTC" || got.Price != 50000 || got.Volume != 1.5 {
				t.Errorf("out[%d] received wrong tick: %+v", i, got)
			}
		case <-time.After(time.Second):
			t.Errorf("out[%d] did not receive within 1s", i)
		}
	}
}
