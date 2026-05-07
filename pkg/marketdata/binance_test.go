package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// mockKline returns one Binance kline row in the JSON-array shape the production
// code expects. Time fields are JSON numbers; price/volume are quoted strings —
// matching parseRawInt64 / parseRawFloat in binance.go.
func mockKline(openMs int64, price string) []any {
	return []any{
		openMs,
		price, price, price, price, // open / high / low / close
		"1.0",               // volume
		openMs + 60_000 - 1, // closeMs (1m bar)
		"100.0", 1, "0", "0", "0", // quote vol, trades, taker base/quote, ignore
	}
}

// klineHandler is a test double for /fapi/v1/klines. It records every call's
// query params (so tests can assert pagination cursor advancement and limit
// handling) and returns `serveLimit` chronologically-ordered klines starting at
// `startTime`. When serveLimit < 0 it serves the requested limit verbatim — the
// normal Binance behavior. Setting serveLimit to a smaller positive number
// simulates "end of available history" / sparse symbols.
type klineHandler struct {
	mu         sync.Mutex
	calls      []url.Values
	serveLimit int // -1 = honor request, otherwise truncate
}

func (h *klineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.calls = append(h.calls, r.URL.Query())
	h.mu.Unlock()

	startTime, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
	requested, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	limit := requested
	if h.serveLimit >= 0 && h.serveLimit < limit {
		limit = h.serveLimit
	}

	klines := make([]any, 0, limit)
	for i := 0; i < limit; i++ {
		openMs := startTime + int64(i)*60_000
		klines = append(klines, mockKline(openMs, "100.0"))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(klines)
}

// TestBackfillPaginates verifies that backfill walks forward in 1500-kline pages
// when backfillHours requires more than one Binance call (per-call cap is 1500
// klines = 25h of 1m data). Pre-fix this silently truncated at 25h regardless of
// the configured backfill_hours, keeping 4H-signal indicators (EMA21) unprimed
// until ~64h of live runtime accumulated — see CLAUDE.md Bug 5.
func TestBackfillPaginates_96Hours(t *testing.T) {
	h := &klineHandler{serveLimit: -1}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	bufSize := 96*60*4 + 2048
	ch := make(chan models.Tick, bufSize)

	if err := b.backfill(context.Background(), ch); err != nil {
		t.Fatalf("backfill returned error: %v", err)
	}

	// 96h × 60 min = 5760 klines. Per-call cap is 1500 → expect ceil(5760/1500) = 4 pages.
	const expectedPages = 4
	if got := len(h.calls); got != expectedPages {
		t.Fatalf("expected %d HTTP calls, got %d", expectedPages, got)
	}

	// First three calls request the full 1500. The last requests the remainder
	// (5760 − 3×1500 = 1260) so we don't over-fetch past totalKlines.
	for i := 0; i < 3; i++ {
		if got := h.calls[i].Get("limit"); got != "1500" {
			t.Errorf("call %d: expected limit=1500, got %s", i, got)
		}
	}
	if got := h.calls[3].Get("limit"); got != "1260" {
		t.Errorf("call 3: expected limit=1260 (5760-3×1500), got %s", got)
	}

	// startTime must advance monotonically — each page picks up where the prior
	// page's last 1m bar ended. Without this, klines would overlap (duplicates)
	// or skip ranges (gaps), breaking aggregator state.
	var prev int64
	for i, q := range h.calls {
		st, err := strconv.ParseInt(q.Get("startTime"), 10, 64)
		if err != nil {
			t.Errorf("call %d: malformed startTime: %v", i, err)
			continue
		}
		if i > 0 && st <= prev {
			t.Errorf("call %d: startTime %d not greater than prior %d", i, st, prev)
		}
		prev = st
	}

	// Each kline expands to 4 ticks (open/high/low/close) — see expandKlineToTicks.
	expectedTicks := 5760 * 4
	if got := len(ch); got != expectedTicks {
		t.Fatalf("expected %d ticks in channel, got %d", expectedTicks, got)
	}

	// Drain and verify chronological order (load-bearing for the aggregator —
	// it bins by exchange timestamp and would mis-bin out-of-order ticks).
	var lastTime time.Time
	for i := 0; i < expectedTicks; i++ {
		tick := <-ch
		if !lastTime.IsZero() && tick.Timestamp.Before(lastTime) {
			t.Fatalf("tick %d out of order: %v before %v", i, tick.Timestamp, lastTime)
		}
		lastTime = tick.Timestamp
	}
}

// TestBackfillSinglePage covers the small-backfill path (≤ 25h) that fits in
// one Binance call, preserving the pre-Bug-5 single-call behavior for callers
// that explicitly set a small backfill_hours.
func TestBackfillSinglePage_24Hours(t *testing.T) {
	h := &klineHandler{serveLimit: -1}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 24)
	ch := make(chan models.Tick, 24*60*4+2048)

	if err := b.backfill(context.Background(), ch); err != nil {
		t.Fatalf("backfill returned error: %v", err)
	}
	if len(h.calls) != 1 {
		t.Errorf("expected 1 HTTP call for 24h backfill, got %d", len(h.calls))
	}
	if got := h.calls[0].Get("limit"); got != "1440" {
		t.Errorf("expected limit=1440 (24×60), got %s", got)
	}
	if got, want := len(ch), 24*60*4; got != want {
		t.Errorf("expected %d ticks, got %d", want, got)
	}
}

// TestBackfillStopsOnPartialPage verifies graceful early termination when the
// server returns fewer rows than asked — the case for a recently-listed symbol
// with less than backfillHours of available history. Without this terminator,
// the loop would spin re-requesting from the same cursor.
func TestBackfillStopsOnPartialPage(t *testing.T) {
	// Serve only 100 klines per call regardless of requested limit.
	h := &klineHandler{serveLimit: 100}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	if err := b.backfill(context.Background(), ch); err != nil {
		t.Fatalf("backfill returned error: %v", err)
	}

	// After receiving 100 < 1500 on the first call, backfill must break out of
	// the pagination loop rather than keep asking for more.
	if len(h.calls) != 1 {
		t.Errorf("expected pagination to terminate after partial page, got %d calls", len(h.calls))
	}
	if got, want := len(ch), 100*4; got != want {
		t.Errorf("expected %d ticks (100 klines × 4), got %d", want, got)
	}
}

// TestBackfillRespectsContext verifies that an already-cancelled context aborts
// the loop without making HTTP calls (or stops mid-way without leaking the
// running goroutine). Important because Subscribe is called from main(),
// which may be unwinding via a SIGTERM ctx.Done.
func TestBackfillRespectsContext(t *testing.T) {
	h := &klineHandler{serveLimit: -1}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	err := b.backfill(ctx, ch)
	// Either an explicit ctx error or a request build error from the cancelled
	// context is acceptable — both prove the cancellation propagated.
	if err == nil {
		t.Errorf("expected error from cancelled context, got nil")
	}
}
