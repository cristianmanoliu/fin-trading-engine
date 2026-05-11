// Command journal_diff is the Layer 3 shadow-mode parity gate tool.
//
// Per the locked rule
// (results/real_money_executor_architecture_decision_rule_2026-05-08.md
// "Layer 3 — Production shadow mode"), before flipping any engine to
// BinanceLive in production, the operator runs BinanceLive in PARALLEL on
// testnet alongside the existing Stub on the same input ticks for ≥7 days.
// The acceptance criterion is:
//
//   - pnl_usd per closed trade differs by ≤ 0.5%
//   - no signal-generation divergence (both executors fire the same trades)
//
// This tool does the diff. Inputs are two journal directories (e.g.,
// /var/log/paper-live/journal/  and  /var/log/paper-live/journal-testnet/).
// For each pair of closed trades that match by (symbol, open_ts, side), the
// tool computes |pnl_usd_a − pnl_usd_b| / max(|pnl_usd_a|, |pnl_usd_b|) ×
// 100 and flags violations of the threshold.
//
// Exit codes:
//   0  PASS: all matched pairs within threshold AND no signal divergence
//   1  THRESHOLD VIOLATION: ≥1 matched pair exceeds threshold
//   2  SIGNAL DIVERGENCE: ≥1 closed trade in one journal has no peer in the
//      other (the two executors disagreed on whether to trade)
//   3  INPUT ERROR (bad flags / unreadable journal / corrupt entries beyond
//      the corrupt-trailing-line tolerance)
//
// Both 1 and 2 mean Layer 3 FAILED — operator must NOT promote to STAGE_1
// and must investigate.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// journalEntry mirrors the schema written by pkg/execution. Duplicated here
// because cmd/journal_diff intentionally does NOT import pkg/execution —
// that's an internal package detail; this tool reads the JSONL on-disk
// surface, which IS the locked contract.
type journalEntry struct {
	Event   string  `json:"event"`
	Symbol  string  `json:"symbol"`
	TS      string  `json:"ts"`
	Side    string  `json:"side"`
	Entry   float64 `json:"entry"`
	Exit    float64 `json:"exit,omitempty"`
	Stop    float64 `json:"stop"`
	Target  float64 `json:"target"`
	PnlPts  float64 `json:"pnl_pts,omitempty"`
	PnlUSD  float64 `json:"pnl_usd,omitempty"`
	Outcome string  `json:"outcome,omitempty"`
	Reason  string  `json:"reason"`
}

// trade pairs an open event with its corresponding close event. Built by
// walking a journal in order: each "close" attaches to the most recent
// unclosed "open".
type trade struct {
	Symbol  string
	OpenTS  string  // RFC3339, the matching key across journals
	Side    string  // "LONG" | "SHORT"
	Entry   float64
	Exit    float64
	PnlUSD  float64
	Outcome string  // "TARGET" | "STOP" | "PARTIAL" | "TIME"
}

// tradeKey is the across-journal matching identifier. Open timestamp is
// stable (= signal.Timestamp = candle close time, set by the strategy
// goroutine before the executor sees it). Close timestamps would differ
// across executors due to OnTick latency — explicitly NOT used for matching.
type tradeKey struct {
	Symbol string
	OpenTS string
	Side   string
}

func (t trade) key() tradeKey {
	return tradeKey{Symbol: t.Symbol, OpenTS: t.OpenTS, Side: t.Side}
}

