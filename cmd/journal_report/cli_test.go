package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end CLI tests for journal_report.
//
// Exit-code contract:
//   1  cannot read --journal-dir (missing or unreadable)
//   3  --journal-dir exists but contains no usable journal data
//   0  reconciliation completed (text report on stdout)
//
// Sibling CLIs (journal_validate, journal_diff) use the same exit-3
// semantics for "input was readable but empty" — keeping the contract
// consistent across operator tools.

var jrBinary string

func TestMain(m *testing.M) {
	tmp, err := os.CreateTemp("", "journal_report_cli_test_*")
	if err != nil {
		panic(fmt.Sprintf("create tmp binary: %v", err))
	}
	tmp.Close()
	jrBinary = tmp.Name()

	build := exec.Command("go", "build", "-o", jrBinary, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		os.Remove(jrBinary)
		fmt.Printf("build failed: %v\n%s\n", err, out)
		os.Exit(2)
	}

	code := m.Run()
	os.Remove(jrBinary)
	os.Exit(code)
}

func runReport(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(jrBinary, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), buf.String()
		}
		t.Fatalf("exec failed: %v\noutput:\n%s", err, buf.String())
	}
	return 0, buf.String()
}

// TestCLI_MissingJournalDir_Exits1 — os.ReadDir failure path. Distinct
// from the empty-dir case — this is the operator typo'd a path that
// doesn't exist.
func TestCLI_MissingJournalDir_Exits1(t *testing.T) {
	code, out := runReport(t, "--journal-dir", "/nonexistent/path/abc")
	if code != 1 {
		t.Errorf("expected exit 1 on missing journal dir, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "cannot read journal dir") {
		t.Errorf("expected error message about missing dir, got:\n%s", out)
	}
}

// TestCLI_EmptyJournalDir_Exits3 — pre-fix this exited 0 with a text
// message, so any cron/CI consumer treated empty data as a clean
// reconciliation. Now mirrors journal_validate's exit-3 contract.
func TestCLI_EmptyJournalDir_Exits3(t *testing.T) {
	dir := t.TempDir()
	code, out := runReport(t, "--journal-dir", dir)
	if code != 3 {
		t.Errorf("expected exit 3 on empty journal dir, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "No journal data found") {
		t.Errorf("expected 'No journal data found' message, got:\n%s", out)
	}
}

// TestCLI_DirWithNonJsonlFiles_Exits3 — a dir that exists and has files
// but none match *.jsonl is functionally empty. Cron-callable case
// where the operator pointed at /var/log/paper-live (which has *.log
// files) instead of /var/log/paper-live/journal.
func TestCLI_DirWithNonJsonlFiles_Exits3(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "engine.log"),
		[]byte("not a journal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("symbol: BTCUSDT"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runReport(t, "--journal-dir", dir)
	if code != 3 {
		t.Errorf("expected exit 3 when no *.jsonl files match, got %d\nout:\n%s",
			code, out)
	}
}

// TestCLI_ValidJournalProducesReport — happy path. A minimal journal
// with one closed trade should produce a non-empty report at exit 0.
// Backtest binary is intentionally absent so the BT_T/BT% columns
// show 0/0% — that's fine, we're testing the live-side aggregation.
func TestCLI_ValidJournalProducesReport(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "BTCUSDT-2026-05.jsonl")
	content := strings.Join([]string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"test"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T10:00:00Z","side":"LONG","entry":50000,"exit":53000,"stop":49500,"target":53000,"pnl_usd":6000,"outcome":"TARGET","reason":"test"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(jsonl, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point at a guaranteed-missing backtest binary so runBacktest no-ops
	// cleanly without invoking external tooling. The presence of any path
	// that doesn't os.Stat triggers the early return inside runBacktest.
	code, out := runReport(t,
		"--journal-dir", dir,
		"--backtest-bin", "/nonexistent/backtest",
		"--config-dir", "/nonexistent/configs")
	if code != 0 {
		t.Errorf("expected exit 0 on valid journal, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "PAPER-LIVE RECONCILIATION REPORT") {
		t.Errorf("expected report header, got:\n%s", out)
	}
	if !strings.Contains(out, "BTCUSDT") {
		t.Errorf("expected symbol in report, got:\n%s", out)
	}
}
