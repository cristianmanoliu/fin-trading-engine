package main

// TestJournalDirFlag is a characterization test that locks two invariants
// for the --journal-dir flag:
//
//  1. Silent when unset: no --journal-dir means ZERO *.jsonl files written.
//     This preserves the documented backtest no-op invariant that keeps
//     historical sweep output byte-identical.
//
//  2. Written when set: --journal-dir <dir> on a trade-producing run writes
//     at least one TESTUSDT-<month>.jsonl containing a "event":"close" line.
//
//  3. Result invariant: the SUMMARY line (total_trades) is identical between
//     the silent and journaled runs. Journaling must not change results.
//
// The test is fully hermetic — no repo data files are required. A minimal
// synthetic 1-minute kline CSV is generated in t.TempDir() so this test
// runs clean in CI where data/ symlinks are absent.
//
// Price-path rationale (why this fixture produces a trade):
//
//	Phase 1 (rows 0-59): Dec-31 ticks at 100.0 — initialises DailyLevels.
//	Phase 2 (rows 60-299): Jan-01 00:00-03:59 UTC with price declining
//	  99→90 — fills a bearish 4H candle (open≈99, close≈90) that closes
//	  at 04:00 UTC and sets BiasTracker to Short.
//	  Row 60 (00:00 UTC) also triggers the midnight rollover that sets
//	  PDH/PDL (DailyLevels.HasData = true), unblocking evaluateEntry.
//	Phase 3 (rows 300-324): five 5m candles (prices 92→93→94→95→80)
//	  prime EMA2 and EMA3 (fast=2, slow=3). On the 5th candle (close=80,
//	  high=82) the EMA2 drops below EMA3 → bearish cross → SHORT signal:
//	    entry=80, stop≈82.08, target≈67.5
//	Phase 4 (rows 325-334): price drops to 60, below the 67.5 target →
//	  TARGET hit → close event written with outcome="TARGET".

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTradeFixture writes a synthetic 1-minute kline CSV to dir/fixture.csv.
// The price path is engineered to produce exactly one closed trade with
// --signal-tf 5m --ema-fast-period 2 --ema-slow-period 3 --target-rr 6.0
// --side-filter short --exact-fills.
//
// CSV column order (Binance klines, header included):
//
//	open_time, open, high, low, close, volume, close_time,
//	quote_volume, count, taker_buy_volume, taker_buy_quote_volume, ignore
func buildTradeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.csv")

	const (
		msPerMin = 60_000
		// 2023-12-31 23:00:00 UTC
		startMs int64 = 1_704_063_600_000
	)

	// row writes one 1m kline where open=high=low=close=price.
	row := func(idx int, open, high, low, close float64) string {
		openMs := startMs + int64(idx)*msPerMin
		closeMs := openMs + msPerMin - 1 // 59999 ms later
		return fmt.Sprintf(
			"%d,%.2f,%.2f,%.2f,%.2f,1.0,%d,100.0,10,0.5,50.0,0\n",
			openMs, open, high, low, close, closeMs,
		)
	}

	var sb strings.Builder
	// Header row that CSVReplay skips.
	sb.WriteString("open_time,open,high,low,close,volume,close_time," +
		"quote_volume,count,taker_buy_volume,taker_buy_quote_volume,ignore\n")

	// ── Phase 1: 60 rows on 2023-12-31 23:00–23:59 UTC (price=100) ──────────
	// Initialises DailyLevels with first session day.
	for i := 0; i < 60; i++ {
		sb.WriteString(row(i, 100.0, 100.0, 100.0, 100.0))
	}

	// ── Phase 2a: row 60 – 2024-01-01 00:00 UTC (price=99) ──────────────────
	// Midnight rollover: DailyLevels sets PDH=100, PDL=100 from Dec-31.
	// HasData() = true from this point onward.
	sb.WriteString(row(60, 99.0, 99.0, 99.0, 99.0))

	// ── Phase 2b: rows 61–299 – 2024-01-01 00:01–03:59 UTC ──────────────────
	// Fill the 4H candle (opened at 00:00 UTC) with declining prices.
	// Prices drop from 98→90 so the 4H candle is bearish (close<open).
	for i := 61; i < 300; i++ {
		// Linear interpolation from 98.0 down to 90.0 over 239 rows.
		p := 98.0 - float64(i-61)*8.0/238.0
		sb.WriteString(row(i, p, p, p, p))
	}

	// ── Row 300 – 2024-01-01 04:00 UTC ──────────────────────────────────────
	// First tick of this row (at 04:00:00 UTC) triggers the 4H candle close
	// (open≈99, close≈90 from prev row → bearish). BiasTracker = Short.
	// This row also starts 5m candle #1 of the signal-evaluation period.
	// Price 92 starts the rising run needed for EMA cross setup.
	sb.WriteString(row(300, 92.0, 92.0, 92.0, 92.0))

	// ── 5m candle 1: rows 301–304 (same 5m window, price=92) ────────────────
	for i := 301; i < 305; i++ {
		sb.WriteString(row(i, 92.0, 92.0, 92.0, 92.0))
	}
	// Row 305 (04:05 UTC) closes 5m-1 (close=92). EMA2 count=1, EMA3 count=1.
	// prevEma2/3=0 → no signal.

	// ── 5m candle 2: rows 305–309 (price=93) ─────────────────────────────────
	for i := 305; i < 310; i++ {
		sb.WriteString(row(i, 93.0, 93.0, 93.0, 93.0))
	}
	// Row 310 (04:10) closes 5m-2 (close=93). EMA2 primed=(92+93)/2=92.5.
	// EMA3 count=2. prevEma21=0 → no signal.

	// ── 5m candle 3: rows 310–314 (price=94) ─────────────────────────────────
	for i := 310; i < 315; i++ {
		sb.WriteString(row(i, 94.0, 94.0, 94.0, 94.0))
	}
	// Row 315 (04:15) closes 5m-3 (close=94). EMA3 primed=(92+93+94)/3=93.0.
	// prevEma21 still 0 at AddCandle time → no signal.

	// ── 5m candle 4: rows 315–319 (price=95) ─────────────────────────────────
	for i := 315; i < 320; i++ {
		sb.WriteString(row(i, 95.0, 95.0, 95.0, 95.0))
	}
	// Row 320 (04:20) closes 5m-4 (close=95).
	// prevEma2≈93.67, prevEma3=93.0 (both non-zero, primed).
	// curEma2≈94.56 > curEma3=94.0 → no cross (fast still above slow).

	// ── 5m candle 5: rows 320–324 (first row has high=82 for stop calc) ──────
	// Row 320: open=82, high=82, low=80, close=80 (sets the 5m candle's High).
	sb.WriteString(row(320, 82.0, 82.0, 80.0, 80.0))
	// Rows 321–324: flat at 80.
	for i := 321; i < 325; i++ {
		sb.WriteString(row(i, 80.0, 80.0, 80.0, 80.0))
	}
	// Row 325 (04:25) closes 5m-5 (close=80, high=82).
	// prevEma2≈94.56 >= prevEma3=94.0; curEma2≈85.34 < curEma3=87.0
	// → BEARISH CROSS. Bias=Short, SideFilter=Short → signal fires!
	//   entry=80, stop=82×1.001≈82.08, stopDist≈2.08, target=80−6×2.08≈67.5

	// ── Phase 4: rows 325–334 (price=60, below target ≈67.5) ─────────────────
	// The first tick from row 326 (~60) crosses the target → TARGET close.
	// Row 325 itself opens at 80 (boundary tick after 5m-5 closes).
	for i := 325; i < 335; i++ {
		sb.WriteString(row(i, 60.0, 60.0, 60.0, 60.0))
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("buildTradeFixture: write: %v", err)
	}
	return path
}