// parseJournalDir reads every *.jsonl file under dir and returns the trades.
// PARTIAL closes do NOT terminate a trade — the same open can produce both
// a PARTIAL close and a subsequent terminal close. Both close events go to
// separate trade records.
//
// Validates dir existence up-front: a typo'd path would otherwise produce
// zero trades on both sides → false-positive Layer 3 PASS, silently
// failing the gate open. This is the only failure mode that could
// propagate past the operator into a real-money promotion decision.
func parseJournalDir(dir string) ([]trade, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", dir, err)
	}
	// Also pick up shadow subdirs (shadow/<label>/*.jsonl) so the tool
	// covers full-fleet diffs in one pass. Symmetric error handling with
	// the primary glob — a permission-denied or other I/O error during
	// the shadow walk used to be silently dropped (J3 finding), which
	// would surface as missing trades on Layer 3 → false-positive
	// signal divergence → wrongly-blocked promotion.
	shadowMatches, err := filepath.Glob(filepath.Join(dir, "shadow", "*", "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob %s/shadow: %w", dir, err)
	}
	matches = append(matches, shadowMatches...)
	sort.Strings(matches) // deterministic order

	var all []trade
	for _, fname := range matches {
		ts, err := parseJournalFile(fname)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", fname, err)
		}
		all = append(all, ts...)
	}
	return all, nil
}

// parseJournalFile walks one journal file, pairing opens with closes.
// Tolerates corrupt lines (engine killed mid-flush, schema drift) by
// counting them; emits a stderr warning if the corrupt count is
// suspiciously high so silent format-change → empty-trade-list →
// false-PASS doesn't slip past the gate. Sibling fix to the Stub
// corrupt-counter pattern locked at 4154374. Scanner err is checked
// at end-of-loop so a token-too-long line (>1MB) surfaces as an
// I/O error rather than silently truncating the trade list — the
// latter would produce false signal divergence on Layer 3.
func parseJournalFile(path string) ([]trade, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var trades []trade
	var openEntry *journalEntry
	totalLines, corruptLines := 0, 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		totalLines++
		var e journalEntry
		if jerr := json.Unmarshal(line, &e); jerr != nil {
			// Corrupt line — skip, mirrors Stub recovery tolerance,
			// but track count so an all-corrupt file (schema drift,
			// disk corruption) surfaces a warning rather than an empty
			// trade list silently passing as PASS.
			corruptLines++
			continue
		}
		switch e.Event {
		case "open":
			eCopy := e
			openEntry = &eCopy
		case "close":
			if openEntry == nil {
				// Close without preceding open — recovery scenario where the
				// engine restarted mid-trade. Skip; tool reports trade-level
				// only and the open isn't visible without prior-month data.
				continue
			}
			trades = append(trades, trade{
				Symbol:  e.Symbol,
				OpenTS:  openEntry.TS,
				Side:    e.Side,
				Entry:   e.Entry,
				Exit:    e.Exit,
				PnlUSD:  e.PnlUSD,
				Outcome: e.Outcome,
			})
			// Only PARTIAL keeps the open alive; full closes terminate.
			if e.Outcome != "PARTIAL" {
				openEntry = nil
			}
		}
	}
	// J1: scanner error must surface — token-too-long (>1MB single line)
	// would otherwise leave the loop silently with the trades collected
	// so far, propagating partial truncation as success. For the Layer 3
	// parity gate this would manifest as false signal divergence (one
	// side's truncation produces unmatched trades).
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scanner: %w", err)
	}
	// All-corrupt warning (Stub-pattern sibling at 4154374). totalLines
	// counts only non-empty lines; corruptLines tracks failed JSON parses.
	// Threshold: > 50% corrupt OR all-N-corrupt with N > 0.
	if totalLines > 0 && (corruptLines == totalLines || float64(corruptLines)/float64(totalLines) > 0.5) {
		fmt.Fprintf(os.Stderr,
			"WARN: %s — %d/%d lines failed JSON parse (schema drift? disk corruption?)\n",
			path, corruptLines, totalLines)
	}
	return trades, nil
}

// pairing is one across-journal trade match.
type pairing struct {
	Key    tradeKey
	A      *trade // from --dir-a (typically the Stub paper-money journal)
	B      *trade // from --dir-b (typically the BinanceLive testnet journal)
	DiffPct float64
}

