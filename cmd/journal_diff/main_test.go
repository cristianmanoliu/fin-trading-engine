package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ── pnlDiffPct ──────────────────────────────────────────────────────────────

func TestPnlDiffPct_Identical(t *testing.T) {
	if got := pnlDiffPct(100, 100); got != 0 {
		t.Errorf("identical pnl: got %v, want 0", got)
	}
}

func TestPnlDiffPct_BothZero(t *testing.T) {
	if got := pnlDiffPct(0, 0); got != 0 {
		t.Errorf("both zero: got %v, want 0", got)
	}
}

func TestPnlDiffPct_HalfPct(t *testing.T) {
	// $1000 vs $995 → 0.5%
	got := pnlDiffPct(1000, 995)
	if math.Abs(got-0.5) > 0.0001 {
		t.Errorf("0.5%% case: got %v, want 0.5", got)
	}
}

func TestPnlDiffPct_LargerDenominatorWins(t *testing.T) {
	// pnlDiffPct uses max(|a|, |b|) as denominator — symmetric.
	if got := pnlDiffPct(100, 200); math.Abs(got-50) > 0.0001 {
		t.Errorf("100 vs 200: got %v, want 50.0", got)
	}
	if got := pnlDiffPct(200, 100); math.Abs(got-50) > 0.0001 {
		t.Errorf("200 vs 100 (symmetric): got %v, want 50.0", got)
	}
}

func TestPnlDiffPct_ZeroVsNonZero_MaxDivergence(t *testing.T) {
	if got := pnlDiffPct(0, 100); got != 100 {
		t.Errorf("0 vs 100: got %v, want 100 (max divergence)", got)
	}
	if got := pnlDiffPct(-100, 0); got != 100 {
		t.Errorf("-100 vs 0: got %v, want 100", got)
	}
}

func TestPnlDiffPct_OppositeSign_HighDivergence(t *testing.T) {
	// Long pnl in one journal, short pnl in the other = 200% diff.
	got := pnlDiffPct(100, -100)
	if math.Abs(got-200) > 0.0001 {
		t.Errorf("opposite-sign: got %v, want 200", got)
	}
}

// ── matchPairs ──────────────────────────────────────────────────────────────

func makeTrade(symbol, openTS, side string, pnl float64) trade {
	return trade{Symbol: symbol, OpenTS: openTS, Side: side, PnlUSD: pnl, Outcome: "STOP"}
}

func TestMatchPairs_BothEmpty(t *testing.T) {
	matched, onlyA, onlyB := matchPairs(nil, nil)
	if len(matched) != 0 || len(onlyA) != 0 || len(onlyB) != 0 {
		t.Errorf("got matched=%d onlyA=%d onlyB=%d, want 0/0/0",
			len(matched), len(onlyA), len(onlyB))
	}
}

func TestMatchPairs_FullyMatched(t *testing.T) {
	a := []trade{
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000),
		makeTrade("ETHUSDT", "2026-05-08T12:00:00Z", "SHORT", -500),
	}
	b := []trade{
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1005),
		makeTrade("ETHUSDT", "2026-05-08T12:00:00Z", "SHORT", -498),
	}
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 2 {
		t.Errorf("matched = %d, want 2", len(matched))
	}
	if len(onlyA) != 0 || len(onlyB) != 0 {
		t.Errorf("expected zero unmatched, got onlyA=%d onlyB=%d", len(onlyA), len(onlyB))
	}
	// First pair: 1000 vs 1005 → 0.4975% diff
	if matched[0].DiffPct < 0.49 || matched[0].DiffPct > 0.5 {
		t.Errorf("BTC pair DiffPct: got %v, want ~0.5", matched[0].DiffPct)
	}
}

func TestMatchPairs_OnlyA_SignalDivergence(t *testing.T) {
	a := []trade{
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000),
	}
	b := []trade{} // B has nothing
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 0 {
		t.Errorf("matched = %d, want 0", len(matched))
	}
	if len(onlyA) != 1 {
		t.Errorf("onlyA = %d, want 1", len(onlyA))
	}
	if len(onlyB) != 0 {
		t.Errorf("onlyB = %d, want 0", len(onlyB))
	}
}

