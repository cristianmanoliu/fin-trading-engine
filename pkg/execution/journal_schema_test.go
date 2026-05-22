package execution

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestJournalEntry_FieldSetIsPinned guards the on-disk JSONL surface — the
// locked contract between the engine (writer) and the three readers:
//   - cmd/journal_diff       (Layer 3 stub-vs-testnet parity)
//   - cmd/journal_validate   (cross-month integrity audit)
//   - scripts/live_vs_backtest_drift.py  (decision-grade drift detector)
//
// Each reader maintains its OWN subset struct (Go) or dict parsing (Python),
// intentionally decoupled from this struct. When a writer-side field is
// added or renamed, the JSONL contract changes — any consumer that needs
// the new field has to be updated explicitly. This test forces that
// deliberation by failing if the field set drifts from the locked golden.
//
// To intentionally add a field:
//  1. Add the field + JSON tag to journalEntry in stub.go
//  2. Update the expected list below
//  3. Audit each reader (journal_diff/journal_validate/live_vs_backtest_drift.py)
//     and either add the field there too OR document why it's intentionally
//     ignored downstream
//  4. Commit all four files together
//
// The test compares the *JSON tag* set (the on-disk surface), not Go field
// names — renaming a Go field without changing its JSON tag is contract-safe.
func TestJournalEntry_FieldSetIsPinned(t *testing.T) {
	// Locked field set. Append-only growth is fine (omitempty fields stay
	// backwards-compatible for existing readers). Mutation/removal requires
	// a coordinated migration across all three readers.
	expected := []string{
		"entry",
		"event",
		"exit",
		"fee_usd",
		"funding_usd",
		"gross_usd",
		"mae_r",
		"mfe_r",
		"notional_usd",
		"outcome",
		"pnl_pts",
		"pnl_usd",
		"reason",
		"side",
		"slip_usd",
		"stop",
		"symbol",
		"target",
		"ts",
	}

	actual := jsonTagsOf(reflect.TypeOf(journalEntry{}))

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf(
			"journalEntry JSON field set drifted from locked golden.\n"+
				"  actual:   %v\n"+
				"  expected: %v\n"+
				"  added:    %v\n"+
				"  removed:  %v\n"+
				"\nIf this is an intentional schema change:\n"+
				"  1. Update the expected list in this test\n"+
				"  2. Audit cmd/journal_diff/main.go journalEntry struct\n"+
				"  3. Audit cmd/journal_validate/main.go entry struct\n"+
				"  4. Audit scripts/live_vs_backtest_drift.py load_local_trades\n"+
				"  5. Commit all four together",
			actual, expected,
			setDiff(actual, expected),
			setDiff(expected, actual),
		)
	}
}

// jsonTagsOf returns the sorted set of `json:"..."` tag names (sans options
// like ",omitempty") for all exported fields on t. Skipped: fields with
// json:"-" or no json tag at all.
func jsonTagsOf(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// setDiff returns elements of a not present in b.
func setDiff(a, b []string) []string {
	bset := make(map[string]bool, len(b))
	for _, s := range b {
		bset[s] = true
	}
	var out []string
	for _, s := range a {
		if !bset[s] {
			out = append(out, s)
		}
	}
	return out
}