// matchPairs builds the trade-pair set: matched (both A and B), only-A
// (A had a trade B didn't = signal divergence), only-B (mirror).
func matchPairs(aTrades, bTrades []trade) (matched []pairing, onlyA, onlyB []*trade) {
	bIndex := make(map[tradeKey]*trade, len(bTrades))
	for i := range bTrades {
		bIndex[bTrades[i].key()] = &bTrades[i]
	}
	consumed := make(map[tradeKey]bool, len(bTrades))

	for i := range aTrades {
		a := &aTrades[i]
		k := a.key()
		if b, ok := bIndex[k]; ok && !consumed[k] {
			matched = append(matched, pairing{
				Key:     k,
				A:       a,
				B:       b,
				DiffPct: pnlDiffPct(a.PnlUSD, b.PnlUSD),
			})
			consumed[k] = true
		} else {
			ac := *a
			onlyA = append(onlyA, &ac)
		}
	}
	for i := range bTrades {
		b := &bTrades[i]
		if !consumed[b.key()] {
			bc := *b
			onlyB = append(onlyB, &bc)
		}
	}
	return matched, onlyA, onlyB
}

// pnlDiffPct returns |a − b| / max(|a|, |b|) × 100. Returns 0 when both
// sides are zero (treated as identical no-op closes); returns 100 when
// only one side is zero (max divergence — flag).
func pnlDiffPct(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	denom := math.Max(math.Abs(a), math.Abs(b))
	if denom == 0 {
		return 0
	}
	return math.Abs(a-b) / denom * 100
}

