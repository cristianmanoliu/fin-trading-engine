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

// CLI startup-gate tests for cmd/backtest.
//
// cmd/backtest is the source-of-truth for every locked sweep verdict in
// results/. A regression in flag parsing or config loading would silently
// poison future re-runs (which is also why "locked = immutable" matters
// — past sweep outputs are frozen artifacts, not re-derivable).
//
// These tests exercise the fast-fail validation gates that fire BEFORE
// the actual replay loop spends CPU time:
//   - bad config path → exit 1
//   - invalid --signal-tf → exit 1
//   - invalid --side-filter → exit 1
//   - missing CSV path → exit 1 (CSVReplay.Subscribe errors)
//
// NOT covered: actual replay correctness — that's exercised exhaustively
// by pkg/strategy, pkg/execution, pkg/aggregator unit tests.

var btBinary string

func TestMain(m *testing.M) {
	tmp, err := os.CreateTemp("", "backtest_cli_test_*")
	if err != nil {
		panic(fmt.Sprintf("create tmp binary: %v", err))
	}
	tmp.Close()
	btBinary = tmp.Name()

	build := exec.Command("go", "build", "-o", btBinary, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		os.Remove(btBinary)
		fmt.Printf("build failed: %v\n%s\n", err, out)
		os.Exit(2)
	}

	code := m.Run()
	os.Remove(btBinary)
	os.Exit(code)
}

func minimalConfig(t *testing.T, csvPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	body := fmt.Sprintf("symbol: TESTUSDT\nbacktest:\n  csv_path: %s\n", csvPath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBT(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(btBinary, args...)
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

// TestCLI_BadConfigPath_Exits1 — config.Load failure path.
func TestCLI_BadConfigPath_Exits1(t *testing.T) {
	code, out := runBT(t, "--config", "/nonexistent/path/abc.yaml")
	if code != 1 {
		t.Errorf("expected exit 1, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "failed to load config") {
		t.Errorf("expected config-load error message, got:\n%s", out)
	}
}

// TestCLI_InvalidSignalTF_Exits1 — same allowlist as cmd/engine.
func TestCLI_InvalidSignalTF_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/nonexistent.csv")
	code, out := runBT(t, "--config", cfg, "--signal-tf", "10m")
	if code != 1 {
		t.Errorf("expected exit 1 on invalid --signal-tf, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid --signal-tf") {
		t.Errorf("expected signal-tf error, got:\n%s", out)
	}
}

// TestCLI_InvalidSideFilter_Exits1 — symmetric to cmd/engine's check.
func TestCLI_InvalidSideFilter_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/nonexistent.csv")
	code, out := runBT(t, "--config", cfg, "--side-filter", "diagonal")
	if code != 1 {
		t.Errorf("expected exit 1 on invalid --side-filter, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid --side-filter") {
		t.Errorf("expected side-filter error, got:\n%s", out)
	}
}

// TestCLI_MissingCSV_Exits1 — CSVReplay.Subscribe errors when the file
// doesn't exist; cmd/backtest catches that and exits 1.
func TestCLI_MissingCSV_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/nonexistent/data.csv")
	code, out := runBT(t, "--config", cfg)
	if code != 1 {
		t.Errorf("expected exit 1 on missing CSV, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "failed to subscribe to csv replay") {
		t.Errorf("expected CSV subscribe error, got:\n%s", out)
	}
}

// TestCLI_YearMonthOverride_BuildsCSVPath — --year+--month override the
// config's csv_path. With both set and bogus year, the constructed path
// should be missing → exit 1 with "failed to subscribe".
func TestCLI_YearMonthOverride_BuildsCSVPath(t *testing.T) {
	// Config CSV path doesn't matter — overridden by year/month.
	cfg := minimalConfig(t, "./data/TESTUSDT-1m-2024-01.csv")
	code, out := runBT(t, "--config", cfg, "--year", "1999", "--month", "01")
	if code != 1 {
		t.Errorf("expected exit 1 on missing override CSV, got %d\nout:\n%s", code, out)
	}
	// Path is built as ./data/<symbol>-1m-1999-01.csv. The error message
	// from CSVReplay should reference this path.
	if !strings.Contains(out, "1999-01") {
		t.Errorf("expected year/month-overridden path in error, got:\n%s", out)
	}
}

// TestCLI_BD1_PartialYearOnly_Exits1 — operator passes only --year
// without --month (or vice versa). Previously the override was silently
// skipped (`if *year != "" && *month != ""`) and the CSV path defaulted
// to whatever the YAML had — operator runs "Jan 2024" but gets a
// different month/year, then locks the wrong verdict into results/.
// Now: refuse asymmetric flags.
func TestCLI_BD1_PartialYearOnly_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/tmp/nonexistent.csv")
	code, out := runBT(t, "--config", cfg, "--year", "2024")
	if code != 1 {
		t.Errorf("expected exit 1 on partial year-only, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "--year and --month must be set together") {
		t.Errorf("expected partial-flag error, got:\n%s", out)
	}
}

func TestCLI_BD1_PartialMonthOnly_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/tmp/nonexistent.csv")
	code, out := runBT(t, "--config", cfg, "--month", "06")
	if code != 1 {
		t.Errorf("expected exit 1 on partial month-only, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "--year and --month must be set together") {
		t.Errorf("expected partial-flag error, got:\n%s", out)
	}
}

// TestCLI_BD3_FundingFilterWithoutCSVDir_Exits1 — operator enables the
// funding-cross filter flag expecting shorts to be gated; previously the
// type assertion to *funding.Historical failed (no --funding-csv-dir
// provided), slog.Warn fired, and the backtest ran WITHOUT the filter.
// Strategy results then didn't match operator intent. Same audit-pattern
// shape as the cmd/engine 2nd-pass fix at fe4bf21. Now: exit 1.
func TestCLI_BD3_FundingFilterWithoutCSVDir_Exits1(t *testing.T) {
	cfg := minimalConfig(t, "/tmp/nonexistent.csv")
	code, out := runBT(t, "--config", cfg,
		"--funding-filter-max-bps-per-day", "5")
	if code != 1 {
		t.Errorf("expected exit 1 on funding-filter without csv-dir, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "refusing silent disable") {
		t.Errorf("expected explicit refusal message, got:\n%s", out)
	}
}
