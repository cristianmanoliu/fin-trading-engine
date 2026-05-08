package marketdata

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// CSVReplay is the backtest data source — every backtest claim transitively
// depends on it correctly parsing Binance kline CSVs into a tick stream. The
// shared expandKlineToTicks helper is already covered by klines_test, so
// these tests focus on the CSV-parse → kline → expand → channel pipeline:
//
//   - Subscribe error path (missing file)
//   - Header-row skipping (both Binance variants: lowercase and capitalized)
//   - Malformed-row resilience (short rows, unparseable timestamps)
//   - Kline-to-tick fan-out (4 ticks per kline, prices in O→H→L→C order)
//   - Multi-kline ordering (timestamps monotonic across kline boundaries)
//   - Context cancellation during streaming (no goroutine leak)
//   - Close() before Subscribe is a no-op
//
// Per-tick interpolation correctness lives in klines_test.go. A regression
// in CSVReplay specifically (parse bug, channel-close miss, cancellation
// not honored) would be caught here and not by klines_test.

// writeCSV creates a temp CSV file from the given lines and returns the path.
// Uses t.TempDir so cleanup is automatic.
func writeCSV(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

// drainTicks reads all ticks from ch until it closes, with a hard timeout to
// surface goroutine-leak bugs (channel never closing) as test failures rather
// than hung tests.
func drainTicks(t *testing.T, ch <-chan models.Tick, timeout time.Duration) []models.Tick {
	t.Helper()
	var ticks []models.Tick
	deadline := time.After(timeout)
	for {
		select {
		case tick, ok := <-ch:
			if !ok {
				return ticks
			}
			ticks = append(ticks, tick)
		case <-deadline:
			t.Fatalf("drain timeout after %v — channel not closed (got %d ticks)", timeout, len(ticks))
			return ticks
		}
	}
}

func TestCSVReplay_NonexistentFile_ReturnsError(t *testing.T) {
	r := NewCSVReplay("/nonexistent/path/does-not-exist.csv", "BTCUSDT")
	_, err := r.Subscribe(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestCSVReplay_EmptyFile_ChannelClosesWithNoTicks(t *testing.T) {
	path := writeCSV(t, "")
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 0 {
		t.Errorf("empty file produced %d ticks, want 0", len(ticks))
	}
}

func TestCSVReplay_SingleKline_ProducesFourTicks_OHLC_Order(t *testing.T) {
	// 60s kline window. expandKlineToTicks emits open→high→low→close.
	row := "1000,100.0,105.0,95.0,102.0,1000.0,60999"
	path := writeCSV(t, row)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Fatalf("single kline produced %d ticks, want 4", len(ticks))
	}
	wantPrices := []float64{100.0, 105.0, 95.0, 102.0}
	for i, tick := range ticks {
		if tick.Price != wantPrices[i] {
			t.Errorf("tick %d price = %v, want %v (O→H→L→C order)", i, tick.Price, wantPrices[i])
		}
		if tick.Symbol != "BTCUSDT" {
			t.Errorf("tick %d symbol = %q, want BTCUSDT", i, tick.Symbol)
		}
	}
}

func TestCSVReplay_MultipleKlines_TimestampsMonotonic(t *testing.T) {
	// Three sequential 60s klines. All 12 ticks should be in chronological order
	// across kline boundaries (no out-of-order from CSVReplay).
	rows := []string{
		"1000,100,105,95,102,500,60999",
		"61000,102,108,101,107,600,120999",
		"121000,107,110,103,108,700,180999",
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, 2*time.Second)
	if len(ticks) != 12 {
		t.Fatalf("3 klines produced %d ticks, want 12", len(ticks))
	}
	for i := 1; i < len(ticks); i++ {
		if ticks[i].Timestamp.Before(ticks[i-1].Timestamp) {
			t.Errorf("tick %d ts %v < tick %d ts %v (non-monotonic across kline boundary)",
				i, ticks[i].Timestamp, i-1, ticks[i-1].Timestamp)
		}
	}
}

func TestCSVReplay_HeaderRow_LowerCase_Skipped(t *testing.T) {
	// Binance "open_time" header.
	rows := []string{
		"open_time,open,high,low,close,volume,close_time",
		"1000,100,105,95,102,500,60999",
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Errorf("header + 1 kline produced %d ticks, want 4 (header should be skipped)", len(ticks))
	}
}

func TestCSVReplay_HeaderRow_CapitalCase_Skipped(t *testing.T) {
	// Alternate Binance header variant: "Open time".
	rows := []string{
		"Open time,Open,High,Low,Close,Volume,Close time",
		"1000,100,105,95,102,500,60999",
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Errorf("'Open time' header + 1 kline = %d ticks, want 4", len(ticks))
	}
}

func TestCSVReplay_FieldCountMismatch_TerminatesStream(t *testing.T) {
	// csv.NewReader defaults to FieldsPerRecord=0 which means "set from first
	// record." Any subsequent row with a different column count returns an
	// error from reader.Read, and the stream loop treats every non-EOF error
	// as terminal. So a malformed mid-file row silently drops everything
	// after it. This is a known footgun — pin the behavior explicitly so a
	// future refactor that adjusts the reader config (e.g. FieldsPerRecord=-1
	// to disable the check) is a deliberate API change, not a silent one.
	//
	// Note: the `len(rec) < 7` defense in the loop is only reachable if the
	// FIRST row has fewer than 7 fields (which would set FPR<7). For typical
	// well-formed Binance CSVs (header + 7-col data rows) the guard is
	// effectively dead code — the reader-level FPR check fires first.
	rows := []string{
		"1000,100,105,95,102,500,60999",     // 7 cols, valid → 4 ticks; sets FPR=7
		"61000,102,108,101,107",              // 5 cols → reader err → stream terminates
		"121000,107,110,103,108,700,180999", // unreachable
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Errorf("valid + short + valid = %d ticks, want 4 (stream terminates at first field-count mismatch)", len(ticks))
	}
}

func TestCSVReplay_MalformedOpenTime_RowSkipped(t *testing.T) {
	rows := []string{
		"not_a_number,100,105,95,102,500,60999",
		"61000,102,108,101,107,600,120999",
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Errorf("malformed open_time + valid row = %d ticks, want 4", len(ticks))
	}
}

func TestCSVReplay_MalformedCloseTime_RowSkipped(t *testing.T) {
	rows := []string{
		"1000,100,105,95,102,500,not_a_number",
		"61000,102,108,101,107,600,120999",
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Errorf("malformed close_time + valid row = %d ticks, want 4", len(ticks))
	}
}

func TestCSVReplay_ContextCancel_StopsStreamCleanly(t *testing.T) {
	// Many klines (more than the 1000-buffer chan can hold) so the goroutine
	// will block on send if cancellation isn't honored. Cancel after consuming
	// a handful, expect channel to close within a bounded window.
	rows := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		ms := i * 60_000
		rows = append(rows, fmt.Sprintf("%d,100,105,95,102,500,%d", ms, ms+59_999))
	}
	path := writeCSV(t, rows...)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for i := 0; i < 8; i++ {
		<-ch
	}
	cancel()

	// Drain remaining buffered ticks; channel must close.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed → cancellation honored
			}
		case <-deadline:
			t.Fatal("channel not closed within 2s after context cancel — goroutine leak suspected")
			return
		}
	}
}

func TestCSVReplay_Close_BeforeSubscribe_NoError(t *testing.T) {
	// Close() on a CSVReplay that never had Subscribe called must be a no-op.
	r := NewCSVReplay("/some/path", "BTCUSDT")
	if err := r.Close(); err != nil {
		t.Errorf("Close() before Subscribe = %v, want nil", err)
	}
}

func TestCSVReplay_Volume_SplitEquallyAcrossTicks(t *testing.T) {
	// expandKlineToTicks splits volume equally across the 4 ticks (already
	// covered in klines_test, but pinning it here too — through CSVReplay's
	// integer-vs-float parsing path — guards against a CSV parser bug that
	// could alter the volume value before it reaches the helper.
	row := "1000,100.0,105.0,95.0,102.0,4.0,60999" // volume = 4.0
	path := writeCSV(t, row)
	r := NewCSVReplay(path, "BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ticks := drainTicks(t, ch, time.Second)
	if len(ticks) != 4 {
		t.Fatalf("got %d ticks, want 4", len(ticks))
	}
	for i, tick := range ticks {
		if tick.Volume != 1.0 {
			t.Errorf("tick %d volume = %v, want 1.0 (4.0 split into 4 ticks)", i, tick.Volume)
		}
	}
}