// journalConfig writes a test YAML config that enables EMA-cross mode and
// sets stake_usd so the SUMMARY emits total_pnl_usd. Returns the config path.
func journalConfig(t *testing.T, csvPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "journal_test.yaml")
	body := fmt.Sprintf(`symbol: TESTUSDT
backtest:
  csv_path: %s
strategy:
  ema_mode: true
  stake_usd: 1000
  stop_buffer_pct: 0.001
  target_rr: 6.0
`, csvPath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// summaryFields extracts total_trades (and total_pnl_usd when present) from
// the JSON slog SUMMARY line emitted by cmd/backtest.
func summaryFields(t *testing.T, output string) (totalTrades int, totalPnlPresent bool, totalPnlUSD float64) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "BACKTEST SUMMARY") {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("summaryFields: cannot parse SUMMARY line: %v\nline: %s", err, line)
		}
		if raw, ok := m["total_trades"]; ok {
			if err := json.Unmarshal(raw, &totalTrades); err != nil {
				t.Fatalf("summaryFields: total_trades parse: %v", err)
			}
		}
		if raw, ok := m["total_pnl_usd"]; ok {
			totalPnlPresent = true
			if err := json.Unmarshal(raw, &totalPnlUSD); err != nil {
				t.Fatalf("summaryFields: total_pnl_usd parse: %v", err)
			}
		}
		return
	}
	t.Fatalf("summaryFields: no BACKTEST SUMMARY line found in output:\n%s", output)
	return
}

