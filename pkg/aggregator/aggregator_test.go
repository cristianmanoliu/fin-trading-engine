package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// fixtureTick builds a Tick with the given UTC time + price/volume defaults.
func fixtureTick(ts time.Time, price float64) models.Tick {
	return models.Tick{
		Symbol:    "BTCUSDT",
		Timestamp: ts.UTC(),
		Price:     price,
		Volume:    1.0,
	}
}

func TestAggregator_FirstTickOpensCandlesButClosesNone(t *testing.T) {
	a := New(make(chan models.Tick))
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	closed := a.ProcessTick(fixtureTick(t0, 100.0))

	if len(closed) != 0 {
		t.Errorf("first tick should close 0 candles, got %d", len(closed))
	}
	// All timeframes should now have an open candle.
	for _, tf := range models.Timeframes {
		if a.open[tf] == nil {
			t.Errorf("timeframe %s: expected open candle after first tick", tf)
		}
	}
}

func TestAggregator_OHLCTrackingWithinCandle(t *testing.T) {
	a := New(make(chan models.Tick))
	// All ticks within a single 5m window so no candle closes.
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	a.ProcessTick(fixtureTick(t0, 100.0))                      // open=100, h=100, l=100, c=100
	a.ProcessTick(fixtureTick(t0.Add(1*time.Minute), 110.0))   // c=110, h=110
	a.ProcessTick(fixtureTick(t0.Add(2*time.Minute), 95.0))    // c=95, l=95
	a.ProcessTick(fixtureTick(t0.Add(3*time.Minute), 105.0))   // c=105

	c5 := a.open[models.Timeframe5m]
	if c5 == nil {
		t.Fatal("expected open 5m candle")
	}
	if c5.Open != 100 || c5.High != 110 || c5.Low != 95 || c5.Close != 105 {
		t.Errorf("OHLC: got O=%v H=%v L=%v C=%v, want 100/110/95/105",
			c5.Open, c5.High, c5.Low, c5.Close)
	}
	if c5.Volume != 4.0 {
		t.Errorf("Volume: got %v want 4.0 (4 ticks @ 1.0 each)", c5.Volume)
	}
}

func TestAggregator_BoundaryTickClosesCandle_ExcludeBoundary(t *testing.T) {
	// Default behavior (IncludeBoundaryTick=false): boundary tick goes into NEXT candle only.
	a := New(make(chan models.Tick))
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	a.ProcessTick(fixtureTick(t0, 100.0))
	a.ProcessTick(fixtureTick(t0.Add(2*time.Minute), 110.0))
	a.ProcessTick(fixtureTick(t0.Add(4*time.Minute), 105.0))
	// Boundary at t0+5m: this should close the open 5m candle.
	closed := a.ProcessTick(fixtureTick(t0.Add(5*time.Minute), 200.0))

	// Expect the 5m candle closed (and 30m/1H/2H/4H/1D may also have first ticks each)
	var c5 *models.Candle
	for i := range closed {
		if closed[i].Timeframe == models.Timeframe5m {
			c5 = &closed[i]
			break
		}
	}
	if c5 == nil {
		t.Fatalf("expected 5m candle to close, got %d candles: %+v", len(closed), closed)
	}
	// Closed candle reflects ticks 100/110/105 — boundary tick (200) NOT folded in.
	if c5.Open != 100 || c5.High != 110 || c5.Low != 100 || c5.Close != 105 {
		t.Errorf("closed candle excluding boundary: O=%v H=%v L=%v C=%v, want 100/110/100/105",
			c5.Open, c5.High, c5.Low, c5.Close)
	}
	if c5.Volume != 3.0 {
		t.Errorf("Volume: got %v want 3.0", c5.Volume)
	}
	// New open 5m candle should start with the boundary tick (200).
	if a.open[models.Timeframe5m].Open != 200 {
		t.Errorf("new open candle should start at boundary tick price 200, got %v",
			a.open[models.Timeframe5m].Open)
	}
}

func TestAggregator_BoundaryTickClosesCandle_IncludeBoundary(t *testing.T) {
	// IncludeBoundaryTick=true: the boundary tick is folded into the closing candle's OHLCV.
	a := New(make(chan models.Tick))
	a.IncludeBoundaryTick = true
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	a.ProcessTick(fixtureTick(t0, 100.0))
	a.ProcessTick(fixtureTick(t0.Add(2*time.Minute), 110.0))
	a.ProcessTick(fixtureTick(t0.Add(4*time.Minute), 105.0))
	closed := a.ProcessTick(fixtureTick(t0.Add(5*time.Minute), 200.0))

	var c5 *models.Candle
	for i := range closed {
		if closed[i].Timeframe == models.Timeframe5m {
			c5 = &closed[i]
			break
		}
	}
	if c5 == nil {
		t.Fatal("expected 5m candle to close")
	}
	// With boundary inclusion, High = max(110, 200) = 200, Close = 200, Volume += 1.
	if c5.High != 200 {
		t.Errorf("High should include boundary tick 200, got %v", c5.High)
	}
	if c5.Close != 200 {
		t.Errorf("Close should be boundary tick 200, got %v", c5.Close)
	}
	if c5.Volume != 4.0 {
		t.Errorf("Volume should include boundary tick: got %v want 4.0", c5.Volume)
	}
	if c5.Low != 100 {
		t.Errorf("Low unchanged: got %v want 100", c5.Low)
	}
}