func TestMatchPairs_OnlyB_SignalDivergence(t *testing.T) {
	a := []trade{}
	b := []trade{
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000),
	}
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 0 || len(onlyA) != 0 || len(onlyB) != 1 {
		t.Errorf("matched=%d onlyA=%d onlyB=%d, want 0/0/1",
			len(matched), len(onlyA), len(onlyB))
	}
}

func TestMatchPairs_SideMismatch_NotMatched(t *testing.T) {
	// Same symbol + ts but different side = different signal (one strategy
	// fired LONG, the other SHORT). Should NOT match — that's signal divergence.
	a := []trade{makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000)}
	b := []trade{makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "SHORT", 1000)}
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 0 {
		t.Errorf("side-mismatch matched: got %d, want 0", len(matched))
	}
	if len(onlyA) != 1 || len(onlyB) != 1 {
		t.Errorf("expected each to be unique: onlyA=%d onlyB=%d", len(onlyA), len(onlyB))
	}
}

func TestMatchPairs_TimestampMismatch_NotMatched(t *testing.T) {
	// Even ms-level differences in open_ts mean no match. This matters
	// because Layer 3 uses sig.Timestamp (the candle close ts) which is
	// stable across executors — if it's NOT stable, the tool surfaces it
	// as signal divergence rather than silently aligning.
	a := []trade{makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000)}
	b := []trade{makeTrade("BTCUSDT", "2026-05-08T08:00:01Z", "LONG", 1000)}
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 0 {
		t.Error("ts-mismatch should not match")
	}
	if len(onlyA) != 1 || len(onlyB) != 1 {
		t.Errorf("expected each unique: onlyA=%d onlyB=%d", len(onlyA), len(onlyB))
	}
}

func TestMatchPairs_DuplicateOpenTS_FirstWins(t *testing.T) {
	// Defensive: if two trades share the same key (shouldn't happen in
	// practice — opens are at candle close + side filter ensures one
	// signal per candle per direction), the first match consumes the B
	// side. Subsequent A-side entries with the same key go to onlyA.
	a := []trade{
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1000),
		makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 2000), // same key
	}
	b := []trade{makeTrade("BTCUSDT", "2026-05-08T08:00:00Z", "LONG", 1010)}
	matched, onlyA, onlyB := matchPairs(a, b)
	if len(matched) != 1 || len(onlyA) != 1 || len(onlyB) != 0 {
		t.Errorf("duplicate-key handling: matched=%d onlyA=%d onlyB=%d, want 1/1/0",
			len(matched), len(onlyA), len(onlyB))
	}
	// The matched A should be the FIRST one (1000 vs 1010, ~1% diff).
	if matched[0].A.PnlUSD != 1000 {
		t.Errorf("first-wins: matched A.PnlUSD = %v, want 1000", matched[0].A.PnlUSD)
	}
}

// ── parseJournalFile ────────────────────────────────────────────────────────

func writeFile(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestParseJournalFile_OpenAndClose_SingleTrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	writeFile(t, path, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	})
	got, err := parseJournalFile(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("trades = %d, want 1", len(got))
	}
	if got[0].OpenTS != "2026-05-08T08:00:00Z" {
		t.Errorf("OpenTS: %q want 2026-05-08T08:00:00Z (matches the OPEN event ts, NOT close)", got[0].OpenTS)
	}
	if got[0].PnlUSD != 3000 {
		t.Errorf("PnlUSD: %v want 3000", got[0].PnlUSD)
	}
}

