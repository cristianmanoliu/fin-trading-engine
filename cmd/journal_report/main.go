// journal_report reads the paper-live JSONL trade journals under logs/journal/
// and prints a per-symbol summary table. For each symbol it also runs the backtest
// over the same date range (using the per-symbol config) and shows a drift column
// so you can verify that live paper behaviour matches backtest expectations.
//
// Usage:
//   go run ./cmd/journal_report [--journal-dir ./logs/journal] [--config-dir ./configs]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

type journalEntry struct {
	Event   string  `json:"event"`
	Symbol  string  `json:"symbol"`
	TS      string  `json:"ts"`
	Side    string  `json:"side"`
	Entry   float64 `json:"entry"`
	Exit    float64 `json:"exit"`
	PnlPts  float64 `json:"pnl_pts"`
	PnlUSD  float64 `json:"pnl_usd"`
	Outcome string  `json:"outcome"`
}

type symbolStats struct {
	trades int
	wins   int
	pnlUSD float64
	minTS  time.Time
	maxTS  time.Time
}

// recoverPanic converts an unrecovered Go panic to exit code 4. Without
// this, a panic exits 2 (Go's default) which is INDISTINGUISHABLE from
// the existing exit codes (1=load error, 3=no data). cron consumers
// can't tell "report ran cleanly" from "report crashed mid-walk."
func recoverPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, debug.Stack())
		os.Exit(4)
	}
}

func main() {
	defer recoverPanic()

	journalDir := flag.String("journal-dir", "./logs/journal", "directory containing JSONL journals")
	configDir := flag.String("config-dir", "./configs", "directory containing per-symbol configs")
	binPath := flag.String("backtest-bin", "./bin/backtest", "path to compiled backtest binary")
	flag.Parse()

	entries, err := os.ReadDir(*journalDir)
	if err != nil {
		// R1: harmonize with the no-data path below — exit 3 (INPUT ERROR)
		// rather than exit 1. cron / CI consumers and the canonical
		// journal_validate / journal_diff contract both use exit 3 for
		// unreadable input; using exit 1 here was an asymmetry that could
		// have routed through a different Telegram tier than the no-data
		// case did.
		slog.Error("cannot read journal dir", "path", *journalDir, "err", err)
		os.Exit(3)
	}

	stats := map[string]*symbolStats{}

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(*journalDir, de.Name())
		f, err := os.Open(path)
		if err != nil {
			slog.Warn("cannot open journal", "file", path, "err", err)
			continue
		}

		sc := bufio.NewScanner(f)
		// Match journal_diff's buffer sizing — default 64KB max line is
		// too small for any future schema with embedded context blobs.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		fileLines, fileCorrupt := 0, 0
		for sc.Scan() {
			fileLines++
			var e journalEntry
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				// R3: track corrupt lines (sibling of journal_diff J2 +
				// Stub fix at 4154374) so an all-corrupt file (schema
				// drift) doesn't silently produce an empty stats table.
				fileCorrupt++
				continue
			}
			if e.Event != "close" {
				continue
			}
			sym := strings.ToUpper(e.Symbol)
			if stats[sym] == nil {
				stats[sym] = &symbolStats{}
			}
			s := stats[sym]
			s.trades++
			s.pnlUSD += e.PnlUSD
			if e.Outcome == "TARGET" {
				s.wins++
			}
			ts, _ := time.Parse(time.RFC3339, e.TS)
			if s.minTS.IsZero() || ts.Before(s.minTS) {
				s.minTS = ts
			}
			if ts.After(s.maxTS) {
				s.maxTS = ts
			}
		}
		// R2: scanner.Err() must be checked. Token-too-long (>1MB single
		// line) would otherwise silently truncate the file's contribution
		// to the report — operator sees correct-looking stats from a
		// truncated parse. Same shape as journal_diff J1.
		if err := sc.Err(); err != nil {
			slog.Warn("scanner error mid-file (some events may have been dropped)",
				"file", path, "err", err, "lines_read", fileLines)
		}
		// R3: all-corrupt warning. Threshold mirrors journal_diff: >50%
		// corrupt OR all-N-corrupt is a likely schema-drift signal.
		if fileLines > 0 && (fileCorrupt == fileLines || float64(fileCorrupt)/float64(fileLines) > 0.5) {
			slog.Warn("file has high JSON-parse failure rate (schema drift?)",
				"file", path, "corrupt", fileCorrupt, "total", fileLines)
		}
		f.Close()
	}

	if len(stats) == 0 {
		fmt.Fprintln(os.Stderr, "No journal data found. Have you run paper_live_start.sh yet?")
		// Exit non-zero so cron/CI consumers don't treat an empty journal
		// as a successful reconciliation. Matches the no-data handling in
		// cmd/journal_validate (exit 3) and cmd/journal_diff's missing-dir
		// fail-fast.
		os.Exit(3)
	}

	// Sort symbols for stable output.
	symbols := make([]string, 0, len(stats))
	for sym := range stats {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols)

	fmt.Printf("\n%-12s  %s\n", "PAPER-LIVE RECONCILIATION REPORT", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Printf("%-12s  %-6s  %-6s  %-8s  %-12s  │  %-6s  %-6s  %-12s  %s\n",
		"SYMBOL", "LIVE_T", "LIVE%", "LIVE_$1k", "RANGE",
		"BT_T", "BT%", "BT_$1k", "DRIFT_T%")
	fmt.Printf("%s\n", strings.Repeat("─", 100))

	for _, sym := range symbols {
		s := stats[sym]
		winPct := 0.0
		if s.trades > 0 {
			winPct = float64(s.wins) / float64(s.trades) * 100
		}
		rangeStr := "?"
		if !s.minTS.IsZero() {
			rangeStr = fmt.Sprintf("%s–%s",
				s.minTS.Format("01/02"),
				s.maxTS.Format("01/02"))
		}

		// Run backtest over the same period if binary is available.
		btTrades, btWinPct, btPnlUSD := runBacktest(sym, s.minTS, s.maxTS, *configDir, *binPath)

		driftStr := "n/a"
		if s.trades > 0 && btTrades > 0 {
			drift := math.Abs(float64(s.trades-btTrades)) / float64(btTrades) * 100
			driftStr = fmt.Sprintf("%.0f%%", drift)
		}

		fmt.Printf("%-12s  %-6d  %5.1f%%  %+10.0f  %-12s  │  %-6d  %5.1f%%  %+10.0f  %s\n",
			sym, s.trades, winPct, s.pnlUSD, rangeStr,
			btTrades, btWinPct, btPnlUSD, driftStr)
	}

	fmt.Printf("\nDrift > 10%% on trade count = investigate before continuing the observation window.\n")
}