func TestAggregator_TimeframeBoundaryAlignment(t *testing.T) {
	// First tick at 12:37:00 UTC should align candles to:
	//   5m  → 12:35:00 (5min boundaries: 0,5,10,...,35,40)
	//   30m → 12:30:00
	//   1H  → 12:00:00
	//   2H  → 12:00:00 (2H boundaries: 0,2,4,...,12,14)
	//   4H  → 12:00:00 (4H boundaries: 0,4,8,12,16,20)
	//   1D  → 00:00:00 of same day
	a := New(make(chan models.Tick))
	t0 := time.Date(2026, 5, 6, 12, 37, 0, 0, time.UTC)
	a.ProcessTick(fixtureTick(t0, 100.0))

	expectedOpens := map[models.Timeframe]time.Time{
		models.Timeframe5m:  time.Date(2026, 5, 6, 12, 35, 0, 0, time.UTC),
		models.Timeframe30m: time.Date(2026, 5, 6, 12, 30, 0, 0, time.UTC),
		models.Timeframe1H:  time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		models.Timeframe2H:  time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		models.Timeframe4H:  time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		models.Timeframe1D:  time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	for tf, want := range expectedOpens {
		got := a.open[tf]
		if got == nil {
			t.Errorf("%s: no open candle", tf)
			continue
		}
		if !got.OpenTime.Equal(want) {
			t.Errorf("%s OpenTime: got %v want %v", tf, got.OpenTime, want)
		}
	}
}

func TestAggregator_MultipleTimeframesCloseSimultaneously(t *testing.T) {
	// A tick at exactly 16:00:00 UTC should close 5m/30m/1H/2H/4H simultaneously
	// (1D not yet — 1D boundary is 00:00).
	a := New(make(chan models.Tick))
	// Open all candles starting at 14:30:00.
	a.ProcessTick(fixtureTick(time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC), 100.0))
	// Add a tick mid-window to populate OHLC.
	a.ProcessTick(fixtureTick(time.Date(2026, 5, 6, 15, 0, 0, 0, time.UTC), 110.0))
	// Boundary tick at 16:00 closes 5m/30m/1H/2H/4H but NOT 1D.
	closed := a.ProcessTick(fixtureTick(time.Date(2026, 5, 6, 16, 0, 0, 0, time.UTC), 105.0))

	closedTFs := map[models.Timeframe]bool{}
	for _, c := range closed {
		closedTFs[c.Timeframe] = true
	}
	for _, tf := range []models.Timeframe{
		models.Timeframe5m, models.Timeframe30m, models.Timeframe1H,
		models.Timeframe2H, models.Timeframe4H,
	} {
		if !closedTFs[tf] {
			t.Errorf("%s: expected to close at 16:00 boundary", tf)
		}
	}
	if closedTFs[models.Timeframe1D] {
		t.Errorf("1D should NOT close at 16:00 (1D boundary is 00:00)")
	}
}

func TestAggregator_RunLifecycle_ClosesChannelsOnContextCancel(t *testing.T) {
	ticks := make(chan models.Tick, 10)
	a := New(ticks)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	// Push one tick to ensure Run is processing.
	ticks <- fixtureTick(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), 100.0)

	// Cancel and verify Run returns and channels close.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}

	// All output channels should be closed.
	for _, tf := range models.Timeframes {
		ch := a.candles[tf]
		select {
		case _, ok := <-ch:
			if ok {
				// Drained any pending — try one more receive to confirm closed.
				_, ok2 := <-ch
				if ok2 {
					t.Errorf("%s channel: expected closed after ctx cancel", tf)
				}
			}
		default:
			t.Errorf("%s channel: not closed (or empty without close signal)", tf)
		}
	}
}

func TestAggregator_RunLifecycle_ClosesChannelsOnTickChannelClose(t *testing.T) {
	ticks := make(chan models.Tick, 10)
	a := New(ticks)
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	ticks <- fixtureTick(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), 100.0)
	close(ticks)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of tick channel close")
	}

	// 5m channel should be closed; reading should yield zero-value, ok=false.
	_, ok := <-a.candles[models.Timeframe5m]
	if ok {
		// drain and try again
		_, ok2 := <-a.candles[models.Timeframe5m]
		if ok2 {
			t.Errorf("5m channel not closed after tick channel close")
		}
	}
}

func TestAggregator_VolumeAccumulation(t *testing.T) {
	a := New(make(chan models.Tick))
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	// Three ticks within same 5m window with different volumes.
	a.ProcessTick(models.Tick{Symbol: "BTC", Timestamp: t0, Price: 100, Volume: 0.5})
	a.ProcessTick(models.Tick{Symbol: "BTC", Timestamp: t0.Add(time.Minute), Price: 100, Volume: 1.5})
	a.ProcessTick(models.Tick{Symbol: "BTC", Timestamp: t0.Add(2 * time.Minute), Price: 100, Volume: 3.0})

	c5 := a.open[models.Timeframe5m]
	if c5.Volume != 5.0 {
		t.Errorf("Volume: got %v want 5.0 (0.5 + 1.5 + 3.0)", c5.Volume)
	}
}
