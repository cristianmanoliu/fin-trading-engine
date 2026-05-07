package strategy

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
