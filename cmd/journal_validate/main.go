// Command journal_validate checks self-consistency invariants on the
// paper-live JSONL trade journals.
//
// Why: the drift detector + forward_paper_status both READ journals to
// produce decision-grade signals (drift verdict) and operator-facing views
// (forward-paper progress). Silent journal corruption — duplicate opens
// from a recovery anomaly, out-of-order timestamps from a clock skew,
// malformed JSON from a partial flush — would poison both. Bug 6 (orphan
// recovery) was an instance of this class; that's now fixed but other
// instances could emerge. This validator catches them mechanically.
//
// Invariants checked (per file):
//
//  1. Monotonic timestamps within a file (engine writes chronologically;
//     decreasing ts = data corruption).
//  2. Every "close" event has a preceding "open" event for the same
//     symbol earlier in the same file (close-without-open = impossible
//     state, indicates a write race or a manually-edited journal).
//  3. No duplicate "open" events for the same (symbol, ts) within a file
//     (recovery firing twice would produce this).
//
// Tolerated (not flagged):
//   - Single trailing malformed line (matches the engine's recovery
//     tolerance for crash-mid-flush).
//   - Open-without-close at end of file (position is still in flight).
//   - PARTIAL close followed by full close (intentional B2 mid-R closes).
//
// Exit codes:
//
//	0  CLEAN: no issues
//	1  WARN-only: warnings present but no errors (e.g., trailing malformed
//	   line). With --strict, this becomes exit 2.
//	2  ERROR: at least one invariant violated
//	3  USAGE / I/O error
//
// Usage:
//
//	journal_validate --dir /var/log/paper-live/journal
//	journal_validate --dir ./logs/journal --strict
//	journal_validate --dir /var/log/paper-live/journal --verbose
//
// The --dir is walked recursively so shadow journals under shadow/<label>/
// are picked up automatically. Files matching *.jsonl are validated.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
)

// entry mirrors the on-disk journal schema (a strict subset of the engine's
// fields — only what the validator inspects). Decoupled from pkg/execution
// because the JSONL surface IS the locked contract; the validator should
// not break if pkg/execution adds new optional fields.
type entry struct {
	Event   string  `json:"event"`
	Symbol  string  `json:"symbol"`
	TS      string  `json:"ts"`
	Outcome string  `json:"outcome,omitempty"`

	// Cost-decomposition fields (close events only, schema from commit
	// 7939786). Invariant per pkg/execution/stub.go:718:
	//   PnlUSD == GrossUSD - FeeUSD - SlipUSD - FundingUSD
	// Each engine-side field is rounded to cents (math.Round(*100)/100).
	// Validator gates the invariant check on Notional > 0 to skip
	// pre-decomp closes where all cost fields are zero.
	GrossUSD   float64 `json:"gross_usd,omitempty"`
	FeeUSD     float64 `json:"fee_usd,omitempty"`
	SlipUSD    float64 `json:"slip_usd,omitempty"`
	FundingUSD float64 `json:"funding_usd,omitempty"`
	Notional   float64 `json:"notional_usd,omitempty"`
	PnlUSD     float64 `json:"pnl_usd,omitempty"`
}

// issue is a single validation finding.
type issue struct {
	Severity string // "ERROR" | "WARN"
	File     string
	Line     int
	Msg      string
}

// recoverPanic converts an unrecovered Go panic to exit code 4 with a
// clear "PANIC" stderr message, distinct from the documented ERROR exit
// code (2). Without this, a panic would exit 2 (Go's default) and
// collide with weekly_audit's CRITICAL "journal_validate found errors"
// classifier — Telegram-tier dual sense. Exit 4 routes through the
// classifier's UNEXPECTED branch (WARN) with correct text.
func recoverPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, debug.Stack())
		os.Exit(4)
	}
}