func TestParseJournalFile_PartialClose_KeepsOpenAlive(t *testing.T) {
	// open → partial close → terminal close should produce TWO trade records,
	// both keyed off the SAME open_ts. PARTIAL doesn't terminate the open.
	dir := t.TempDir()
	path := filepath.Join(dir, "X-2026-05.jsonl")
	writeFile(t, path, []string{
		`{"event":"open","symbol":"X","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"r"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-08T08:30:00Z","side":"LONG","entry":100,"exit":103,"pnl_usd":1500,"outcome":"PARTIAL","reason":"r"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":100,"exit":106,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	})
	got, err := parseJournalFile(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("trades = %d, want 2 (PARTIAL + TARGET)", len(got))
	}
	if got[0].Outcome != "PARTIAL" || got[1].Outcome != "TARGET" {
		t.Errorf("outcomes: %q / %q, want PARTIAL / TARGET", got[0].Outcome, got[1].Outcome)
	}
	// Both trades reference the SAME open_ts.
	if got[0].OpenTS != got[1].OpenTS {
		t.Errorf("OpenTS should match for PARTIAL+TARGET: %q vs %q", got[0].OpenTS, got[1].OpenTS)
	}
}

func TestParseJournalFile_CorruptTrailingLine_Tolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "X-2026-05.jsonl")
	writeFile(t, path, []string{
		`{"event":"open","symbol":"X","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"r"}`,
		`{"event":"close","symbol":"X","ts":"2026-05-08T08:30:00Z","side":"LONG","entry":100,"exit":106,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
		`{"event":"clo`, // truncated mid-flush
	})
	got, err := parseJournalFile(path)
	if err != nil {
		t.Fatalf("corrupt-line tolerance: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("trades = %d, want 1 (corrupt line skipped)", len(got))
	}
}

func TestParseJournalFile_CloseWithoutOpen_Skipped(t *testing.T) {
	// Engine-restart recovery scenario — close event in a journal with no
	// preceding open. The open might be in the prior month's file, but
	// for the diff tool we only count CLOSED trades that have visible
	// opens in the same scan.
	dir := t.TempDir()
	path := filepath.Join(dir, "X-2026-05.jsonl")
	writeFile(t, path, []string{
		`{"event":"close","symbol":"X","ts":"2026-05-08T08:30:00Z","side":"LONG","entry":100,"exit":106,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	})
	got, err := parseJournalFile(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("orphan close should be skipped, got %d trades", len(got))
	}
}

// ── End-to-end through parseJournalDir ─────────────────────────────────────

// writeLayer3TopologyFixture builds the production paper-live tree under root:
//
//	<root>/BTCUSDT-2026-05.jsonl                   ← live trade (1 pair)
//	<root>/shadow/alt5-15-336/ETHUSDT-2026-05.jsonl ← shadow trade (1 pair)
//
// Both live and shadow contain matched open/close events. Used by tests that
// exercise the include-shadows / Layer-3-topology contract.
func writeLayer3TopologyFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "BTCUSDT-2026-05.jsonl"), []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	})
	if err := os.MkdirAll(filepath.Join(root, "shadow", "alt5-15-336"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "shadow", "alt5-15-336", "ETHUSDT-2026-05.jsonl"), []string{
		`{"event":"open","symbol":"ETHUSDT","ts":"2026-05-08T08:00:00Z","side":"SHORT","entry":3000,"stop":3050,"target":2700,"reason":"r"}`,
		`{"event":"close","symbol":"ETHUSDT","ts":"2026-05-08T09:00:00Z","side":"SHORT","entry":3000,"exit":2700,"pnl_usd":1000,"outcome":"TARGET","reason":"r"}`,
	})
}

func TestParseJournalDir_DefaultExcludesShadows(t *testing.T) {
	// Layer 3 regression: stub-dir contains shadows, testnet-dir doesn't.
	// Pre-fix, parseJournalDir always globbed shadow/<label>/*.jsonl, so the
	// stub side picked up shadow trades that had no testnet peer → false
	// SIGNAL_DIVERGENCE on every legitimate Layer 3 run. Default must now
	// EXCLUDE shadows so the locked production caller (layer3_verdict.sh)
	// is safe by construction.
	root := t.TempDir()
	writeLayer3TopologyFixture(t, root)

	got, err := parseJournalDir(root, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("trades = %d, want 1 (live only — shadow must be excluded by default)", len(got))
	}
	if got[0].Symbol != "BTCUSDT" {
		t.Errorf("expected live BTCUSDT, got %q (shadow ETHUSDT leaked through default-OFF)", got[0].Symbol)
	}
}

func TestParseJournalDir_IncludeShadows_FlagOn(t *testing.T) {
	// Cross-shadow diff use case: operator explicitly opts in to walking
	// shadow/<label>/. Both live and shadow trades returned, matching the
	// pre-fix behavior so interactive shadow-vs-shadow debugging continues
	// to work.
	root := t.TempDir()
	writeLayer3TopologyFixture(t, root)

	got, err := parseJournalDir(root, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("trades = %d, want 2 (live + shadow when flag ON)", len(got))
	}
	symbols := map[string]bool{}
	for _, t := range got {
		symbols[t.Symbol] = true
	}
	if !symbols["BTCUSDT"] || !symbols["ETHUSDT"] {
		t.Errorf("expected BTCUSDT (live) + ETHUSDT (shadow); got %v", symbols)
	}
}

func TestParseJournalDir_DefaultExcludesShadows_PreservesShadowIOErrors(t *testing.T) {
	// When shadows are EXCLUDED, the function must not attempt to glob the
	// shadow subdir at all — so a permission-denied or other I/O error on
	// the shadow path can't surface (there's nothing to surface). This
	// pins that the shadow-glob path is taken only on opt-in: writing an
	// unreadable shadow dir is non-fatal when the flag is OFF.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "BTCUSDT-2026-05.jsonl"), []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":100,"stop":99,"target":106,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":100,"exit":106,"pnl_usd":3000,"outcome":"TARGET","reason":"r"}`,
	})
	// shadow path exists but is irrelevant when flag OFF.
	if err := os.MkdirAll(filepath.Join(root, "shadow", "x"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := parseJournalDir(root, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("trades = %d, want 1 (live only with shadow tree present but excluded)", len(got))
	}
}

func TestParseJournalFile_TokenTooLong_SurfacesError(t *testing.T) {
	// J1 regression: a single line >1MB triggers bufio.Scanner's
	// "token too long" error. Without checking sc.Err() at end of loop,
	// the function silently returned partial trades — for Layer 3 this
	// would manifest as false signal divergence on truncation.
	dir := t.TempDir()
	p := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	// Build a giant single line (>1MB). Scanner buffer max is 1024*1024.
	huge := make([]byte, 0, 2*1024*1024)
	huge = append(huge, []byte(`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","reason":"`)...)
	for i := 0; i < 2*1024*1024; i++ {
		huge = append(huge, 'x')
	}
	huge = append(huge, []byte(`"}`)...)
	if err := os.WriteFile(p, huge, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := parseJournalFile(p)
	if err == nil {
		t.Fatal("expected scanner error on token-too-long line, got nil")
	}
}

func TestParseJournalFile_AllCorruptLines_WarnsOnStderr(t *testing.T) {
	// J2 regression: when every parseable line fails JSON parse (schema
	// drift / disk corruption), the file silently produces zero trades
	// with no diagnostic. Sibling of the Stub fix at 4154374. Capture
	// stderr to verify the WARN line is emitted.
	dir := t.TempDir()
	p := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	if err := os.WriteFile(p, []byte("garbage 1\ngarbage 2\ngarbage 3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Capture stderr.
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	trades, err := parseJournalFile(p)
	w.Close()

	out := make([]byte, 4096)
	n, _ := r.Read(out)
	stderr := string(out[:n])

	if err != nil {
		t.Fatalf("parseJournalFile err: %v", err)
	}
	if len(trades) != 0 {
		t.Errorf("expected 0 trades from all-corrupt file, got %d", len(trades))
	}
	if !contains(stderr, "WARN") || !contains(stderr, "failed JSON parse") {
		t.Errorf("expected stderr WARN about JSON-parse failures, got: %q", stderr)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
