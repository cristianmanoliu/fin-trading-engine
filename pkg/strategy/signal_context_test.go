package strategy

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSignalContextWriter_AppendsAndRotates verifies the writer:
//   - creates the dir on first write,
//   - writes JSONL records (one per Write call, newline-terminated),
//   - reuses the same file handle across writes within the same month,
//   - is a no-op when Dir is empty (matches the journal-disabled-when-path-empty
//     pattern used by Stub.appendJournal).
func TestSignalContextWriter_AppendsJSONLLines(t *testing.T) {
	dir := t.TempDir()
	w := &SignalContextWriter{Dir: filepath.Join(dir, "live"), Symbol: "BTCUSDT"}
	defer w.Close()

	w.Write(SignalContext{
		Event:  "signal_context",
		Symbol: "BTCUSDT",
		TS:     "2026-05-07T08:00:00Z",
		Label:  "live",
		Side:   "SHORT",
		Entry:  100000,
		Stop:   101000,
		Target: 94000,
		Bias:   -1,
	})
	w.Write(SignalContext{
		Event:  "signal_context",
		Symbol: "BTCUSDT",
		TS:     "2026-05-07T12:00:00Z",
		Label:  "live",
		Side:   "SHORT",
		Entry:  99000,
		Stop:   99900,
		Target: 93600,
		Bias:   -1,
	})

	// Find the produced file (month is wall-clock-derived, so we glob).
	matches, err := filepath.Glob(filepath.Join(w.Dir, "BTCUSDT-*.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one journal file under %s, got %v (err=%v)", w.Dir, matches, err)
	}

	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Copy: scanner.Bytes is reused.
		b := append([]byte(nil), scanner.Bytes()...)
		lines = append(lines, b)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var c SignalContext
		if err := json.Unmarshal(line, &c); err != nil {
			t.Fatalf("line %d: failed to parse JSON: %v\n  line: %s", i, err, string(line))
		}
		if c.Event != "signal_context" {
			t.Errorf("line %d: event=%s, want signal_context", i, c.Event)
		}
		if c.Symbol != "BTCUSDT" {
			t.Errorf("line %d: symbol=%s, want BTCUSDT", i, c.Symbol)
		}
	}
}

// TestSignalContextWriter_WriteFailureClearsHandleForRetry verifies the
// 2026-05-10 audit-pattern fix on the THIRD paired-implementation of the
// silent-write-failure shape (after Stub `4154374` and BinanceLive `028e6a2`).
// When Write fails on the underlying file (disk full, fd revoked, etc.),
// the file handle MUST be cleared so the next call goes through open-or-
// create and gets a fresh fd. Pre-fix the writer left w.file non-nil after
// failure → every subsequent Write hit the same dead handle and failed
// identically while the engine kept emitting signals into a void.
func TestSignalContextWriter_WriteFailureClearsHandleForRetry(t *testing.T) {
	dir := t.TempDir()
	w := &SignalContextWriter{Dir: dir, Symbol: "BTCUSDT"}
	defer w.Close()

	// Plant a closed-file handle as w.file. Write on a closed *os.File
	// returns os.ErrClosed — exactly the kind of mid-runtime failure
	// the audit pattern targets (disk full, fd leak, permissions revoked).
	closedFile, err := os.CreateTemp(dir, "broken-*.jsonl")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if cerr := closedFile.Close(); cerr != nil {
		t.Fatalf("setup close: %v", cerr)
	}
	w.file = closedFile
	// MUST equal the month Write() derives from time.Now() so the
	// month-rollover branch does NOT fire — otherwise Write reopens a fresh
	// handle, the planted dead fd is never used, and the write-failure path
	// under test never runs. Hardcoding a literal month (e.g. "2026-05") made
	// this a time-bomb: it passed only during that calendar month and reddened
	// CI on the next month's UTC rollover (broke 2026-06-01). Derive it live.
	w.month = time.Now().UTC().Format("2006-01")

	// First Write — internal Write call on the closed handle fails.
	w.Write(SignalContext{
		Event: "signal_context", Symbol: "BTCUSDT",
		TS: "2026-05-10T20:00:00Z", Label: "live", Side: "LONG",
		Entry: 100000, Stop: 99000, Target: 106000,
	})

	// Critical assertion: handle MUST be cleared so next call reopens.
	if w.file != nil {
		t.Fatal("w.file not cleared after write failure — next call would " +
			"write to the same dead handle and lose the record identically")
	}
	if w.month != "" {
		t.Errorf("w.month not cleared: got %q", w.month)
	}

	// Second Write MUST succeed via the open-or-create path. Dir is a
	// writable temp dir so the recovery path runs cleanly.
	w.Write(SignalContext{
		Event: "signal_context", Symbol: "BTCUSDT",
		TS: "2026-05-10T20:01:00Z", Label: "live", Side: "LONG",
		Entry: 100100, Stop: 99100, Target: 106100,
	})

	if w.file == nil {
		t.Error("w.file not re-opened on subsequent Write after failure clear")
	}
	// And the on-disk file must contain the recovered event (proves a real,
	// writeable handle, not just a non-nil sentinel).
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("no signal-context file on disk after recovery")
	}
	body, _ := os.ReadFile(matches[0])
	if len(body) == 0 {
		t.Errorf("recovered file is empty — Write recovery did not actually write")
	}
}

// TestSignalContextWriter_NilAndEmptyAreNoOps protects against panics when
// the writer is unconfigured (cmd/backtest path, or when the operator hasn't
// set --signal-context-dir).
func TestSignalContextWriter_NilAndEmptyAreNoOps(t *testing.T) {
	// Nil receiver — must not panic.
	var w *SignalContextWriter
	w.Write(SignalContext{Symbol: "X"})
	w.Close()

	// Empty Dir — must not create files.
	dir := t.TempDir()
	w2 := &SignalContextWriter{Dir: "", Symbol: "X"}
	w2.Write(SignalContext{Symbol: "X"})
	w2.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty Dir to produce no files, got %d", len(entries))
	}
}