func main() {
	var (
		dirA         = flag.String("dir-a", "", "first journal directory (typically Stub paper-money)")
		dirB         = flag.String("dir-b", "", "second journal directory (typically BinanceLive testnet)")
		thresholdPct = flag.Float64("threshold-pct", 0.5, "max allowed pnl_usd diff per pair (locked: 0.5)")
		labelA       = flag.String("label-a", "A", "label for journal A in the report")
		labelB       = flag.String("label-b", "B", "label for journal B in the report")
		verbose      = flag.Bool("verbose", false, "print every matched pair, not just violations")
	)
	flag.Parse()

	if *dirA == "" || *dirB == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --dir-a and --dir-b are required")
		flag.Usage()
		os.Exit(3)
	}

	aTrades, err := parseJournalDir(*dirA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR reading --dir-a: %v\n", err)
		os.Exit(3)
	}
	bTrades, err := parseJournalDir(*dirB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR reading --dir-b: %v\n", err)
		os.Exit(3)
	}

	matched, onlyA, onlyB := matchPairs(aTrades, bTrades)
	report(matched, onlyA, onlyB, *labelA, *labelB, *thresholdPct, *verbose)

	// Exit code precedence: signal divergence beats threshold (it's a more
	// fundamental failure — the executors aren't firing on the same signals).
	if len(onlyA) > 0 || len(onlyB) > 0 {
		os.Exit(2)
	}
	for _, p := range matched {
		if p.DiffPct > *thresholdPct {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func report(matched []pairing, onlyA, onlyB []*trade, labelA, labelB string, threshold float64, verbose bool) {
	fmt.Println(strings.Repeat("=", 78))
	fmt.Printf(" Layer 3 parity diff — %s vs %s (threshold=%.2f%% per pair)\n", labelA, labelB, threshold)
	fmt.Println(strings.Repeat("=", 78))

	fmt.Printf("\n  Matched pairs:        %d\n", len(matched))
	fmt.Printf("  Only-%s (signal div): %d\n", labelA, len(onlyA))
	fmt.Printf("  Only-%s (signal div): %d\n", labelB, len(onlyB))

	if len(matched) > 0 {
		var maxDiff, sumDiff float64
		var violations int
		for _, p := range matched {
			if p.DiffPct > maxDiff {
				maxDiff = p.DiffPct
			}
			sumDiff += p.DiffPct
			if p.DiffPct > threshold {
				violations++
			}
		}
		meanDiff := sumDiff / float64(len(matched))
		fmt.Printf("\n  Mean diff:  %.4f%%\n", meanDiff)
		fmt.Printf("  Max diff:   %.4f%%\n", maxDiff)
		fmt.Printf("  Violations: %d / %d (%.1f%%)\n", violations, len(matched),
			100*float64(violations)/float64(len(matched)))
	}

	// Per-symbol summary.
	if len(matched) > 0 {
		fmt.Println("\n  Per-symbol matched-pair stats:")
		bySymbol := make(map[string][]pairing)
		for _, p := range matched {
			bySymbol[p.Key.Symbol] = append(bySymbol[p.Key.Symbol], p)
		}
		symbols := make([]string, 0, len(bySymbol))
		for s := range bySymbol {
			symbols = append(symbols, s)
		}
		sort.Strings(symbols)
		fmt.Printf("    %-12s  %-8s  %-12s  %-12s  %s\n", "SYMBOL", "PAIRS", "MAX DIFF%", "MEAN DIFF%", "VIOLATIONS")
		for _, sym := range symbols {
			ps := bySymbol[sym]
			var maxD, sumD float64
			var viol int
			for _, p := range ps {
				if p.DiffPct > maxD {
					maxD = p.DiffPct
				}
				sumD += p.DiffPct
				if p.DiffPct > threshold {
					viol++
				}
			}
			fmt.Printf("    %-12s  %-8d  %-12.4f  %-12.4f  %d\n",
				sym, len(ps), maxD, sumD/float64(len(ps)), viol)
		}
	}

	// Print violations.
	violations := 0
	for _, p := range matched {
		if p.DiffPct > threshold {
			if violations == 0 {
				fmt.Printf("\n  Threshold violations (>%.2f%%):\n", threshold)
			}
			fmt.Printf("    %s %-7s open=%s  pnl(%s)=%v  pnl(%s)=%v  diff=%.4f%%\n",
				p.Key.Symbol, p.Key.Side, p.Key.OpenTS,
				labelA, p.A.PnlUSD, labelB, p.B.PnlUSD, p.DiffPct)
			violations++
		}
	}

	if verbose && len(matched) > 0 {
		fmt.Println("\n  All matched pairs:")
		for _, p := range matched {
			fmt.Printf("    %s %-7s open=%s  pnl(%s)=%v  pnl(%s)=%v  diff=%.4f%%\n",
				p.Key.Symbol, p.Key.Side, p.Key.OpenTS,
				labelA, p.A.PnlUSD, labelB, p.B.PnlUSD, p.DiffPct)
		}
	}

	if len(onlyA) > 0 {
		fmt.Printf("\n  Trades only in %s (%s closed but %s did not):\n", labelA, labelA, labelB)
		for _, t := range onlyA {
			fmt.Printf("    %s %-7s open=%s  pnl=%v  outcome=%s\n",
				t.Symbol, t.Side, t.OpenTS, t.PnlUSD, t.Outcome)
		}
	}
	if len(onlyB) > 0 {
		fmt.Printf("\n  Trades only in %s (%s closed but %s did not):\n", labelB, labelB, labelA)
		for _, t := range onlyB {
			fmt.Printf("    %s %-7s open=%s  pnl=%v  outcome=%s\n",
				t.Symbol, t.Side, t.OpenTS, t.PnlUSD, t.Outcome)
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 78))
	switch {
	case len(onlyA) > 0 || len(onlyB) > 0:
		fmt.Println(" VERDICT: SIGNAL DIVERGENCE — Layer 3 FAIL (executors disagreed on signals)")
	case violations > 0:
		fmt.Println(" VERDICT: THRESHOLD VIOLATION — Layer 3 FAIL (pnl drift > threshold)")
	default:
		fmt.Println(" VERDICT: PASS — Layer 3 parity criterion met")
	}
	fmt.Println(strings.Repeat("=", 78))
}
