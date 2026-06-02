package main

import (
	"fmt"
	"time"
)

// healthReport evaluates the dashboard's monitoring health from loaded state +
// drift status. Returns (ok, status, lines):
//   - ok=true / status="OK"        → cache fresh, has data, drift clean
//   - ok=false / status="DEGRADED" → one or more monitoring problems; `lines`
//     carries machine-readable reason tokens (STALE / NO_DATA / DRIFT_KILL /
//     DRIFT_MISSING / DRIFT_DEAD) plus human context.
//
// This closes a fail-open in the old /healthz, which returned the literal "OK"
// whenever LoadState succeeded — i.e. even on a stale/empty cache or while the
// drift detector's exit-4 AUTO-KILL was live. A health endpoint that reads green
// when reality is "no monitoring / kill fired" is the canonical audit-lens bug
// (see docs/AUDIT_LENS.md). Mirrors the kill_switch exit-code-per-class fix
// (6dc3836): distinct, greppable status so an automated poller can discriminate.
//
// staleAfter is the cache-age threshold (production: 1h, matching the index
// stale banner). A zero/absent newest-mtime is treated as stale (no data read).
func healthReport(st *State, drift DriftStatus, staleAfter time.Duration) (bool, string, []string) {
	var reasons []string

	// Cache freshness. Zero mtime = nothing was read at all → stale.
	mtime := st.NewestCacheMtime()
	switch {
	case mtime.IsZero():
		reasons = append(reasons, "STALE: no journal files read (cache empty or path wrong)")
	case time.Since(mtime) > staleAfter:
		reasons = append(reasons, fmt.Sprintf("STALE: newest journal %s old (> %s) — run scripts/journal_fetch.sh",
			time.Since(mtime).Round(time.Minute), staleAfter))
	}

	// Data presence. Zero terminal trades across every cohort = no monitoring
	// signal, distinct from a fresh cache that simply has trades.
	total := 0
	for _, c := range st.AllCohorts() {
		total += c.TotalTrades
	}
	if total == 0 {
		reasons = append(reasons, "NO_DATA: zero terminal trades across all cohorts")
	}

	// Drift detector — the decision-grade kill mechanism. Surface its failure
	// shapes distinctly; exit-4 is an active AUTO-KILL and must never read green.
	switch {
	case drift.Missing:
		reasons = append(reasons, "DRIFT_MISSING: drift_check_history.jsonl absent/empty/corrupt")
	case drift.ExitCode == 4:
		reasons = append(reasons, fmt.Sprintf("DRIFT_KILL: drift detector last fired exit 4 (AUTO-KILL) on %s",
			drift.LastRunTS.Format("2006-01-02")))
	case drift.AgeDays > 10:
		reasons = append(reasons, fmt.Sprintf("DRIFT_DEAD: last drift run %dd ago (> 10d) — cron may be dead",
			drift.AgeDays))
	}

	if len(reasons) == 0 {
		return true, "OK", nil
	}
	return false, "DEGRADED", reasons
}