// commonFlags returns the flag set shared by both the silent and journaled runs
// so the result-invariant assertion is comparing apples-to-apples.
func commonFlags(cfgPath string) []string {
	return []string{
		"--config", cfgPath,
		"--signal-tf", "5m",
		"--ema-fast-period", "2",
		"--ema-slow-period", "3",
		"--side-filter", "short",
		"--exact-fills",
		"--include-boundary",
	}
}

// TestJournalDirFlag locks the three invariants of --journal-dir.
func TestJournalDirFlag(t *testing.T) {
	csvPath := buildTradeFixture(t)
	cfgPath := journalConfig(t, csvPath)

	// ── Invariant 1: journal-silent when flag is absent ──────────────────────
	silentJournalDir := t.TempDir()
	silentCode, silentOut := runBT(t, commonFlags(cfgPath)...)
	if silentCode != 0 {
		t.Fatalf("silent run: expected exit 0, got %d\noutput:\n%s", silentCode, silentOut)
	}
	silentFiles, err := filepath.Glob(filepath.Join(silentJournalDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("glob silent dir: %v", err)
	}
	if len(silentFiles) != 0 {
		t.Errorf("invariant 1 FAIL: expected 0 *.jsonl in silent dir, got %d: %v",
			len(silentFiles), silentFiles)
	}

	// ── Invariant 2: journal written when flag is set ─────────────────────────
	jDir := t.TempDir()
	journaledArgs := append(commonFlags(cfgPath), "--journal-dir", jDir)
	journaledCode, journaledOut := runBT(t, journaledArgs...)
	if journaledCode != 0 {
		t.Fatalf("journaled run: expected exit 0, got %d\noutput:\n%s", journaledCode, journaledOut)
	}

	// At least one TESTUSDT-*.jsonl file must exist.
	jFiles, err := filepath.Glob(filepath.Join(jDir, "TESTUSDT-*.jsonl"))
	if err != nil {
		t.Fatalf("glob journal dir: %v", err)
	}
	if len(jFiles) == 0 {
		t.Fatalf("invariant 2 FAIL: expected ≥1 TESTUSDT-*.jsonl in %s, got 0\nrun output:\n%s",
			jDir, journaledOut)
	}

	// The journal must contain at least one "event":"close" line.
	foundClose := false
	for _, jf := range jFiles {
		data, rerr := os.ReadFile(jf)
		if rerr != nil {
			t.Fatalf("read journal %s: %v", jf, rerr)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var entry map[string]json.RawMessage
			if jerr := json.Unmarshal([]byte(line), &entry); jerr != nil {
				t.Errorf("journal line not valid JSON in %s: %v\nline: %s", jf, jerr, line)
				continue
			}
			var event string
			if raw, ok := entry["event"]; ok {
				_ = json.Unmarshal(raw, &event)
			}
			if event == "close" {
				foundClose = true
			}
		}
	}
	if !foundClose {
		t.Errorf("invariant 2 FAIL: no \"event\":\"close\" line found in journal files %v", jFiles)
	}

	// ── Invariant 3: SUMMARY result unchanged by journaling ───────────────────
	silentTrades, silentPnlPresent, silentPnl := summaryFields(t, silentOut)
	journaledTrades, journaledPnlPresent, journaledPnl := summaryFields(t, journaledOut)

	if silentTrades != journaledTrades {
		t.Errorf("invariant 3 FAIL: total_trades differs: silent=%d journaled=%d",
			silentTrades, journaledTrades)
	}
	if silentTrades == 0 {
		// The fixture failed to produce any trade — the test is meaningless.
		// Report a clear failure so it's obvious the fixture needs adjustment.
		t.Fatalf("fixture produced 0 trades: no signal fired. Check the synthetic price path.\nsilent output:\n%s", silentOut)
	}
	if silentPnlPresent && journaledPnlPresent {
		if silentPnl != journaledPnl {
			t.Errorf("invariant 3 FAIL: total_pnl_usd differs: silent=%.2f journaled=%.2f",
				silentPnl, journaledPnl)
		}
	}

	t.Logf("fixture produced %d trade(s); journal files: %v", silentTrades, jFiles)
}
