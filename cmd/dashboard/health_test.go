package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// freshCohort builds a cohort with n terminal trades and a freshness mtime.
func freshCohort(label string, n int, mtime time.Time) *Cohort {
	return &Cohort{
		Label:            label,
		TotalTrades:      n,
		SymbolPnL:        map[string]float64{},
		SymbolTrades:     map[string]int{},
		CacheFreshnessTS: mtime,
	}
}

// healthyState is a State that should report OK: one live cohort with trades and
// a fresh cache mtime.
func healthyState(mtime time.Time) *State {
	return &State{
		Live:     freshCohort("live", 40, mtime),
		LoadedAt: time.Now(),
	}
}

func cleanDrift() DriftStatus {
	return DriftStatus{LastRunTS: time.Now().Add(-24 * time.Hour), ExitCode: 0, AgeDays: 1}
}

func TestHealthReport_AllGood_OK(t *testing.T) {
	st := healthyState(time.Now())
	ok, status, lines := healthReport(st, cleanDrift(), time.Hour)
	if !ok {
		t.Errorf("expected ok=true, got false; lines=%v", lines)
	}
	if status != "OK" {
		t.Errorf("status = %q, want OK", status)
	}
}

func TestHealthReport_StaleCache_NotOK(t *testing.T) {
	// Newest journal mtime is 3h old → past the 1h staleness threshold. The old
	// /healthz returned OK regardless; the fix must report DEGRADED with a STALE
	// reason so an automated poller (or operator) sees "no fresh monitoring."
	st := healthyState(time.Now().Add(-3 * time.Hour))
	ok, status, lines := healthReport(st, cleanDrift(), time.Hour)
	if ok {
		t.Error("expected ok=false on stale cache (3h > 1h threshold)")
	}
	if status == "OK" {
		t.Errorf("status = %q, want non-OK", status)
	}
	if !containsReason(lines, "stale") {
		t.Errorf("expected a STALE reason in lines, got %v", lines)
	}
}

func TestHealthReport_NoData_NotOK(t *testing.T) {
	// Cache is fresh but holds zero terminal trades across all cohorts. The old
	// /healthz returned OK (empty cache loads cleanly) — the canonical fail-open:
	// "OK" when reality is "no monitoring data at all."
	st := &State{Live: freshCohort("live", 0, time.Now()), LoadedAt: time.Now()}
	ok, status, lines := healthReport(st, cleanDrift(), time.Hour)
	if ok {
		t.Error("expected ok=false when zero terminal trades across all cohorts")
	}
	if status == "OK" {
		t.Errorf("status = %q, want non-OK", status)
	}
	if !containsReason(lines, "no_data") {
		t.Errorf("expected a NO_DATA reason, got %v", lines)
	}
}

func TestHealthReport_DriftExit4_KillSurfaced(t *testing.T) {
	// Drift detector fired exit 4 (AUTO-KILL). A health endpoint that returns OK
	// while the decision-grade kill signal is live is the worst fail-open in this
	// file — the operator's automated check would show green during a kill.
	st := healthyState(time.Now())
	drift := DriftStatus{LastRunTS: time.Now().Add(-12 * time.Hour), ExitCode: 4, AgeDays: 0}
	ok, _, lines := healthReport(st, drift, time.Hour)
	if ok {
		t.Error("expected ok=false on drift exit-4 (AUTO-KILL)")
	}
	if !containsReason(lines, "drift_kill") {
		t.Errorf("expected a DRIFT_KILL reason, got %v", lines)
	}
}

func TestHealthReport_DriftMissing_NotOK(t *testing.T) {
	st := healthyState(time.Now())
	ok, _, lines := healthReport(st, DriftStatus{Missing: true}, time.Hour)
	if ok {
		t.Error("expected ok=false when drift history missing (monitoring not seeded)")
	}
	if !containsReason(lines, "drift_missing") {
		t.Errorf("expected a DRIFT_MISSING reason, got %v", lines)
	}
}

func TestHealthReport_DriftDead_NotOK(t *testing.T) {
	// Weekly cadence; >10d since last run = dead cron. Must not report OK.
	st := healthyState(time.Now())
	drift := DriftStatus{LastRunTS: time.Now().Add(-14 * 24 * time.Hour), ExitCode: 0, AgeDays: 14}
	ok, _, lines := healthReport(st, drift, time.Hour)
	if ok {
		t.Error("expected ok=false when drift cron dead (>10d)")
	}
	if !containsReason(lines, "drift_dead") {
		t.Errorf("expected a DRIFT_DEAD reason, got %v", lines)
	}
}

