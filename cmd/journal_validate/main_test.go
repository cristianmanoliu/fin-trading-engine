package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJournal writes the given JSONL lines to a tempfile and returns the
// path. Each line is taken verbatim — caller controls formatting.
func writeJournal(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func openLine(symbol, ts string) string {
	return `{"event":"open","symbol":"` + symbol + `","ts":"` + ts + `","side":"LONG","entry":100}`
}

func closeLine(symbol, ts, outcome string) string {
	return `{"event":"close","symbol":"` + symbol + `","ts":"` + ts + `","side":"LONG","entry":100,"exit":110,"outcome":"` + outcome + `"}`
}

func TestValidateFile_CleanJournal(t *testing.T) {
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
		openLine("BTCUSDT", "2026-05-08T12:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T14:00:00Z", "STOP"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("clean journal produced issues: %+v", issues)
	}
}

func TestValidateFile_StillOpenAtEndOfFile(t *testing.T) {
	// Open without close at end-of-file = position still in flight, OK.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
		openLine("BTCUSDT", "2026-05-08T12:00:00Z"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("still-open position should not produce issues: %+v", issues)
	}
}

func TestValidateFile_TimestampGoesBackwards(t *testing.T) {
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T07:00:00Z", "STOP"), // earlier than open!
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 || issues[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR for backward ts, got %+v", issues)
	}
	if !strings.Contains(issues[0].Msg, "backwards") {
		t.Errorf("error msg should mention backwards: %q", issues[0].Msg)
	}
}

func TestValidateFile_DuplicateOpen(t *testing.T) {
	// Same (symbol, ts) appearing twice = recovery firing twice.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Severity != "WARN" {
		t.Errorf("duplicate open should be WARN, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Msg, "duplicate") {
		t.Errorf("msg should mention duplicate: %q", issues[0].Msg)
	}
}

func TestValidateFile_CloseWithoutOpen(t *testing.T) {
	// Close for a symbol with no preceding open in the file = impossible state.
	p := writeJournal(t,
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 || issues[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR for close-without-open, got %+v", issues)
	}
	if !strings.Contains(issues[0].Msg, "without preceding open") {
		t.Errorf("error msg unclear: %q", issues[0].Msg)
	}
}

func TestValidateFile_PartialThenFullClose_OK(t *testing.T) {
	// PARTIAL closes don't terminate the position; a subsequent terminal close
	// for the same open is valid (B2 mid-R partial-take).
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T09:00:00Z", "PARTIAL"),
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("PARTIAL+final closes should not flag: %+v", issues)
	}
}

func TestValidateFile_TrailingMalformed_TolerantWarn(t *testing.T) {
	// Engine recovery explicitly tolerates one corrupt trailing line
	// (crash mid-flush). Validator should emit a WARN, not an ERROR.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
		`{"event":"open","symbol":"BTCUSDT"`, // truncated mid-write
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 trailing-malformed issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Severity != "WARN" {
		t.Errorf("trailing malformed should be WARN, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Msg, "trailing") {
		t.Errorf("msg should mention trailing: %q", issues[0].Msg)
	}
}

func TestValidateFile_MidFileMalformed_Error(t *testing.T) {
	// Malformed JSON NOT at end-of-file is a real error (engine doesn't
	// produce these).
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		`garbage line in the middle`,
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	hasMidFileError := false
	for _, iss := range issues {
		if iss.Severity == "ERROR" && strings.Contains(iss.Msg, "mid-file") {
			hasMidFileError = true
		}
	}
	if !hasMidFileError {
		t.Errorf("expected mid-file ERROR, got %+v", issues)
	}
}

func TestDiscoverJournals_RecursesShadowSubdirs(t *testing.T) {
	dir := t.TempDir()
	// Create a structure mirroring the production layout.
	for _, sub := range []string{"", "shadow/alt5-15-336", "shadow/bb20"} {
		full := filepath.Join(dir, sub)
		if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(full, "BTCUSDT-2026-05.jsonl"),
			[]byte(openLine("BTCUSDT", "2026-05-08T08:00:00Z")+"\n"),
			0644); err != nil {
			t.Fatal(err)
		}
	}
	// Also a non-jsonl file that must NOT be picked up.
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverJournals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 jsonl files, got %d: %v", len(files), files)
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".jsonl") {
			t.Errorf("non-jsonl file picked up: %s", f)
		}
	}
}

func TestValidateFile_MultipleSymbolsIndependent(t *testing.T) {
	// open ETH then close BTC (without ETH being closed first) is OK only if
	// BTC was previously opened. Cross-symbol pairing is per-symbol.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		openLine("ETHUSDT", "2026-05-08T08:00:00Z"),
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
		closeLine("ETHUSDT", "2026-05-08T11:00:00Z", "TARGET"),
	)
	issues, err := validateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("two-symbol interleaved should be clean: %+v", issues)
	}
}