func main() {
	defer recoverPanic()

	dir := flag.String("dir", "", "journal directory (recursively walked for *.jsonl files)")
	exclude := flag.String("exclude", "", "comma-separated path substrings to skip (e.g., 'archive,old_runs'); useful for frozen historical journals whose pre-fix issues are known and immutable")
	strict := flag.Bool("strict", false, "treat warnings as errors (exit 2 instead of 1)")
	verbose := flag.Bool("verbose", false, "print files with no issues too (default: only print files with issues)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `journal_validate: self-consistency checker for paper-live JSONL trade journals.

Invariants checked (per-cohort-per-symbol stream, cross-month aware):
  • timestamps monotonic within each file
  • no duplicate (symbol, ts) opens (recovery firing twice would emit this)
  • close events have a preceding in-flight open (cross-file aware so a
    close in month-N+1 matching an open in month-N doesn't false-positive)
  • no open while one is already in flight for the same symbol+cohort
  • cost-decomp invariant on closes: pnl_usd ≈ gross_usd - fee_usd - slip_usd - funding_usd
    (gated on notional_usd > 0 to skip pre-decomp closes; 5-cent tolerance)

Tolerated (matches engine recovery semantics):
  • single trailing malformed line → WARN (engine recovery accepts this for crash-mid-flush)
  • open without close at end-of-stream (position still in flight)
  • PARTIAL close followed by terminal close (B2 mid-R partial-take)

Exit codes:
  0  CLEAN              no issues
  1  WARN-only          warnings present, no errors (--strict promotes to 2)
  2  ERROR              at least one invariant violated
  3  USAGE / I/O        bad flags / unreadable directory / no jsonl files

Examples:
  journal_validate --dir /var/log/paper-live/journal --exclude archive
  journal_validate --dir ./logs/journal --strict
  journal_validate --dir ./logs/journal --verbose

Flags:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "--dir is required")
		flag.Usage()
		os.Exit(3)
	}

	var excludes []string
	if *exclude != "" {
		for _, s := range strings.Split(*exclude, ",") {
			if s = strings.TrimSpace(s); s != "" {
				excludes = append(excludes, s)
			}
		}
	}

	files, err := discoverJournals(*dir, excludes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover: %v\n", err)
		os.Exit(3)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no *.jsonl files under %s\n", *dir)
		os.Exit(3)
	}

	// Group files by (parent_dir, symbol). The parent_dir distinguishes
	// cohort (live vs shadow/<label>); the symbol is parsed from the
	// filename. Files within a group are processed in chronological order
	// (filenames sort lexically because YYYY-MM is well-ordered) so a
	// position opened in month-N and closed in month-N+1 doesn't trigger
	// a spurious "close without open" error.
	groups := groupBySymbolCohort(files)

	totalErr, totalWarn := 0, 0
	for _, group := range groups {
		state := newSymbolState()
		for _, fp := range group {
			issues, err := validateFile(fp, state)
			if err != nil {
				fmt.Printf("[ERROR] %s: %v\n", fp, err)
				totalErr++
				continue
			}
			if len(issues) == 0 {
				if *verbose {
					fmt.Printf("[OK] %s\n", fp)
				}
				continue
			}
			for _, iss := range issues {
				fmt.Printf("[%s] %s:%d  %s\n", iss.Severity, iss.File, iss.Line, iss.Msg)
				if iss.Severity == "ERROR" {
					totalErr++
				} else {
					totalWarn++
				}
			}
		}
	}

	fmt.Printf("\n%d file(s) scanned · %d error(s) · %d warning(s)\n", len(files), totalErr, totalWarn)

	switch {
	case totalErr > 0:
		os.Exit(2)
	case *strict && totalWarn > 0:
		os.Exit(2)
	case totalWarn > 0:
		os.Exit(1)
	default:
		os.Exit(0)
	}
}

