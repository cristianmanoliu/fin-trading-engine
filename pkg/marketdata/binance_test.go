package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
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

// TestProcessAggTradeBatch_AdvancesCursor_AllSkipped verifies the 2026-05-10
// 2nd-pass audit fix on Finding J: when every trade in a batch fails price/
// qty parse (e.g., Binance schema rename of "p" or "q"), lastID must STILL
// advance via max(t.ID) so the next poll fetches new data. Pre-fix the
// cursor advance was inside the parse-success branch — all-skipped batches
// left lastID stuck and the loop spun on the same range indefinitely while
// `count=len(trades)` masked the failure as a normal "1500 trades" log.
//
// The heartbeat catches the no-tick downstream, but at the marketdata layer
// the spin was invisible. The fix is twofold: cursor advances independently
// of parse outcome, and an all-skipped batch routes to slog.Error rather
// than the misleading INFO.
func TestProcessAggTradeBatch_AdvancesCursor_AllSkipped(t *testing.T) {
	b := NewBinanceFutures("ws://unused", "http://unused", "TESTUSDT", 0)
	ch := make(chan models.Tick, 100)

	// Batch where every trade has a malformed price (e.g., schema rename
	// where "p" no longer exists so Price comes back as ""). IDs are valid.
	trades := []aggTradeREST{
		{ID: 1001, Price: "", Qty: "1.0", Timestamp: time.Now().UnixMilli()},
		{ID: 1002, Price: "not-a-num", Qty: "1.0", Timestamp: time.Now().UnixMilli()},
		{ID: 1003, Price: "", Qty: "1.0", Timestamp: time.Now().UnixMilli()},
	}

	newLastID, parsed, skipped := b.processAggTradeBatch(context.Background(), trades, 1000, ch)

	// Cursor MUST advance via max(t.ID) even though every trade was skipped.
	// Pre-fix: newLastID would equal 1000 (lastID didn't move).
	if newLastID != 1003 {
		t.Errorf("cursor not advanced on all-skipped batch: expected lastID=1003, got %d", newLastID)
	}
	if parsed != 0 {
		t.Errorf("expected 0 parsed (every trade malformed), got %d", parsed)
	}
	if skipped != 3 {
		t.Errorf("expected 3 skipped, got %d", skipped)
	}
	if len(ch) != 0 {
		t.Errorf("expected 0 ticks emitted from all-skipped batch, got %d", len(ch))
	}
}

// TestProcessAggTradeBatch_AdvancesCursor_AllValid verifies the happy path
// is unchanged: every valid trade emits a tick and lastID advances.
func TestProcessAggTradeBatch_AdvancesCursor_AllValid(t *testing.T) {
	b := NewBinanceFutures("ws://unused", "http://unused", "TESTUSDT", 0)
	ch := make(chan models.Tick, 100)

	now := time.Now().UnixMilli()
	trades := []aggTradeREST{
		{ID: 2001, Price: "100.5", Qty: "1.0", Timestamp: now},
		{ID: 2002, Price: "100.6", Qty: "1.5", Timestamp: now + 100},
		{ID: 2003, Price: "100.7", Qty: "0.5", Timestamp: now + 200},
	}

	newLastID, parsed, skipped := b.processAggTradeBatch(context.Background(), trades, 2000, ch)

	if newLastID != 2003 {
		t.Errorf("expected lastID=2003 on clean batch, got %d", newLastID)
	}
	if parsed != 3 {
		t.Errorf("expected 3 parsed, got %d", parsed)
	}
	if skipped != 0 {
		t.Errorf("expected 0 skipped on clean batch, got %d", skipped)
	}
	if len(ch) != 3 {
		t.Errorf("expected 3 ticks emitted, got %d", len(ch))
	}
}

