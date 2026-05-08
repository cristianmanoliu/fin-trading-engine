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
	"os"
	"path/filepath"
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
}

// issue is a single validation finding.
type issue struct {
	Severity string // "ERROR" | "WARN"
	File     string
	Line     int
	Msg      string
}

func main() {
	dir := flag.String("dir", "", "journal directory (recursively walked for *.jsonl files)")
	exclude := flag.String("exclude", "", "comma-separated path substrings to skip (e.g., 'archive,old_runs'); useful for frozen historical journals whose pre-fix issues are known and immutable")
	strict := flag.Bool("strict", false, "treat warnings as errors (exit 2 instead of 1)")
	verbose := flag.Bool("verbose", false, "print files with no issues too (default: only print files with issues)")
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

	totalErr, totalWarn := 0, 0
	for _, fp := range files {
		issues, err := validateFile(fp)
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

// validateFile reads one journal file and returns issues. A genuine I/O
// error (file unreadable) returns err; per-line content issues are returned
// as []issue without an outer error.
func validateFile(path string) ([]issue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var issues []issue
	type openKey struct{ Sym, TS string }
	seenOpens := make(map[openKey]int) // (symbol, ts) → first-seen line number
	openSyms := make(map[string]bool)  // symbols with at least one un-fully-closed open

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

		switch e.Event {
		case "open":
			k := openKey{e.Symbol, e.TS}
			if firstLine, dup := seenOpens[k]; dup {
				issues = append(issues, issue{
					Severity: "WARN",
					File:     path,
					Line:     lineno,
					Msg: fmt.Sprintf("duplicate open for %s @ %s (first seen at line %d) — recovery firing twice?",
						e.Symbol, e.TS, firstLine),
				})
			} else {
				seenOpens[k] = lineno
			}
			openSyms[e.Symbol] = true

		case "close":
			// Invariant 2: close must have a preceding open in same file for
			// same symbol. We don't pair specific opens with specific closes
			// (PARTIAL semantics complicate that); just verify ANY open
			// existed for this symbol earlier in the file.
			if !openSyms[e.Symbol] {
				issues = append(issues, issue{
					Severity: "ERROR",
					File:     path,
					Line:     lineno,
					Msg: fmt.Sprintf("close for %s without preceding open in same file — impossible state",
						e.Symbol),
				})
			}
			// A non-PARTIAL (TARGET/STOP/TIME) close terminates the position
			// for that symbol, so the next open for the same symbol is fresh.
			// We don't track this strictly because multiple closes for the
			// same open (one PARTIAL, one final) are valid by design.
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