// discoverJournals walks dir and returns sorted *.jsonl paths, skipping any
// path whose contents include any of the given substring patterns. Empty
// excludes = no filtering.
func discoverJournals(dir string, excludes []string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		for _, ex := range excludes {
			if strings.Contains(p, ex) {
				return nil
			}
		}
		out = append(out, p)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// symbolState tracks per-symbol in-flight position state across files for
// the same (cohort, symbol) group. Carried across month-boundary file
// transitions so a position opened in month-N and closed in month-N+1
// doesn't trigger a spurious "close without open" error. Initialized
// empty for each new group.
type symbolState struct {
	// inFlight: count of currently-in-flight opens per symbol. Should be
	// 0 or 1 (engine rejects a second open while one is in flight). >1
	// is an invariant violation worth flagging.
	inFlight map[string]int

	// seenOpens: dedup key for "duplicate open" check, scoped per file
	// (cleared at file boundary — same (symbol, ts) appearing in adjacent
	// files isn't a duplicate, just a continuation marker).
	seenOpens map[openKey]int
}

type openKey struct{ Sym, TS string }

func newSymbolState() *symbolState {
	return &symbolState{
		inFlight:  make(map[string]int),
		seenOpens: make(map[openKey]int),
	}
}

// groupBySymbolCohort groups files by (parent_dir, symbol-prefix). Files
// in the same group are guaranteed to share a journal cohort (live or a
// specific shadow label) and the same symbol; they only differ by month.
// Sort within each group is lexical, which yields chronological order
// because the filename schema is `<SYMBOL>-YYYY-MM.jsonl`.
func groupBySymbolCohort(files []string) [][]string {
	type key struct{ Dir, Sym string }
	g := map[key][]string{}
	var order []key
	for _, p := range files {
		dir := filepath.Dir(p)
		base := filepath.Base(p)
		// Filename schema: <SYMBOL>-YYYY-MM.jsonl  (symbol may contain digits).
		// Suffix "-YYYY-MM.jsonl" is exactly 14 chars (dash + 4y + dash + 2m + .jsonl).
		// Counting positions from end: "-YYYY-MM.jsonl"
		//                              -14 -13     -9   -6
		// So the suffix matches when: base[-14]='-', base[-9]='-', base[-13]='2'.
		// Defensive: only strip when schema matches; otherwise treat the whole
		// basename as the symbol (so off-schema files fall into their own
		// group rather than being clumped wrongly).
		sym := base
		if len(base) > 14 && base[len(base)-14] == '-' && base[len(base)-9] == '-' &&
			base[len(base)-13] == '2' /* year prefix; will need updating in 1000y */ {
			sym = base[:len(base)-14]
		}
		k := key{Dir: dir, Sym: sym}
		if _, ok := g[k]; !ok {
			order = append(order, k)
		}
		g[k] = append(g[k], p)
	}
	out := make([][]string, 0, len(order))
	for _, k := range order {
		grp := g[k]
		sort.Strings(grp)
		out = append(out, grp)
	}
	return out
}

// validateFile reads one journal file and returns issues. A genuine I/O
// error (file unreadable) returns err; per-line content issues are returned
// as []issue without an outer error. The state pointer carries
// in-flight-position state across files in the same (cohort, symbol)
// group so cross-month closes don't false-positive.
func validateFile(path string, state *symbolState) ([]issue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var issues []issue
	// Reset per-file dedup state (same (sym, ts) across adjacent files is
	// a continuation, not a duplicate).
	state.seenOpens = make(map[openKey]int)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	var prevTS string
	lineno := 0
	totalLines := 0

	for sc.Scan() {
		lineno++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		totalLines++

		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// Defer the malformed-line decision until we know if it's the
			// trailing line — engine recovery tolerates ONE corrupt trailing
			// line (crash mid-flush). Mid-file malformed JSON is a real bug.
			// We can't tell trailing vs mid-file until we've read everything,
			// so save with placeholder and resolve at end.
			issues = append(issues, issue{
				Severity: "PENDING_TRAIL",
				File:     path,
				Line:     lineno,
				Msg:      fmt.Sprintf("malformed JSON: %v", err),
			})
			continue
		}

		// Invariant 1: ts monotonic
		if prevTS != "" && e.TS != "" && e.TS < prevTS {
			issues = append(issues, issue{
				Severity: "ERROR",
				File:     path,
				Line:     lineno,
				Msg:      fmt.Sprintf("timestamp goes backwards: %s < previous %s", e.TS, prevTS),
			})
		}
		if e.TS != "" {
			prevTS = e.TS
		}

		// Field-presence guard for open/close events. Missing symbol or ts
		// silently corrupts the in-flight tracker (state.inFlight[""] is
		// shared across all empty-symbol entries; openKey{"", ""} collapses
		// across events). Without this guard, a malformed engine emit could
		// hide invariant violations (e.g., a real "open while in-flight"
		// gets miscounted because both opens land in inFlight[""]).
		if e.Event == "open" || e.Event == "close" {
			if e.Symbol == "" {
				issues = append(issues, issue{
					Severity: "ERROR",
					File:     path,
					Line:     lineno,
					Msg:      fmt.Sprintf("%s event missing required field: symbol", e.Event),
				})
				continue
			}
			if e.TS == "" {
				issues = append(issues, issue{
					Severity: "ERROR",
					File:     path,
					Line:     lineno,
					Msg:      fmt.Sprintf("%s event for %s missing required field: ts", e.Event, e.Symbol),
				})
				continue
			}
		}

		switch e.Event {
		case "open":
			k := openKey{e.Symbol, e.TS}
			if firstLine, dup := state.seenOpens[k]; dup {
				issues = append(issues, issue{
					Severity: "WARN",
					File:     path,
					Line:     lineno,
					Msg: fmt.Sprintf("duplicate open for %s @ %s (first seen at line %d) — recovery firing twice?",
						e.Symbol, e.TS, firstLine),
				})
			} else {
				state.seenOpens[k] = lineno
			}
			// Invariant: in-flight count should never exceed 1 (engine
			// rejects a second open while one is in flight).
			if state.inFlight[e.Symbol] >= 1 {
				issues = append(issues, issue{
					Severity: "ERROR",
					File:     path,
					Line:     lineno,
					Msg: fmt.Sprintf("open for %s while already in-flight (count=%d) — engine should have rejected this",
						e.Symbol, state.inFlight[e.Symbol]),
				})
			}
			state.inFlight[e.Symbol]++

		case "close":
			// Invariant 2: close must have a preceding open — either earlier
			// in this file or in a prior-month file in the same cohort
			// (state map carries across files in a group).
			if state.inFlight[e.Symbol] == 0 {
				issues = append(issues, issue{
					Severity: "ERROR",
					File:     path,
					Line:     lineno,
					Msg: fmt.Sprintf("close for %s with no in-flight open (cohort+symbol stream)",
						e.Symbol),
				})
			} else if e.Outcome != "PARTIAL" {
				// Terminal close (TARGET/STOP/TIME) decrements in-flight.
				// PARTIAL closes leave the position in flight (B2 mid-R
				// partial-take followed by a later terminal close).
				state.inFlight[e.Symbol]--
			}

			// Cost-decomposition invariant (gated on Notional > 0 to skip
			// pre-decomp closes whose cost fields are all zero by virtue
			// of being absent from the JSON):
			//   pnl_usd ≈ gross_usd - fee_usd - slip_usd - funding_usd
			// Tolerance is 5 cents — each engine-side field is rounded to
			// cents (math.Round(*100)/100), so worst-case round-off across
			// 4 fields is ~2 cents; 5 cents gives margin for edge cases.
			if e.Notional > 0 {
				expected := e.GrossUSD - e.FeeUSD - e.SlipUSD - e.FundingUSD
				if math.Abs(e.PnlUSD-expected) > 0.05 {
					issues = append(issues, issue{
						Severity: "ERROR",
						File:     path,
						Line:     lineno,
						Msg: fmt.Sprintf("cost-decomp invariant violated: pnl_usd=%.2f vs gross-fee-slip-funding=%.2f (diff=%.4f) [g=%.2f f=%.2f s=%.2f fund=%.2f]",
							e.PnlUSD, expected, e.PnlUSD-expected, e.GrossUSD, e.FeeUSD, e.SlipUSD, e.FundingUSD),
					})
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	// Resolve PENDING_TRAIL: only the LAST line in the file gets the
	// trailing-malformed tolerance. Earlier malformed lines are real errors.
	out := make([]issue, 0, len(issues))
	pending := -1
	for i, iss := range issues {
		if iss.Severity == "PENDING_TRAIL" {
			if pending >= 0 {
				// Earlier pending is now confirmed not-trailing → ERROR.
				e := issues[pending]
				e.Severity = "ERROR"
				e.Msg = "mid-file " + e.Msg
				out = append(out, e)
			}
			pending = i
			continue
		}
		out = append(out, iss)
	}
	if pending >= 0 {
		e := issues[pending]
		// Was the malformed line literally the last non-empty line?
		if e.Line == lineno {
			e.Severity = "WARN"
			e.Msg = "trailing " + e.Msg + " (engine recovery tolerates this)"
		} else {
			e.Severity = "ERROR"
			e.Msg = "mid-file " + e.Msg
		}
		out = append(out, e)
	}

	_ = totalLines // available for richer output if needed later
	return out, nil
}
