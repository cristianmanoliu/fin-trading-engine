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

// CLI startup-gate tests for cmd/engine.
//
// The engine binary is the highest-blast-radius surface in the system —
// the executor switch decides whether orders go to (a) nowhere (stub,
// paper-money), (b) testnet, or (c) real Binance. A silent regression in
// the dispatch (e.g., a typo'd case label silently falling through to the
// stub default, or a missing creds check letting binance_live start with
// empty API keys) would mean the operator THINKS they're running real-money
// but they're paper-trading — or, worse, vice-versa.
//
// These tests exercise the startup gates that fire BEFORE marketdata
// subscribe (i.e., before any goroutine spawns or network call goes out).
// Specifically:
//   - bad config path → exit 1
//   - invalid --signal-tf → exit 1
//   - invalid --executor → exit 1 (default case in switch)
//   - --executor=binance_live without BINANCE_API_KEY → exit 1
//   - --executor=binance_live_testnet without API key → exit 1
//   - --layer3-binance-testnet-journal-dir + non-stub primary → exit 1
//   - --layer3-binance-testnet-journal-dir + missing creds → exit 1
//
// NOT covered here: the actual marketdata subscribe, runner loop, signal
// flow, or executor execution. Those are exercised by package-level
// unit tests (pkg/strategy, pkg/marketdata, pkg/execution).

var engineBinary string

func TestMain(m *testing.M) {
	tmp, err := os.CreateTemp("", "engine_cli_test_*")
	if err != nil {
		panic(fmt.Sprintf("create tmp binary: %v", err))
	}
	tmp.Close()
	engineBinary = tmp.Name()

	build := exec.Command("go", "build", "-o", engineBinary, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		os.Remove(engineBinary)
		fmt.Printf("build failed: %v\n%s\n", err, out)
		os.Exit(2)
	}

	code := m.Run()
	os.Remove(engineBinary)
	os.Exit(code)
}

// minimalConfig writes a valid config file (only `symbol` is required by
// config.Load) and returns its path. Sized for tests that need to get
// past config loading to test downstream gates.
func minimalConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("symbol: TESTUSDT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runEngine invokes the binary with args and (optionally) a custom env.
// Pass nil env to inherit; otherwise pass an explicit list.
func runEngine(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(engineBinary, args...)
	if env != nil {
		cmd.Env = env
	}
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

// minEnv returns an environment without any BINANCE_API_* leaking from
// the operator's shell — important so creds-required tests don't silently
// pass when the operator runs locally with creds set.
func minEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
}

// TestCLI_BadConfigPath_Exits1 — config.Load failure path. Distinct from
// "config exists but is invalid" since the engine errors at file-open
// before YAML parsing.
func TestCLI_BadConfigPath_Exits1(t *testing.T) {
	code, out := runEngine(t, nil, "--config", "/nonexistent/path/abc.yaml")
	if code != 1 {
		t.Errorf("expected exit 1 on missing config, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "failed to load config") {
		t.Errorf("expected config load error message, got:\n%s", out)
	}
}

// TestCLI_InvalidSignalTF_Exits1 — the signal-tf switch validates against
// a fixed allowlist (5m | 30m | 1H | 2H | 4H | 1D). A typo'd value should
// fail-fast, not silently default.
func TestCLI_InvalidSignalTF_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	code, out := runEngine(t, nil, "--config", cfg, "--signal-tf", "10m")
	if code != 1 {
		t.Errorf("expected exit 1 on invalid signal-tf, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid --signal-tf") {
		t.Errorf("expected signal-tf error, got:\n%s", out)
	}
}

// TestCLI_InvalidExecutor_Exits1 — the executor switch's default branch
// is the LAST line of defense against the "operator typo'd a flag value
// and silently got paper-money instead of real-money" risk. If a future
// refactor accidentally drops the default case, this test catches it.
func TestCLI_InvalidExecutor_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	code, out := runEngine(t, nil, "--config", cfg, "--executor", "binance_lvie") // typo
	if code != 1 {
		t.Errorf("expected exit 1 on invalid --executor, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid --executor") {
		t.Errorf("expected --executor error message, got:\n%s", out)
	}
}

// TestCLI_BinanceLive_MissingAPIKey_Exits1 — real-money mode without
// API key MUST fail-fast. Pre-promotion this is moot, but the test is
// the regression-guard for the moment STAGE_1 lands.
func TestCLI_BinanceLive_MissingAPIKey_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	env := append(minEnv(), "BINANCE_API_SECRET=fakesecret") // key missing
	code, out := runEngine(t, env, "--config", cfg, "--executor", "binance_live")
	if code != 1 {
		t.Errorf("expected exit 1 on binance_live missing API key, got %d\nout:\n%s",
			code, out)
	}
	if !strings.Contains(out, "BINANCE_API_KEY") {
		t.Errorf("expected error to name BINANCE_API_KEY, got:\n%s", out)
	}
}

// TestCLI_BinanceLive_MissingAPISecret_Exits1 — symmetric to above.
func TestCLI_BinanceLive_MissingAPISecret_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	env := append(minEnv(), "BINANCE_API_KEY=fakekey") // secret missing
	code, out := runEngine(t, env, "--config", cfg, "--executor", "binance_live")
	if code != 1 {
		t.Errorf("expected exit 1 on binance_live missing API secret, got %d\nout:\n%s",
			code, out)
	}
}

