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
	issues, err := validateFile(p, newSymbolState())
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
	issues, err := validateFile(p, newSymbolState())
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
	issues, err := validateFile(p, newSymbolState())
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
	// Same (symbol, ts) appearing twice produces two complementary issues:
	// a WARN ("recovery firing twice?") describing the LIKELY CAUSE, and
	// an ERROR ("open while already in-flight") describing the INVARIANT
	// VIOLATION. Operators benefit from seeing both — cause + state.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues (WARN duplicate + ERROR in-flight), got %d: %+v", len(issues), issues)
	}
	hasWarn, hasErr := false, false
	for _, iss := range issues {
		if iss.Severity == "WARN" && strings.Contains(iss.Msg, "duplicate") {
			hasWarn = true
		}
		if iss.Severity == "ERROR" && strings.Contains(iss.Msg, "in-flight") {
			hasErr = true
		}
	}
	if !hasWarn {
		t.Error("missing WARN duplicate-open issue")
	}
	if !hasErr {
		t.Error("missing ERROR in-flight invariant violation")
	}
}

func TestValidateFile_CloseWithoutOpen(t *testing.T) {
	// Close for a symbol with no preceding open in the file = impossible state.
	p := writeJournal(t,
		closeLine("BTCUSDT", "2026-05-08T10:00:00Z", "TARGET"),
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 || issues[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR for close-without-open, got %+v", issues)
	}
	if !strings.Contains(issues[0].Msg, "no in-flight open") {
		t.Errorf("error msg unclear: %q", issues[0].Msg)
	}
}

func TestValidateFile_CrossMonth_OpenInPriorFile_NoFalsePositive(t *testing.T) {
	// Cross-month case: position opened in May, closed in June. The May
	// file has an open without close (still in flight at end-of-file); the
	// June file has a close. Without state carry-over across files, June
	// would false-positive a "close without open" ERROR.
	dir := t.TempDir()
	mayFile := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	juneFile := filepath.Join(dir, "BTCUSDT-2026-06.jsonl")
	if err := os.WriteFile(mayFile,
		[]byte(openLine("BTCUSDT", "2026-05-31T22:00:00Z")+"\n"),
		0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(juneFile,
		[]byte(closeLine("BTCUSDT", "2026-06-01T08:00:00Z", "TARGET")+"\n"),
		0644); err != nil {
		t.Fatal(err)
	}

	state := newSymbolState()
	mayIssues, err := validateFile(mayFile, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(mayIssues) != 0 {
		t.Errorf("May file (open without close = still in flight) should be clean: %+v", mayIssues)
	}
	juneIssues, err := validateFile(juneFile, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(juneIssues) != 0 {
		t.Errorf("June file (close matching prior-month open) should be clean: %+v", juneIssues)
	}
	// And the carryover state should now be drained.
	if state.inFlight["BTCUSDT"] != 0 {
		t.Errorf("after terminal close in June, in-flight count should be 0, got %d", state.inFlight["BTCUSDT"])
	}
}

func TestGroupBySymbolCohort_SortsChronologically(t *testing.T) {
	// Files are grouped by parent_dir + symbol; within a group, sorted
	// lexically (= chronologically, since YYYY-MM filename suffix is
	// well-ordered).
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "BTCUSDT-2026-06.jsonl"),
		filepath.Join(dir, "BTCUSDT-2026-05.jsonl"),
		filepath.Join(dir, "ETHUSDT-2026-05.jsonl"),
		filepath.Join(dir, "shadow", "bb20", "BTCUSDT-2026-05.jsonl"),
	}
	groups := groupBySymbolCohort(files)
	if len(groups) != 3 {
		// BTCUSDT/live (2 files), ETHUSDT/live (1), BTCUSDT/shadow-bb20 (1) = 3 groups
		t.Fatalf("expected 3 groups, got %d: %v", len(groups), groups)
	}
	// Find the BTCUSDT/live group; it must be sorted with May before June.
	for _, g := range groups {
		if len(g) == 2 && strings.HasSuffix(filepath.Dir(g[0]), filepath.Base(dir)) {
			if !strings.Contains(g[0], "2026-05") || !strings.Contains(g[1], "2026-06") {
				t.Errorf("BTCUSDT/live group not chronological: %v", g)
			}
		}
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
	issues, err := validateFile(p, newSymbolState())
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
	issues, err := validateFile(p, newSymbolState())
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
	issues, err := validateFile(p, newSymbolState())
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

	files, err := discoverJournals(dir, nil)
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

	// With --exclude=shadow, only the root-level live journal should remain.
	filtered, err := discoverJournals(dir, []string{"shadow"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Errorf("--exclude=shadow expected 1 file, got %d: %v", len(filtered), filtered)
	}
	if len(filtered) > 0 && strings.Contains(filtered[0], "shadow") {
		t.Errorf("--exclude=shadow leaked a shadow file: %s", filtered[0])
	}
}

func TestValidateFile_CostDecomp_ValidInvariant_NoIssue(t *testing.T) {
	// pnl_usd = gross - fee - slip - funding. With round-cent precision,
	// e.g. gross=100, fee=10, slip=5, funding=2 → pnl=83. Within tolerance.
	closeWithCosts := `{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T10:00:00Z","side":"LONG","entry":100,"outcome":"TARGET","gross_usd":100.00,"fee_usd":10.00,"slip_usd":5.00,"funding_usd":2.00,"notional_usd":50000,"pnl_usd":83.00}`
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeWithCosts,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("valid cost decomp should not flag: %+v", issues)
	}
}

func TestValidateFile_CostDecomp_InvalidInvariant_Errors(t *testing.T) {
	// pnl_usd doesn't match gross - fee - slip - funding. gross=100, fee=10,
	// slip=5, funding=0 → expected pnl=85, but pnl_usd=99 (way off).
	closeWithBadCosts := `{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T10:00:00Z","side":"LONG","entry":100,"outcome":"TARGET","gross_usd":100.00,"fee_usd":10.00,"slip_usd":5.00,"funding_usd":0.00,"notional_usd":50000,"pnl_usd":99.00}`
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeWithBadCosts,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	hasErr := false
	for _, iss := range issues {
		if iss.Severity == "ERROR" && strings.Contains(iss.Msg, "cost-decomp invariant") {
			hasErr = true
		}
	}
	if !hasErr {
		t.Errorf("expected cost-decomp ERROR, got %+v", issues)
	}
}

func TestValidateFile_CostDecomp_PreDecompSchemaSkipped(t *testing.T) {
	// Old close event without the cost-decomp fields — Notional defaults
	// to 0 on JSON unmarshal (omitempty + zero value). Validator should
	// skip the invariant check rather than false-positive.
	oldClose := `{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T10:00:00Z","side":"LONG","entry":100,"exit":110,"outcome":"TARGET","pnl_usd":10.00}`
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		oldClose,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("pre-decomp close should not be flagged: %+v", issues)
	}
}

func TestValidateFile_CostDecomp_RoundOffWithinTolerance(t *testing.T) {
	// Each field is rounded to cents on engine side; sum can drift by a
	// few cents from the rounded sum. 5-cent tolerance covers it.
	// Construct: gross=100.005, fee=10.005, slip=5.005, funding=2.005 →
	// rounded to {100.01, 10.01, 5.01, 2.01}. Sum: 100.01-10.01-5.01-2.01=82.98.
	// With pnl_usd=83.00 (also rounded), diff=0.02 — within 0.05 tolerance.
	closeRounded := `{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T10:00:00Z","side":"LONG","entry":100,"outcome":"TARGET","gross_usd":100.01,"fee_usd":10.01,"slip_usd":5.01,"funding_usd":2.01,"notional_usd":50000,"pnl_usd":83.00}`
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		closeRounded,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	for _, iss := range issues {
		if strings.Contains(iss.Msg, "cost-decomp") {
			t.Errorf("rounding within tolerance should not flag: %+v", iss)
		}
	}
}

func TestValidateFile_OpenMissingSymbol_Errors(t *testing.T) {
	// open event with empty symbol must not silently increment inFlight[""]
	// — that would mask real invariant violations elsewhere.
	p := writeJournal(t,
		`{"event":"open","symbol":"","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":100}`,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 || issues[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR for open-missing-symbol, got %+v", issues)
	}
	if !strings.Contains(issues[0].Msg, "missing required field: symbol") {
		t.Errorf("error msg should name the missing field: %q", issues[0].Msg)
	}
}

func TestValidateFile_CloseMissingTS_Errors(t *testing.T) {
	// close event with empty ts must not silently decrement state — would
	// hide a real "close without open" if the symbol matches an in-flight.
	p := writeJournal(t,
		openLine("BTCUSDT", "2026-05-08T08:00:00Z"),
		`{"event":"close","symbol":"BTCUSDT","ts":"","side":"LONG","entry":100,"exit":110,"outcome":"TARGET"}`,
	)
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	hasFieldErr := false
	for _, iss := range issues {
		if iss.Severity == "ERROR" && strings.Contains(iss.Msg, "missing required field: ts") {
			hasFieldErr = true
		}
	}
	if !hasFieldErr {
		t.Errorf("expected ERROR for close-missing-ts, got %+v", issues)
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
	issues, err := validateFile(p, newSymbolState())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("two-symbol interleaved should be clean: %+v", issues)
	}
}