func TestHealthReport_MultipleReasons_AllListed(t *testing.T) {
	// Stale AND drift-kill at once — both reasons must surface, not just the first.
	st := healthyState(time.Now().Add(-5 * time.Hour))
	drift := DriftStatus{LastRunTS: time.Now().Add(-12 * time.Hour), ExitCode: 4, AgeDays: 0}
	ok, _, lines := healthReport(st, drift, time.Hour)
	if ok {
		t.Error("expected ok=false")
	}
	if !containsReason(lines, "stale") || !containsReason(lines, "drift_kill") {
		t.Errorf("expected BOTH stale and drift_kill reasons, got %v", lines)
	}
}

func containsReason(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), substr) {
			return true
		}
	}
	return false
}

// TestHandleHealthz_EmptyCache_Returns503 is the end-to-end guard: the OLD
// /healthz returned 200 "OK" on an empty journal dir (the canonical fail-open).
// The fixed handler must return 503 + DEGRADED + reason tokens. Drives the real
// HTTP handler through the global flag vars (restored after the test).
func TestHandleHealthz_EmptyCache_Returns503(t *testing.T) {
	dir := t.TempDir() // empty journal cache → no files, zero trades, stale
	resultsDir := t.TempDir()

	defer withFlags(*flagJournalDir, *flagResultsDir)()
	*flagJournalDir = dir
	*flagResultsDir = resultsDir

	rr := httptest.NewRecorder()
	handleHealthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want 503 (empty cache must not read green)", rr.Code)
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, "DEGRADED") {
		t.Errorf("body must start with DEGRADED, got:\n%s", body)
	}
	if !strings.Contains(body, "reason=") {
		t.Errorf("body must carry reason tokens, got:\n%s", body)
	}
}

// TestHandleHealthz_FreshDataCleanDrift_Returns200OK pins the happy path: a
// fresh journal with a terminal trade + a clean drift history → 200 "OK".
func TestHandleHealthz_FreshDataCleanDrift_Returns200OK(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "BTCUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-10T12:00:00Z","side":"SHORT","entry":60000,"stop":60300,"target":58200,"reason":"t"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-11T08:00:00Z","side":"SHORT","entry":60000,"exit":58200,"stop":60300,"target":58200,"pnl_usd":500,"outcome":"TARGET","reason":"t","notional_usd":200000,"fee_usd":20}`,
	})
	resultsDir := t.TempDir()
	driftLine := `{"ts":"` + time.Now().Add(-24*time.Hour).Format(time.RFC3339) + `","exit_code":0}`
	if err := os.WriteFile(filepath.Join(resultsDir, "drift_check_history.jsonl"), []byte(driftLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	defer withFlags(*flagJournalDir, *flagResultsDir)()
	*flagJournalDir = dir
	*flagResultsDir = resultsDir

	rr := httptest.NewRecorder()
	handleHealthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200; body:\n%s", rr.Code, rr.Body.String())
	}
	if !strings.HasPrefix(rr.Body.String(), "OK") {
		t.Errorf("body must start with OK, got:\n%s", rr.Body.String())
	}
}

// withFlags snapshots the two dir flags and returns a restore func for defer.
func withFlags(journalDir, resultsDir string) func() {
	return func() {
		*flagJournalDir = journalDir
		*flagResultsDir = resultsDir
	}
}

// TestRenderTemplates_AllPagesExecute guards the stale-banner wiring: every page
// now reads a .Stale field, and a template/handler field mismatch surfaces only
// at Execute time (template.Must parses but does not execute). Renders each page
// against a minimal cache and asserts a 200 with non-trivial body.
func TestRenderTemplates_AllPagesExecute(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "BTCUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-10T12:00:00Z","side":"SHORT","entry":60000,"stop":60300,"target":58200,"reason":"t"}`,
	})
	defer withFlags(*flagJournalDir, *flagResultsDir)()
	*flagJournalDir = dir
	*flagResultsDir = t.TempDir()

	pages := []struct {
		name string
		fn   http.HandlerFunc
		path string
	}{
		{"index", handleIndex, "/"},
		{"status", handleStatus, "/status"},
		{"cohorts", handleCohorts, "/cohorts"},
	}
	for _, p := range pages {
		rr := httptest.NewRecorder()
		p.fn(rr, httptest.NewRequest(http.MethodGet, p.path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s: code = %d, want 200; body:\n%s", p.name, rr.Code, rr.Body.String())
		}
		if len(rr.Body.String()) < 50 {
			t.Errorf("%s: body suspiciously short (%d bytes) — template likely errored mid-execute:\n%s",
				p.name, len(rr.Body.String()), rr.Body.String())
		}
	}
}