type backtestSummary struct {
	TotalTrades int     `json:"total_trades"`
	WinRate     float64 `json:"win_rate_pct"`
	TotalPnlUSD float64 `json:"total_pnl_usd"`
}

func runBacktest(symbol string, from, to time.Time, configDir, binPath string) (trades int, winPct, pnlUSD float64) {
	if from.IsZero() || to.IsZero() {
		return
	}
	if _, err := os.Stat(binPath); err != nil {
		return // binary not built yet
	}

	sym := strings.ToUpper(symbol)
	cfgLower := strings.ToLower(sym)
	cfg := filepath.Join(configDir, cfgLower+".yaml")
	if _, err := os.Stat(cfg); err != nil {
		return
	}

	// Run backtest for each calendar month in [from, to], accumulate results.
	cur := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)

	var totalTrades, totalWins int
	var totalPnl float64

	for !cur.After(end) {
		year := cur.Format("2006")
		month := cur.Format("01")
		out, err := exec.Command(binPath,
			"--config", cfg,
			"--year", year,
			"--month", month,
		).Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if !strings.Contains(line, "BACKTEST SUMMARY") {
					continue
				}
				// Extract JSON object from slog output.
				idx := strings.Index(line, "{")
				if idx < 0 {
					continue
				}
				var s backtestSummary
				if err := json.Unmarshal([]byte(line[idx:]), &s); err == nil {
					totalTrades += s.TotalTrades
					totalWins += int(math.Round(float64(s.TotalTrades) * s.WinRate / 100))
					totalPnl += s.TotalPnlUSD
				}
			}
		}
		cur = cur.AddDate(0, 1, 0)
	}

	if totalTrades > 0 {
		return totalTrades, float64(totalWins) / float64(totalTrades) * 100, totalPnl
	}
	return
}
