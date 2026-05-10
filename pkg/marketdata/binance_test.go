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

// TestBackfill_MalformedRowsSkippedNotZeroPriced verifies the 2026-05-10 audit
// fix: previously parseRawFloat errors in backfill were silently swallowed via
// `o, _ := parseRawFloat(...)`. A malformed Binance response with non-numeric
// price would produce klines at price=0, which expand into 0-priced ticks
// that poison EMA / BB / ATR priming. The fix checks each parse error and
// skips the row entirely; gaps recover from subsequent klines + live ticks,
// but 0-priced indicators do not.
func TestBackfill_MalformedRowsSkippedNotZeroPriced(t *testing.T) {
	// Mixed response: 3 valid rows + 1 row with non-numeric price + 1 valid row.
	// Pre-fix the malformed row would have produced 4 ticks at price=0,
	// dragging any EMA initialized off these klines down toward 0.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
		klines := []any{
			mockKline(startTime, "100.0"),
			mockKline(startTime+60_000, "101.0"),
			// Malformed row — price field is a non-numeric string.
			[]any{
				startTime + 120_000,
				"not-a-number", "100.0", "100.0", "100.0",
				"1.0", startTime + 120_000 + 60_000 - 1,
				"100.0", 1, "0", "0", "0",
			},
			mockKline(startTime+180_000, "102.0"),
			mockKline(startTime+240_000, "103.0"),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(klines)
	}))
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 1) // 1h = 60 klines requested
	ch := make(chan models.Tick, 1000)

	if err := b.backfill(context.Background(), ch); err != nil {
		t.Fatalf("backfill returned error: %v", err)
	}
	close(ch)

	// Collect all ticks; assert NONE has price=0 (the regression we're guarding).
	var ticks []models.Tick
	for tick := range ch {
		ticks = append(ticks, tick)
		if tick.Price == 0 {
			t.Errorf("backfill emitted a price=0 tick — malformed row silently produced 0-priced data")
		}
	}
	// 4 valid rows × 4 ticks per kline (expandKlineToTicks) = 16 ticks expected.
	// Pre-fix would have been 5 rows × 4 = 20 ticks (4 of them price=0).
	if len(ticks) != 16 {
		t.Errorf("expected 16 ticks (4 valid klines × 4 ticks/kline), got %d", len(ticks))
	}
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

// failingKlineHandler returns HTTP 500 on the configured page index (1-indexed)
// and serves valid klines on all other pages. Used to simulate Binance REST
// returning a transient 5xx mid-pagination — the production path is "log warn,
// proceed with whatever ticks landed" but the failure surface had zero coverage.
type failingKlineHandler struct {
	mu        sync.Mutex
	calls     []url.Values
	failPage  int  // 1-indexed page number to return 500 on; 0 = never fail
	bodyOnce  bool // when true, return invalid JSON instead of klines
}

func (h *failingKlineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.calls = append(h.calls, r.URL.Query())
	page := len(h.calls) // 1-indexed
	h.mu.Unlock()

	if h.failPage > 0 && page == h.failPage {
		http.Error(w, "binance is having a moment", http.StatusInternalServerError)
		return
	}
	if h.bodyOnce && page == 1 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{ this is not a klines array"))
		return
	}

	startTime, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
	requested, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	klines := make([]any, 0, requested)
	for i := 0; i < requested; i++ {
		openMs := startTime + int64(i)*60_000
		klines = append(klines, mockKline(openMs, "100.0"))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(klines)
}

// TestBackfillFailsOnFirstPage_500 verifies the failure path: when the very
// first kline page returns HTTP 500, backfill returns an error and emits no
// ticks. Subscribe (one level up) catches the error, logs a warn, and proceeds
// — the engine starts blind but operational. Without this regression test,
// a future refactor that swallows the 500 (e.g. retry-forever) could hang
// Subscribe indefinitely.
func TestBackfillFailsOnFirstPage_500(t *testing.T) {
	h := &failingKlineHandler{failPage: 1}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	err := b.backfill(context.Background(), ch)
	if err == nil {
		t.Fatal("expected error on first-page 500, got nil")
	}
	if len(ch) != 0 {
		t.Errorf("expected 0 ticks emitted on first-page failure, got %d", len(ch))
	}
	if len(h.calls) != 1 {
		t.Errorf("expected exactly 1 HTTP call (no retry), got %d", len(h.calls))
	}
}

// TestBackfillFailsOnLaterPage_PreservesEarlierTicks verifies that when a
// mid-pagination request fails, ticks already emitted from successful pages
// remain in the channel. The engine then has partial-but-monotonic price
// history — better than nothing, and the EMA/BB priming will catch up after
// 22 closed 4H candles of live data. Without this guarantee, a 500 on page 3
// of 4 could either (a) silently truncate without an error or (b) corrupt
// the channel with an out-of-order retry.
func TestBackfillFailsOnLaterPage_PreservesEarlierTicks(t *testing.T) {
	h := &failingKlineHandler{failPage: 2}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	err := b.backfill(context.Background(), ch)
	if err == nil {
		t.Fatal("expected error on second-page 500, got nil")
	}
	// Page 1 served 1500 klines × 4 ticks = 6000 ticks before the failure.
	if got := len(ch); got != 1500*4 {
		t.Errorf("expected %d ticks from successful page 1, got %d", 1500*4, got)
	}
	if len(h.calls) != 2 {
		t.Errorf("expected 2 HTTP calls (page 1 succeeds, page 2 fails), got %d", len(h.calls))
	}

	// Drain channel and confirm chronological order is preserved across the
	// partial backfill — load-bearing for the aggregator.
	var lastTime time.Time
	for len(ch) > 0 {
		tick := <-ch
		if !lastTime.IsZero() && tick.Timestamp.Before(lastTime) {
			t.Fatalf("partial backfill produced out-of-order tick: %v < %v",
				tick.Timestamp, lastTime)
		}
		lastTime = tick.Timestamp
	}
}

// TestBackfillFailsOnInvalidJSON verifies that a malformed response body
// (network truncation, content-type mismatch, exchange-side bug) yields an
// error rather than a nil-deref or partial garbage tick. Engine still proceeds
// per Subscribe's non-fatal error contract.
func TestBackfillFailsOnInvalidJSON(t *testing.T) {
	h := &failingKlineHandler{bodyOnce: true}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	err := b.backfill(context.Background(), ch)
	if err == nil {
		t.Fatal("expected decode error on invalid JSON, got nil")
	}
	if len(ch) != 0 {
		t.Errorf("expected 0 ticks on decode failure, got %d", len(ch))
	}
}

// emptyKlineHandler always returns `[]` — the case for a brand-new symbol
// with no historical data, or a query window before symbol listing.
type emptyKlineHandler struct {
	mu    sync.Mutex
	calls int
}

func (h *emptyKlineHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

// TestBackfillEmptyResponse_GracefulBreak verifies that an empty kline page
// terminates pagination without error. Engine starts cold but won't loop
// forever asking for data the exchange will never serve.
func TestBackfillEmptyResponse_GracefulBreak(t *testing.T) {
	h := &emptyKlineHandler{}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 96)
	ch := make(chan models.Tick, 96*60*4+2048)

	if err := b.backfill(context.Background(), ch); err != nil {
		t.Fatalf("empty response should not error, got %v", err)
	}
	if h.calls != 1 {
		t.Errorf("expected exactly 1 HTTP call before break, got %d", h.calls)
	}
	if len(ch) != 0 {
		t.Errorf("expected 0 ticks from empty response, got %d", len(ch))
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