// TestProcessAggTradeBatch_PartialSkipped_StillAdvancesCursor verifies the
// realistic mixed-batch case: 1 of N trades has a malformed price (e.g.,
// transient corruption on a single record). Cursor still advances to the
// max ID in the batch — the malformed trade is skipped but does not cause
// re-fetching of the surrounding valid trades.
func TestProcessAggTradeBatch_PartialSkipped_StillAdvancesCursor(t *testing.T) {
	b := NewBinanceFutures("ws://unused", "http://unused", "TESTUSDT", 0)
	ch := make(chan models.Tick, 100)

	now := time.Now().UnixMilli()
	trades := []aggTradeREST{
		{ID: 3001, Price: "100.5", Qty: "1.0", Timestamp: now},
		{ID: 3002, Price: "", Qty: "1.0", Timestamp: now + 100}, // malformed
		{ID: 3003, Price: "100.7", Qty: "0.5", Timestamp: now + 200},
	}

	newLastID, parsed, skipped := b.processAggTradeBatch(context.Background(), trades, 3000, ch)

	if newLastID != 3003 {
		t.Errorf("expected lastID=3003 (max in batch), got %d", newLastID)
	}
	if parsed != 2 {
		t.Errorf("expected 2 parsed (1 malformed skipped), got %d", parsed)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
	if len(ch) != 2 {
		t.Errorf("expected 2 ticks emitted, got %d", len(ch))
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

// aggTradeRateLimitHandler is a test double for /fapi/v1/aggTrades that serves a
// valid latest-ID once (so aggTradeLoop's initial-ID fetch succeeds and the loop
// reaches its steady-state poll), then returns HTTP 418 forever — simulating a
// sustained Binance IP ban. It records call counts for assertions.
//
//   - The initial-ID probe uses limit=1 with NO fromId → serve a valid 1-row body.
//   - Every steady-state poll uses fromId=<n> → serve 418.
type aggTradeRateLimitHandler struct {
	probeCalls int32 // /aggTrades?...&limit=1 with no fromId (latest-ID fetch)
	pollCalls  int32 // /aggTrades?...&fromId=... (steady-state poll)
}

func (h *aggTradeRateLimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("fromId") == "" {
		atomic.AddInt32(&h.probeCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		// One trade so latestAggTradeID parses an ID and the loop proceeds.
		_, _ = w.Write([]byte(`[{"a":1000,"p":"100.0","q":"1.0","T":1700000000000}]`))
		return
	}
	atomic.AddInt32(&h.pollCalls, 1)
	w.WriteHeader(http.StatusTeapot) // 418 — IP banned, every steady-state poll
	_, _ = w.Write([]byte(`{"code":-1003,"msg":"Way too many requests; IP banned until 999"}`))
}

// TestAggTradeLoop_SurvivesSustained418_DoesNotExitGoroutine is the regression
// test for the ORIGINAL Bug 4 incident (CLAUDE.md: "On 418/429, back off 60s and
// retry — never exit the goroutine (closes tick channel → 'clean' shutdown →
// infinite systemd restart loop)"). This invariant — the load-bearing one for the
// live data feed — had ZERO test coverage before this.
//
// The loop must NOT return on a 418; it must enter the rate-limit backoff and keep
// running until ctx is canceled. A buggy `return`-on-418 would close the tick
// channel and (in prod) trigger the systemd restart-loop that self-extended the
// 2026-05-29 testnet ban for 10 days.
func TestAggTradeLoop_SurvivesSustained418_DoesNotExitGoroutine(t *testing.T) {
	h := &aggTradeRateLimitHandler{}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 0)
	// Tiny intervals so the first poll fires immediately and the backoff is short
	// enough to observe survival without waiting the prod 60s. Production leaves
	// both unset → the 10s/60s defaults (asserted byte-for-byte by a sibling test).
	b.RestPollInterval = 5 * time.Millisecond
	b.RateLimitBackoff = 20 * time.Millisecond

	ch := make(chan models.Tick, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { b.aggTradeLoop(ctx, ch); close(done) }()

	// While ctx is live, the loop must NOT exit under sustained 418. A buggy
	// `return`-on-418 impl would close `done` here.
	select {
	case <-done:
		t.Fatalf("aggTradeLoop exited on its own under sustained 418 — goroutine must survive until ctx cancel (Bug 4: exit → closed tick channel → systemd restart loop → re-poll-while-banned)")
	case <-time.After(300 * time.Millisecond):
		// Good: still polling across multiple 418/backoff cycles.
	}

	// Confirm the steady-state 418 path was actually exercised (not stalled in the
	// initial-ID fetch) — otherwise survival proves nothing about the hot path.
	if got := atomic.LoadInt32(&h.pollCalls); got < 2 {
		t.Fatalf("server saw %d steady-state polls in 300ms — expected ≥2 sustained-418 cycles; line-443 backoff path not exercised", got)
	}

	// Cancel: the loop must return promptly.
	cancel()
	select {
	case <-done:
		// Good.
	case <-time.After(2 * time.Second):
		t.Fatal("aggTradeLoop did not return within 2s of ctx cancel")
	}
}

// TestAggTradeLoop_CtxCancelDuringBackoff_ReturnsPromptly asserts a shutdown is
// not blocked for a full RateLimitBackoff (60s in prod) when the cancel lands
// while the loop is sleeping off a 418. The backoff select must honor ctx.Done.
func TestAggTradeLoop_CtxCancelDuringBackoff_ReturnsPromptly(t *testing.T) {
	h := &aggTradeRateLimitHandler{}
	server := httptest.NewServer(h)
	defer server.Close()

	b := NewBinanceFutures("ws://unused", server.URL, "TESTUSDT", 0)
	// Poll fires fast; backoff is LONG so that if the loop ignored ctx during the
	// backoff sleep, this test would time out rather than pass.
	b.RestPollInterval = 5 * time.Millisecond
	b.RateLimitBackoff = 30 * time.Second

	ch := make(chan models.Tick, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { b.aggTradeLoop(ctx, ch); close(done) }()

	// Let the loop fetch the initial ID, fire the first poll (418), and enter the
	// backoff sleep, then cancel mid-backoff.
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("aggTradeLoop returned %v after cancel — blocked on the %v backoff instead of honoring ctx", elapsed, b.RateLimitBackoff)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("aggTradeLoop did not return within 5s of cancel — backoff (%v) blocked ctx cancellation", b.RateLimitBackoff)
	}
}

// TestBinanceFutures_RateLimitDefaults_MatchProductionLiterals locks the
// production cadence: when RestPollInterval / RateLimitBackoff are left unset
// (the prod path — NewBinanceFutures sets neither), the effective values MUST be
// the historical 10s poll / 60s backoff. This is the byte-for-byte guard that the
// testability refactor changed NO runtime behavior. (CLAUDE.md: "Do NOT lower
// [restPollInterval] without recomputing" — this test fails loudly if anyone does.)
func TestBinanceFutures_RateLimitDefaults_MatchProductionLiterals(t *testing.T) {
	b := NewBinanceFutures("ws://unused", "http://unused", "TESTUSDT", 0)
	if got := b.effectiveRestPollInterval(); got != 10*time.Second {
		t.Errorf("default poll interval = %v, want 10s (production literal)", got)
	}
	if got := b.effectiveRateLimitBackoff(); got != 60*time.Second {
		t.Errorf("default rate-limit backoff = %v, want 60s (production literal)", got)
	}
}