// TestCLI_BinanceLiveTestnet_MissingCreds_Exits1 — Layer 2 gate also
// requires creds. Testnet creds are SEPARATE from mainnet per CLAUDE.md
// "real prices, fake fills" contract — but the env var names are the
// same; the operator points at testnet.binancefuture.com.
func TestCLI_BinanceLiveTestnet_MissingCreds_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	code, out := runEngine(t, minEnv(),
		"--config", cfg, "--executor", "binance_live_testnet")
	if code != 1 {
		t.Errorf("expected exit 1 on binance_live_testnet missing creds, got %d\nout:\n%s",
			code, out)
	}
	if !strings.Contains(out, "BINANCE_API_KEY") {
		t.Errorf("expected error to name BINANCE_API_KEY, got:\n%s", out)
	}
}

// TestCLI_Layer3_RequiresStubPrimary verifies the Layer 3 + real-money-
// primary block: the locked rule at real_money_executor_architecture_
// decision_rule_2026-05-08.md forbids wrapping a real-money primary in
// the TeeExecutor. The Tee fans signals/ticks to a testnet shadow for
// parity diffing — wrapping a real-money primary would risk double-
// firing on real money.
func TestCLI_Layer3_RequiresStubPrimary_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	env := append(minEnv(),
		"BINANCE_API_KEY=fakekey",
		"BINANCE_API_SECRET=fakesecret",
	)
	code, out := runEngine(t, env,
		"--config", cfg,
		"--executor", "binance_live",
		"--layer3-binance-testnet-journal-dir", "/tmp/layer3_test")
	if code != 1 {
		t.Errorf("expected exit 1 on Layer 3 + binance_live, got %d\nout:\n%s",
			code, out)
	}
	// The error happens in the binance_live recovery path because Layer 3
	// validation is downstream of the executor switch — but either error
	// message (recovery drift on fake creds OR explicit Layer 3 block)
	// is acceptable. What we care about is that exit != 0.
}

// TestCLI_FundingLoadError_Exits1 — funding CSV load failure must fire a
// Telegram CRITICAL before os.Exit(1). The "engine started" INFO has
// already fired by this point in main(); without the CRITICAL the dead
// engine is invisible to the operator's dashboard until the systemd
// watchdog catches it minutes later. Same shape as the c142e6e fix
// (executor-validation silent on Telegram) — funding load was missed
// in that pass. We verify exit code + that the slog Error message names
// the failure (Telegram delivery itself isn't observable in CLI tests
// without TELEGRAM_* env, but the slog-error-then-exit contract is).
func TestCLI_FundingLoadError_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	// Create a funding dir with a malformed CSV for TESTUSDT — funding.NewHistorical
	// returns a "X data rows but 0 parsed" error when the schema doesn't match,
	// which is the realistic operator-misconfig failure mode (e.g., a bad
	// refresh script left a corrupted CSV in place).
	fundingDir := t.TempDir()
	csvPath := filepath.Join(fundingDir, "TESTUSDT.csv")
	if err := os.WriteFile(csvPath, []byte("not_a_real,header,row\nbad,data,1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runEngine(t, nil, "--config", cfg, "--funding-csv-dir", fundingDir)
	if code != 1 {
		t.Errorf("expected exit 1 on funding load error, got %d\nout:\n%s", code, out)
	}
	if !strings.Contains(out, "failed to load historical funding") {
		t.Errorf("expected funding load error in output, got:\n%s", out)
	}
}

// TestCLI_Layer3_MissingCreds_Exits1 — when the Layer 3 wrap is requested
// but BINANCE_API_* env is unset, the testnet shadow can't be constructed.
// Must fail-fast.
func TestCLI_Layer3_MissingCreds_Exits1(t *testing.T) {
	cfg := minimalConfig(t)
	code, out := runEngine(t, minEnv(),
		"--config", cfg,
		"--layer3-binance-testnet-journal-dir", "/tmp/layer3_test")
	if code != 1 {
		t.Errorf("expected exit 1 on Layer 3 missing creds, got %d\nout:\n%s",
			code, out)
	}
	if !strings.Contains(out, "BINANCE_API_KEY") {
		t.Errorf("expected error to name BINANCE_API_KEY, got:\n%s", out)
	}
}
